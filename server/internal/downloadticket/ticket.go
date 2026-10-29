// Package downloadticket 提供下载票据（Download Ticket）的签发与验签。
//
// 背景：登录 token 只应通过请求头（Authorization）传递、绝不能出现在 URL 中，
// 而浏览器原生 <a href>/<img>/<video> 与外部下载工具（迅雷等）只能使用 URL。
// 因此对「需要鉴权 + 原生流式下载」的资源，采用业界通用的预签名短时效 URL：
// 服务端携带登录用户上下文签发一个「短时效、绑定单个文件」的签名串放进 URL，
// 下载端点只校验该签名而不校验登录 token。签名放 URL 的是票据而非登录凭证，
// 泄露影响收敛为「单个文件 + 短时间窗」。
//
// 票据按「时限」生效（不做一次性消费）：同一票据在有效期内可重复读取，
// 以兼容断点续传、多段 Range、HEAD 探测与音视频拖动。
//
// 票据绑定的是文件身份（端点种类 + 根目录 + 规范相对路径 + 大小 + 修改时间），
// 而不是可被前缀归一化混淆的 URL：同一票据不能跨端点（管理下载 vs 普通下载）、
// 不能下载其它文件，也不能命中同名重传后的新文件。
// 校验比对必须由下载服务在解析出真实文件后执行（见服务层 matchTicket）。
//
// 票据格式：BASE64URL(JSON payload) + "." + BASE64URL(HMAC-SHA256(payload, JWTSecret))。
package downloadticket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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

// 端点种类
const (
	// KindPublic 普通下载（/download/*、/api/v1/web/download/*）
	KindPublic = "public"
	// KindAdmin 管理页下载（/api/v1/web/admin/download/*）
	KindAdmin = "admin"
)

const (
	// downloadTicketTTL 下载票据有效期（秒）：按泄露面收敛，取较短时间窗
	downloadTicketTTL = 5 * 60
	// previewTicketTTL 预览票据有效期（秒）：音视频拖动/Range 需要更长时间窗
	previewTicketTTL = 10 * 60
)

// Payload 票据内嵌载荷（会被编码进 URL，字段名保持简短）
type Payload struct {
	V     int    `json:"v"`  // 版本
	Kind  string `json:"k"`  // 端点种类：public / admin
	Root  string `json:"r"`  // 存储根目录名
	Rel   string `json:"p"`  // 规范相对路径（不含根目录前缀）
	Size  int64  `json:"s"`  // 文件大小（字节）
	Mtime int64  `json:"mt"` // 文件修改时间（Unix 秒）
	M     Mode   `json:"m"`  // 用途：download / preview
	E     int64  `json:"e"`  // 过期 Unix 秒
	U     string `json:"u"`  // 签发时的用户名
	R     string `json:"ro"` // 签发时的角色（admin / user）
	D     string `json:"d"`  // 签发时的用户 UUID
}

// ResourceKey 文件身份（由下载服务解析真实文件后计算）
type ResourceKey struct {
	Kind  string
	Root  string
	Rel   string
	Size  int64
	Mtime int64
}

// Signer 签发票据所需的用户上下文
type Signer struct {
	Username string
	Role     string
	UUID     string
}

// ErrInvalid 票据非法（签名错误/格式错误/字段缺失）
var ErrInvalid = errors.New("invalid download ticket")

// ErrExpired 票据已过期
var ErrExpired = errors.New("download ticket expired")

// TicketParamName 票据在 URL 中的查询参数名
const TicketParamName = "t"

// 票据载荷在 request context 中的键（供服务层读取并做文件绑定比对）
type payloadCtxKey struct{}

// WithPayload 将票据载荷写入 request context
func WithPayload(ctx context.Context, p *Payload) context.Context {
	return context.WithValue(ctx, payloadCtxKey{}, p)
}

// FromContext 从 request context 读取票据载荷；无票据时返回 nil
func FromContext(ctx context.Context) *Payload {
	if ctx == nil {
		return nil
	}
	if p, ok := ctx.Value(payloadCtxKey{}).(*Payload); ok {
		return p
	}
	return nil
}

// NormalizeRel 归一化相对路径：统一分隔符、清理 . / ..、去掉首尾斜杠。
// 签发侧与服务侧必须共用本函数，保证比对一致。
func NormalizeRel(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if cleaned == "." {
		return ""
	}
	return strings.Trim(cleaned, "/")
}

// SplitRequestPath 从下载 URL 路径中拆出端点种类与剩余部分（根目录/子路径或哈希）。
// 顺序很重要：先匹配更长的管理下载前缀，避免被普通下载前缀误判。
func SplitRequestPath(p string) (kind string, rest string, ok bool) {
	switch {
	case strings.HasPrefix(p, "/api/v1/web/admin/download/"):
		return KindAdmin, strings.TrimPrefix(p, "/api/v1/web/admin/download/"), true
	case strings.HasPrefix(p, "/api/v1/web/download/"):
		return KindPublic, strings.TrimPrefix(p, "/api/v1/web/download/"), true
	case strings.HasPrefix(p, "/download/"):
		return KindPublic, strings.TrimPrefix(p, "/download/"), true
	default:
		return "", "", false
	}
}

// ModeForPreview 返回预览开关对应的票据用途
func ModeForPreview(preview bool) Mode {
	if preview {
		return ModePreview
	}
	return ModeDownload
}

// expire 计算票据过期时间（按用途区分有效期）
func expire(m Mode) time.Time {
	if m == ModePreview {
		return time.Now().Add(previewTicketTTL * time.Second)
	}
	return time.Now().Add(downloadTicketTTL * time.Second)
}

// Sign 为指定文件身份签发一枚票据字符串（不包含路径本身，路径以请求路径形式附在 URL 上）。
// 返回可直接拼接到 URL 的 t 参数值。
func Sign(key ResourceKey, mode Mode, user Signer) (string, error) {
	payload := &Payload{
		V:     1,
		Kind:  key.Kind,
		Root:  key.Root,
		Rel:   NormalizeRel(key.Rel),
		Size:  key.Size,
		Mtime: key.Mtime,
		M:     mode,
		E:     expire(mode).Unix(),
		U:     user.Username,
		R:     user.Role,
		D:     user.UUID,
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
// 注意：本函数只做签名/格式/时限校验；文件绑定由下载服务解析真实文件后比对。
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
	// 旧版本票据（无文件绑定字段）一律拒绝，避免退化为“路径级”弱绑定
	if p.V != 1 || p.Kind == "" || p.Root == "" || p.Rel == "" || p.M == "" || p.Size < 0 {
		return nil, ErrInvalid
	}
	if p.M != ModeDownload && p.M != ModePreview {
		return nil, ErrInvalid
	}
	if p.E < time.Now().Unix() {
		return nil, ErrExpired
	}
	return &p, nil
}

// Match 判断本次请求的真实文件身份与用途是否与票据一致。
// preview 为本次请求是否按内联预览处理（来自 preview=true 查询参数）。
func (p *Payload) Match(key ResourceKey, preview bool) bool {
	if p == nil {
		return false
	}
	if p.M != ModeForPreview(preview) {
		return false
	}
	if p.Kind != key.Kind {
		return false
	}
	if p.Root != key.Root {
		return false
	}
	if NormalizeRel(p.Rel) != NormalizeRel(key.Rel) {
		return false
	}
	if p.Size != key.Size || p.Mtime != key.Mtime {
		return false
	}
	return true
}
