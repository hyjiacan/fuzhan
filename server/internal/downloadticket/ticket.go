// Package downloadticket 提供下载票据（Download Ticket）的签发、验签与一次性消费。
//
// 背景：登录 token 只应通过请求头（Authorization）传递、绝不能出现在 URL 中，
// 而浏览器原生 <a href>/<img>/<video> 与外部下载工具（迅雷等）只能使用 URL。
// 因此对「需要鉴权 + 原生流式下载」的资源，采用业界通用的预签名短时效 URL：
// 服务端携带登录用户上下文签发一个「一次性、短时效、绑定单个文件」的签名串放进 URL，
// 下载端点只校验该签名而不校验登录 token。签名放 URL 的是票据而非登录凭证，
// 泄露影响收敛为「单个文件 + 短时间窗」，且消费后即失效。
//
// 票据内嵌签发时的用户上下文（用户名/角色/UUID），经 HMAC 签名防篡改。
// LoginGate 验签通过后据此注入上下文，因此下游 AuthRequired / RequireAdmin
// 能识别该请求已通过「票据鉴权」，正常放行（含管理页下载）。
//
// 票据格式：BASE64URL(JSON payload) + "." + BASE64URL(HMAC-SHA256(payload, JWTSecret))。
// 签名密钥复用 JWT 密钥（HS256），不新增密钥配置；重启后因密钥已持久化，旧票据仍可验签。
package downloadticket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"fuzhan/pkg/jwt"
)

// Mode 票据用途
type Mode string

const (
	// ModeDownload 真实下载（附件 Content-Disposition）
	ModeDownload Mode = "download"
	// ModePreview 内联预览（preview=true，供 <img>/<video> 等原生标签直连）
	ModePreview Mode = "preview"
)

const (
	// downloadTicketTTL 下载票据有效期（秒）
	downloadTicketTTL = 15 * 60
	// previewTicketTTL 预览票据有效期（秒）
	previewTicketTTL = 10 * 60
	// consumedCacheCap 一次性消费缓存上限（条）；超出后自动清除已过期条目，仍超限则允许少量重放
	consumedCacheCap = 20000
)

// Payload 票据内嵌载荷（会被编码进 URL，字段名保持简短）
type Payload struct {
	P string `json:"p"` // 目标路径（根目录名/子路径 或 16 位哈希）
	M Mode   `json:"m"` // 用途：download / preview
	E int64  `json:"e"` // 过期 Unix 秒
	U string `json:"u"` // 签发时的用户名
	R string `json:"r"` // 签发时的角色（admin / user）
	D string `json:"d"` // 签发时的用户 UUID
}

// Signer 签发票据所需的用户上下文
type Signer struct {
	Username string
	Role     string
	UUID     string
}

// ErrInvalid 票据非法（签名错误/格式错误）
var ErrInvalid = errors.New("invalid download ticket")

// ErrExpired 票据已过期
var ErrExpired = errors.New("download ticket expired")

// consumedCache 一次性票据消费缓存（简单内存去重，进程内有效）
type consumedCache struct {
	mu      sync.Mutex
	keys    map[string]int64 // 已消费票据的消费 key -> 消费时 Unix 秒
	overCap bool             // 上次清理后仍超出上限，放宽一次性约束
}

var consumed = &consumedCache{keys: make(map[string]int64)}

// consume 标记指定票据已消费；返回 false 表示该票据此前已被消费过（重复使用）。
func (cc *consumedCache) consume(key string) bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	now := time.Now().Unix()
	if _, seen := cc.keys[key]; seen {
		return false
	}
	if len(cc.keys) >= consumedCacheCap {
		// 超出容量上限：先清除已过期的消费记录（保留 1 天窗口），仍不够则标记超限、放弃去重
		cc.pruneLocked(now - 24*3600)
		if len(cc.keys) >= consumedCacheCap {
			cc.overCap = true
		}
	}
	if cc.overCap {
		// 内存防御：超限后停止累积，允许票据重复使用（不影响签名/时效校验）
		return true
	}
	cc.keys[key] = now
	return true
}

// pruneLocked 删除早于 cutoff 的消费记录（调用方须持有锁）
func (cc *consumedCache) pruneLocked(cutoff int64) {
	for k, ts := range cc.keys {
		if ts < cutoff {
			delete(cc.keys, k)
		}
	}
}

// expire 计算票据过期时间（按用途区分有效期）
func expire(m Mode) time.Time {
	if m == ModePreview {
		return time.Now().Add(previewTicketTTL * time.Second)
	}
	return time.Now().Add(downloadTicketTTL * time.Second)
}

// Sign 为指定路径签发一枚票据字符串（不包含路径本身，路径以请求路径形式附在 URL 上）。
// 返回可直接拼接到 URL 的 t 参数值。
func Sign(path string, mode Mode, user Signer) (string, error) {
	payload := &Payload{
		P: path,
		M: mode,
		E: expire(mode).Unix(),
		U: user.Username,
		R: user.Role,
		D: user.UUID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(raw)
	sig := hmacSHA256(payloadB64)
	return payloadB64 + "." + sig, nil
}

// hmacSHA256 用 JWT 密钥对签名字符串计算 HMAC-SHA256，返回 base64url 摘要
func hmacSHA256(data string) string {
	secret := jwt.GetJWTSecret()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Validate 校验票据并返回其载荷。校验失败时返回 ErrInvalid/ErrExpired。
// 校验通过后随即标记一次性消费：同票据第二次使用返回 ErrInvalid。
func Validate(ticket string) (*Payload, error) {
	if ticket == "" {
		return nil, ErrInvalid
	}
	parts := strings.SplitN(ticket, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalid
	}
	payloadB64, sig := parts[0], parts[1]

	// 常量时间比较签名，防时序侧信道
	expected := hmacSHA256(payloadB64)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return nil, ErrInvalid
	}

	raw, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrInvalid
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ErrInvalid
	}
	if p.P == "" || p.M == "" {
		return nil, ErrInvalid
	}
	if p.E < time.Now().Unix() {
		return nil, ErrExpired
	}

	// 一次性消费（去重 key 绑定 路径+用途+过期时刻）
	if !consumed.consume(p.P + "|" + string(p.M) + "|" + strconv.FormatInt(p.E, 10)) {
		return nil, ErrInvalid // 已消费/重放
	}
	return &p, nil
}

// TicketParamName 票据在 URL 中的查询参数名
const TicketParamName = "t"
