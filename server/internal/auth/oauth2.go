package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"
	"gorm.io/gorm"
)

// OAuth2Config 外部 OAuth2/OIDC 认证配置（与 fuzhan.yaml 的 auth.oauth2 节点一一对应）
type OAuth2Config struct {
	Enabled            bool     `yaml:"enabled"`
	ProviderName       string   `yaml:"provider_name,omitempty"`
	ClientID           string   `yaml:"client_id,omitempty"`
	ClientSecret       string   `yaml:"client_secret,omitempty"`
	AuthURL            string   `yaml:"auth_url,omitempty"`
	TokenURL           string   `yaml:"token_url,omitempty"`
	UserInfoURL        string   `yaml:"user_info_url,omitempty"`
	Issuer             string   `yaml:"issuer,omitempty"`
	Scopes             []string `yaml:"scopes,omitempty"`
	RedirectURL        string   `yaml:"redirect_url,omitempty"`
	UsernameClaim      string   `yaml:"username_claim,omitempty"`
	DisplayNameClaim   string   `yaml:"display_name_claim,omitempty"`
	EmailClaim         string   `yaml:"email_claim,omitempty"`
	AutoCreateUser     bool     `yaml:"auto_create_user,omitempty"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify,omitempty"`
}

// oauth2HTTPTimeout 外部认证系统的单次请求超时
const oauth2HTTPTimeout = 15 * time.Second

// oauth2StateTTL 授权 state 的有效期（秒）
const oauth2StateTTL = 600

// ==================== id_token 签名验证（OIDC） ====================
//
// 背景：id_token 是外部认证系统签发的 JWT，用于向本服务证明「用户已在
// 外部系统成功认证」。此前实现仅 base64 解码载荷、不验签，任何人都可
// 伪造 id_token 冒充认证通过（严重漏洞）。本轮改为硬性验签 + 关键 claim
// 校验，任何失败一律拒绝登录，不允许静默降级为「不验签」放行。

// jwksCacheTTL JWKS 缓存有效期。远端密钥轮换频率通常远低于登录频率，
// 10 分钟 TTL 足以覆盖绝大多数场景，同时把密钥轮换的生效延迟控制在可接受范围。
const jwksCacheTTL = 10 * time.Minute

// jwksRefreshBackoff 拉新失败后的最小重试间隔，避免在远端故障期间高频打爆认证端点。
const jwksRefreshBackoff = 1 * time.Minute

// jwksKey 缓存中的单个 JWK。
// 关键字段（n/e/x/y/crv 等）以 json.RawMessage 原样保留，验签时再解码为字节，
// 从网络到验证全程不丢失任何字符，保证与签发端签名输入完全一致。
type jwksKey struct {
	Kty     string                     `json:"kty"`
	Kid     string                     `json:"kid"`
	Use     string                     `json:"use"`
	Alg     string                     `json:"alg"`
	RawJSON map[string]json.RawMessage `json:"-"` // 原始字段表，验签时按名取值（n/e/x/y/crv 等）
}

// UnmarshalJSON 反序列化单个 JWK：
// 常规字段（kty/kid/use/alg）走标准字段填充，其余原始字段（n/e/x/y/crv 等）原样
// 保留进 RawJSON 供验签时读取。注意：RawJSON 带 json:"-" 标签，若不在此自定义
// Unmarshal 中手动填充，标准库反序列化会将其忽略，验签将永远拿不到公钥参数。
func (k *jwksKey) UnmarshalJSON(data []byte) error {
	type alias jwksKey // 避免递归调用本方法
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*k = jwksKey(a)
	if err := json.Unmarshal(data, &k.RawJSON); err != nil {
		return err
	}
	// RawJSON 中已含 kty/kid/use/alg，保留重复键不影响取值；无需额外处理
	return nil
}

// jwksSet 内存中的 JWKS 缓存（含拉取状态，用于失效/回退决策）。
type jwksSet struct {
	Keys       []*jwksKey
	FetchedAt  time.Time // 最近一次成功拉取时间（用于 TTL 判断）
	LastFetch  time.Time // 最近一次尝试拉取时间（无论成败，用于失败回退计时）
	ConsecFail int       // 连续失败次数
}

// jwtVerifyState id_token 验证过程的并发/缓存状态（挂在 OAuth2Service 上）。
type jwtVerifyState struct {
	mu      sync.Mutex
	jwks    *jwksSet
	lastErr error
}

// OAuth2Service 外部 OAuth2/OIDC 认证服务
type OAuth2Service struct {
	db     *gorm.DB
	cfg    *OAuth2Config
	mu     sync.RWMutex
	verify jwtVerifyState
}

// NewOAuth2Service 创建 OAuth2 服务
func NewOAuth2Service(db *gorm.DB, cfg *OAuth2Config) *OAuth2Service {
	return &OAuth2Service{db: db, cfg: cfg}
}

// UpdateConfig 热更新配置（配置保存后由 OnSave 钩子调用）
func (s *OAuth2Service) UpdateConfig(cfg *OAuth2Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// GetConfig 获取当前配置
func (s *OAuth2Service) GetConfig() *OAuth2Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// IsEnabled 检查 OAuth2 是否启用（需具备客户端凭据与至少一个授权入口）
func (s *OAuth2Service) IsEnabled() bool {
	cfg := s.GetConfig()
	if cfg == nil || !cfg.Enabled {
		return false
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return false
	}
	return cfg.AuthURL != "" || cfg.Issuer != ""
}

// defaultScopes 返回默认请求的 scope（未配置时使用 OIDC 常用组合）
func defaultScopes(cfg *OAuth2Config) []string {
	if len(cfg.Scopes) > 0 {
		return cfg.Scopes
	}
	return []string{"openid", "profile", "email"}
}

// httpClient 构造外部请求客户端，按配置决定是否跳过 TLS 校验；
// 同时复用现有 SSRF 防护（CheckIPSafe + Dialer.Control），
// 在连接建立时校验目标 IP，防止 OIDC 发现 / JWKS / userinfo 等请求被劫持到内网或元数据地址。
func (s *OAuth2Service) httpClient(cfg *OAuth2Config) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			if ip := net.ParseIP(host); ip != nil {
				return utils.CheckIPSafe(ip, oauth2SSRFConfig())
			}
			// 是域名：解析后逐一校验全部解析结果（防 DNS 重绑定/多 A 记录绕过）
			ips, lookupErr := net.LookupIP(host)
			if lookupErr != nil {
				return fmt.Errorf("SSRF 防护: DNS 解析失败 %s: %w", host, lookupErr)
			}
			for _, resolvedIP := range ips {
				if err := utils.CheckIPSafe(resolvedIP, oauth2SSRFConfig()); err != nil {
					return fmt.Errorf("SSRF 防护: %s (%s) %w", host, resolvedIP.String(), err)
				}
			}
			return nil
		},
	}
	transport := &http.Transport{
		DialContext: dialer.DialContext,
		// 仅当配置显式开启 InsecureSkipVerify 时跳过证书校验（默认强制校验）
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
	}
	return &http.Client{Timeout: oauth2HTTPTimeout, Transport: transport}
}

// oauth2SSRFConfig 返回 OAuth2 外部请求的 SSRF 校验配置。
// 默认（url_upload 未显式启用）拒绝内网/回环/链路本地等保留地址；
// 若管理员显式配置了 allowed_ip_ranges 白名单，则按白名单放行（与 URL 上传同一套策略）。
func oauth2SSRFConfig() utils.URLUploadConfig {
	return utils.URLUploadConfig{
		Enabled:         appconfig.GlobalConfig.Upload.URLUpload.Enabled,
		AllowedIPRanges: appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges,
	}
}

// endpoints 已解析的端点集合
type endpoints struct {
	AuthURL     string
	TokenURL    string
	UserInfoURL string
	Issuer      string
	JWKSURI     string
}

// resolveEndpoints 解析实际使用的端点：显式配置优先；配置了 issuer 时执行 OIDC 发现以补齐。
// 无论端点是否已显式配置，只要配置了 issuer 就执行发现——这样能拿到 jwks_uri，
// 供 id_token 验签使用；显式配置的端点始终优先于发现结果。
func (s *OAuth2Service) resolveEndpoints(cfg *OAuth2Config) (*endpoints, error) {
	ep := &endpoints{AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL, UserInfoURL: cfg.UserInfoURL, Issuer: cfg.Issuer}
	if cfg.Issuer == "" {
		if ep.AuthURL == "" || ep.TokenURL == "" {
			return nil, errors.New("OAuth2 授权/令牌端点未配置（可配置 issuer 自动发现）")
		}
		// 未配置 issuer：无法做 OIDC 发现，也就拿不到 jwks_uri 与 issuer 校验值，
		// 此时 id_token 验签（signature 校验仍可做，但 iss 校验会跳过）由 verifyIDToken 按配置处理。
		return ep, nil
	}

	wellKnown := strings.TrimSuffix(cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequest(http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 OIDC 发现请求失败: %w", err)
	}
	resp, err := s.httpClient(cfg).Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC 端点发现失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC 端点发现失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 OIDC 发现结果失败: %w", err)
	}
	var doc struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		UserinfoEndpoint      string `json:"userinfo_endpoint"`
		JwksURI               string `json:"jwks_uri"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("解析 OIDC 发现结果失败: %w", err)
	}
	if ep.AuthURL == "" {
		ep.AuthURL = doc.AuthorizationEndpoint
	}
	if ep.TokenURL == "" {
		ep.TokenURL = doc.TokenEndpoint
	}
	if ep.UserInfoURL == "" {
		ep.UserInfoURL = doc.UserinfoEndpoint
	}
	if ep.Issuer == "" {
		ep.Issuer = doc.Issuer
	}
	// jwks_uri 仅用于 "从 id_token 提取用户信息" 这一备用路径的验签：
	// 当走 userinfo 端点取用户信息时，id_token 仅用于交叉校验 sub（见 Exchange）。
	// 因此仅在未显式配置 user_info_url 时才需要它（此时拿不到则验签无法进行，后续会拒绝登录）。
	ep.JWKSURI = doc.JwksURI
	if ep.AuthURL == "" || ep.TokenURL == "" {
		return nil, errors.New("OIDC 发现结果缺少授权/令牌端点")
	}
	return ep, nil
}

// stateSecret 返回用于签名 state 的密钥（复用 JWT 密钥，随其轮换自动更新）
func stateSecret() []byte {
	return jwt.GetJWTSecret()
}

// signState 生成带签名与过期时间的 state，用于回调时校验请求来源（防 CSRF）
func (s *OAuth2Service) signState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 state 失败: %w", err)
	}
	payload := fmt.Sprintf("%d.%s", time.Now().Add(oauth2StateTTL*time.Second).Unix(), hex.EncodeToString(buf))
	mac := hmac.New(sha256.New, stateSecret())
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "." + sig)), nil
}

// verifyState 校验回调 state 的签名与有效期
func (s *OAuth2Service) verifyState(state string) error {
	if state == "" {
		return errors.New("缺少 state 参数")
	}
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return errors.New("state 格式非法")
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return errors.New("state 格式非法")
	}
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, stateSecret())
	mac.Write([]byte(payload))
	expect := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expect), []byte(parts[2])) {
		return errors.New("state 校验失败")
	}
	var exp int64
	if _, err := fmt.Sscanf(parts[0], "%d", &exp); err != nil {
		return errors.New("state 格式非法")
	}
	if time.Now().Unix() > exp {
		return errors.New("state 已过期，请重新登录")
	}
	return nil
}

// AuthorizeURL 生成跳转到外部认证系统的授权地址
func (s *OAuth2Service) AuthorizeURL(redirectURL string) (string, string, error) {
	cfg := s.GetConfig()
	if !s.IsEnabled() {
		return "", "", errors.New("OAuth2 认证未启用")
	}
	ep, err := s.resolveEndpoints(cfg)
	if err != nil {
		return "", "", err
	}
	state, err := s.signState()
	if err != nil {
		return "", "", err
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("scope", strings.Join(defaultScopes(cfg), " "))
	q.Set("state", state)
	sep := "?"
	if strings.Contains(ep.AuthURL, "?") {
		sep = "&"
	}
	return ep.AuthURL + sep + q.Encode(), state, nil
}

// oauthUserInfo 从外部系统解析出的用户信息
type oauthUserInfo struct {
	Subject     string
	Username    string
	DisplayName string
	Email       string
}

// Exchange 用授权码换取访问令牌，并拉取用户信息
func (s *OAuth2Service) Exchange(code, redirectURL string) (*oauthUserInfo, error) {
	cfg := s.GetConfig()
	if !s.IsEnabled() {
		return nil, errors.New("OAuth2 认证未启用")
	}
	ep, err := s.resolveEndpoints(cfg)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURL)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequest(http.MethodPost, ep.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构造令牌请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient(cfg).Do(req)
	if err != nil {
		return nil, fmt.Errorf("换取令牌失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("换取令牌失败: HTTP %d", resp.StatusCode)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("解析令牌响应失败: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("外部认证失败: %s %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return nil, errors.New("外部认证未返回访问令牌")
	}

	claims := map[string]interface{}{}
	var idTokenSubject string

	// 强制校验 id_token：无论是否走 userinfo 端点，都先对 id_token 验签并校验
	// iss/aud/exp/iat/nbf，任何失败都拒绝登录（不允许静默降级为不验签）。
	if tokenResp.IDToken != "" {
		idClaims, err := s.verifyIDToken(cfg, tokenResp.IDToken)
		if err != nil {
			return nil, err
		}
		idTokenSubject, _ = idClaims["sub"].(string)
	}

	// 用户信息优先取 userinfo 端点；端点缺失时回退到已（强制验签过的）id_token 载荷。
	// 回退路径使用验签后的 claims（即 verifiedIDClaims），绝不再使用未验签的原始载荷。
	if ep.UserInfoURL != "" {
		claims, err = s.fetchUserInfo(cfg, ep.UserInfoURL, tokenResp.AccessToken)
		if err != nil {
			return nil, err
		}
	}
	if len(claims) == 0 {
		if tokenResp.IDToken == "" {
			return nil, errors.New("未返回 id_token 且未配置 user_info_url，无法获取用户信息")
		}
		// 重新走一次完整验签拿到经核验的 claims（避免在路径 A 未验签时被误用）。
		verified, err := s.verifyIDToken(cfg, tokenResp.IDToken)
		if err != nil {
			return nil, err
		}
		claims = verified
	}

	// 交叉校验：userinfo 的 sub 应与 id_token 的 sub 一致（OIDC 要求），
	// 防止用过期/走查的 userinfo 冒充（id_token 的 sub 已经过验签）。
	if idTokenSubject != "" {
		if sub, _ := claims["sub"].(string); sub != "" && sub != idTokenSubject {
			return nil, errors.New("userinfo 与 id_token 的 sub 不一致")
		}
	}
	return s.mapUserInfo(cfg, claims)
}

// fetchUserInfo 调用用户信息端点获取声明集合
func (s *OAuth2Service) fetchUserInfo(cfg *OAuth2Config, userInfoURL, accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequest(http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造用户信息请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient(cfg).Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取用户信息失败: HTTP %d", resp.StatusCode)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %w", err)
	}
	// 部分提供方将用户信息包裹在 data 字段中
	if data, ok := raw["data"].(map[string]interface{}); ok && len(data) > 0 {
		return data, nil
	}
	return raw, nil
}

// ==================== id_token 签名验证与 claim 校验 ====================
//
// 此前的 decodeIDTokenClaims 仅 base64 解码载荷、不做任何校验，任何人伪造
// id_token 即可冒充认证通过。以下实现为硬性验签：签名验证 + 关键 claim
// 校验，任一失败都拒绝登录，不允许静默降级为「不验签」放行。

// verifyIDToken 对 id_token 执行完整的 OIDC 校验：签名验证 + iss/aud/exp/iat/nbf 校验 + nonce 记录。
// 校验通过返回其中的 claims 供后续提取用户信息；任何一步失败均返回错误并拒绝登录。
func (s *OAuth2Service) verifyIDToken(cfg *OAuth2Config, idToken string) (map[string]interface{}, error) {
	if strings.TrimSpace(idToken) == "" {
		return nil, errors.New("缺少 id_token")
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("id_token 格式非法（须为三段式 JWT）")
	}

	headerB64, payloadB64, sigB64 := parts[0], parts[1], parts[2]
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, errors.New("id_token 签名编码非法")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil || json.Unmarshal(headerRaw, &header) != nil {
		return nil, errors.New("id_token 头部编码非法")
	}
	if header.Alg == "" {
		return nil, errors.New("id_token 缺少 alg")
	}

	// 签名数据 = "header.payload"（与签发端完全一致）
	signingInput := headerB64 + "." + payloadB64

	// 按 kid 从 JWKS 取验签公钥（JWK 的 kty/use/alg 与 header.alg 需一致）
	keyForVerify, err := s.lookupVerifyKey(cfg, header.Kid, header.Alg)
	if err != nil {
		return nil, err
	}
	if err := verifySignature(keyForVerify, header.Alg, signingInput, sig); err != nil {
		return nil, err
	}

	// 签名通过后再解析载荷，做 claim 校验
	var claims map[string]interface{}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || json.Unmarshal(payloadRaw, &claims) != nil {
		return nil, errors.New("id_token 载荷解析失败")
	}
	if err := s.validateIDTokenClaims(cfg, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// lookupVerifyKey 获取用于验签的 JWK 公钥：优先按 kid 精确匹配，其次在无 kid 的 key 中按算法类型匹配。
func (s *OAuth2Service) lookupVerifyKey(cfg *OAuth2Config, kid, alg string) (*jwksKey, error) {
	set, err := s.getJWKS(cfg)
	if err != nil {
		return nil, err
	}
	var candidates []*jwksKey
	for _, k := range set.Keys {
		if kid != "" && k.Kid == kid {
			candidates = append(candidates, k)
		}
	}
	if len(candidates) == 0 {
		// kid 精确匹配失败时回退：无 kid 的 key 且密钥类型与算法一致（兼容省略 kid 的简化部署）
		for _, k := range set.Keys {
			if k.Kid == "" && keyTypeMatches(k, alg) {
				candidates = append(candidates, k)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("JWKS 中找不到匹配的公钥（kid=%q，alg=%s）", kid, alg)
	}
	// 有多个候选时优先选 use=sig（或未声明 use）且 alg 匹配的
	for _, k := range candidates {
		if k.Use == "sig" || k.Use == "" {
			if k.Alg == "" || k.Alg == alg {
				return k, nil
			}
		}
	}
	// 其余候选（可能 use 声明不符或 alg 声明不同）仅当恰好只有 1 个时采用，
	// 多个一律拒绝，避免选错密钥（防密钥选择攻击）。
	if len(candidates) == 1 && candidates[0].Kid == kid {
		return candidates[0], nil
	}
	return nil, fmt.Errorf("JWKS 中存在多个候选公钥，但均不符合 use/alg 约束（kid=%q）", kid)
}

// keyTypeMatches 判断 JWK 的密钥类型（kty）是否与 JWT alg 的预期一致（RSA 系 / EC 系）。
func keyTypeMatches(k *jwksKey, alg string) bool {
	if strings.HasPrefix(alg, "RS") || alg == "PS256" || alg == "PS384" || alg == "PS512" {
		return k.Kty == "RSA"
	}
	if strings.HasPrefix(alg, "ES") {
		return k.Kty == "EC"
	}
	return false
}

// verifySignature 用指定算法验签。
// 支持：RS256/RS384/RS512（RSA PKCS#1 v1.5）、ES256/ES384/ES512（ECDSA，签名须为 R||S 拼接格式）。
// 其他 alg（如 none、HS*、PS*）一律拒绝。
func verifySignature(key *jwksKey, alg, signingInput string, sig []byte) error {
	digest, hash, err := computeDigest(alg, signingInput)
	if err != nil {
		return err
	}
	switch alg {
	case "RS256", "RS384", "RS512":
		pub, err := parseRSAPublicKey(key)
		if err != nil {
			return err
		}
		if err := rsa.VerifyPKCS1v15(pub, hash, digest, sig); err != nil {
			return errors.New("id_token RSA 签名验证失败")
		}
		return nil

	case "ES256", "ES384", "ES512":
		pub, err := parseECDSAPublicKey(key)
		if err != nil {
			return err
		}
		size := (pub.Curve.Params().BitSize + 7) / 8
		if len(sig) != 2*size {
			return errors.New("id_token ECDSA 签名长度非法")
		}
		r := new(big.Int).SetBytes(sig[:size])
		sVal := new(big.Int).SetBytes(sig[size:])
		if !ecdsa.Verify(pub, digest, r, sVal) {
			return errors.New("id_token ECDSA 签名验证失败")
		}
		return nil

	default:
		// 白名单外的算法（含 none、HS、PS 等）一律拒绝，防止 alg 混淆攻击
		return fmt.Errorf("不支持的 id_token 签名算法: %s", alg)
	}
}

// computeDigest 按算法计算签名数据的摘要，并返回对应的 crypto.Hash 标识（RSA 验签需要）。
func computeDigest(alg, signingInput string) ([]byte, crypto.Hash, error) {
	input := []byte(signingInput)
	switch alg {
	case "RS256", "ES256":
		d := sha256.Sum256(input)
		return d[:], crypto.SHA256, nil
	case "RS384", "ES384":
		d := sha512.Sum384(input)
		return d[:], crypto.SHA384, nil
	case "RS512", "ES512":
		d := sha512.Sum512(input)
		return d[:], crypto.SHA512, nil
	default:
		return nil, 0, fmt.Errorf("不支持的签名算法: %s", alg)
	}
}

// jwkString 读取 JWK 原始字段中的字符串值（RawMessage 含引号，需先反序列化）
func jwkString(k *jwksKey, field string) (string, bool) {
	raw, ok := k.RawJSON[field]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// parseRSAPublicKey 从 JWK 构造 RSA 公钥（n、e 为大端无符号整数）
func parseRSAPublicKey(k *jwksKey) (*rsa.PublicKey, error) {
	if k.Kty != "RSA" {
		return nil, errors.New("JWK 密钥类型不是 RSA")
	}
	nB64, okN := jwkString(k, "n")
	eB64, okE := jwkString(k, "e")
	if !okN || !okE {
		return nil, errors.New("JWK 缺少 n/e")
	}
	rawN, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, errors.New("JWK n 解码失败")
	}
	rawE, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, errors.New("JWK e 解码失败")
	}
	n := new(big.Int).SetBytes(rawN)
	e := 0
	for _, b := range rawE {
		e = e<<8 | int(b)
	}
	if e < 3 || e == 0 || e&1 == 0 {
		return nil, errors.New("JWK e 非法")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

// parseECDSAPublicKey 从 JWK 构造 ECDSA 公钥（crv 决定曲线）
func parseECDSAPublicKey(k *jwksKey) (*ecdsa.PublicKey, error) {
	if k.Kty != "EC" {
		return nil, errors.New("JWK 密钥类型不是 EC")
	}
	crv, okC := jwkString(k, "crv")
	if !okC {
		return nil, errors.New("JWK 缺少 crv")
	}
	curve, ok := curveForJOSEAlg(crv)
	if !ok {
		return nil, fmt.Errorf("JWK crv 非法: %s", crv)
	}
	xB64, okX := jwkString(k, "x")
	yB64, okY := jwkString(k, "y")
	if !okX || !okY {
		return nil, errors.New("JWK 缺少 x/y")
	}
	rawX, err1 := base64.RawURLEncoding.DecodeString(xB64)
	rawY, err2 := base64.RawURLEncoding.DecodeString(yB64)
	if err1 != nil || err2 != nil {
		return nil, errors.New("JWK x/y 解码失败")
	}
	byteLen := (curve.Params().BitSize + 7) / 8
	pub := &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(rawX),
		Y:     new(big.Int).SetBytes(rawY),
	}
	if len(rawX) > byteLen || len(rawY) > byteLen {
		return nil, errors.New("JWK x/y 长度非法")
	}
	if !curve.IsOnCurve(pub.X, pub.Y) {
		return nil, errors.New("JWK x/y 不在曲线上")
	}
	return pub, nil
}

// curveForJOSEAlg 返回 ECDSA 曲线，支持 P-256/P-384/P-521
func curveForJOSEAlg(crv string) (elliptic.Curve, bool) {
	switch crv {
	case "P-256":
		return elliptic.P256(), true
	case "P-384":
		return elliptic.P384(), true
	case "P-521":
		return elliptic.P521(), true
	default:
		return nil, false
	}
}

// validateIDTokenClaims 校验 id_token 的关键 claim：
// iss（必须等于配置的 issuer）、aud（须包含 client_id）、exp（未过期）、iat/nbf（合理）、nonce（记录但不强制）。
func (s *OAuth2Service) validateIDTokenClaims(cfg *OAuth2Config, claims map[string]interface{}) error {
	now := time.Now()

	// iss：必须等于配置的 issuer（未配置 issuer 时跳过，见下）
	if cfg.Issuer != "" {
		iss, _ := claims["iss"].(string)
		if !strings.EqualFold(strings.TrimSpace(iss), strings.TrimSuffix(cfg.Issuer, "/")) {
			return errors.New("id_token 的 iss 与配置的 issuer 不一致")
		}
	}

	// aud：须包含 client_id
	if !checkAudience(claims["aud"], cfg.ClientID) {
		return errors.New("id_token 的 aud 未包含本服务 client_id")
	}

	// exp：必须存在且未过期（允许 60s 时钟漂移）
	exp, err := numericClaim(claims, "exp")
	if err != nil || now.Unix() >= exp+60 {
		return errors.New("id_token 已过期")
	}

	// nbf：若存在则在生效时间之前拒绝（允许 60s 时钟漂移）
	if _, exists := claims["nbf"]; exists {
		nbfVal, err := numericClaim(claims, "nbf")
		if err == nil && now.Unix() < nbfVal-60 {
			return errors.New("id_token 尚未生效")
		}
	}

	// iat：若存在且明显晚于当前时间，视为异常（允许 60s 时钟漂移）
	if _, exists := claims["iat"]; exists {
		iatVal, err := numericClaim(claims, "iat")
		if err == nil && iatVal > now.Unix()+60 {
			return errors.New("id_token 签发时间异常（iat 晚于当前时间）")
		}
	}

	// nonce：若 token 携带则记录；当前不支持 nonce 校验（授权请求未下发 nonce），不强制，
	// 仅记日志。nonce 的作用是绑定授权请求与 token，若后续启用需在 AuthorizeURL 生成并回传校验。
	if nonce, ok := claims["nonce"]; ok {
		utils.Warn("id_token 携带 nonce 但当前系统未启用 nonce 校验（仅记录）",
			utils.String("nonce", fmt.Sprintf("%v", nonce)))
	}
	return nil
}

// checkAudience 判断 aud 声明是否包含目标值（支持字符串或字符串数组两种格式）
func checkAudience(aud interface{}, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if s == want {
				return true
			}
		}
	}
	return false
}

// numericClaim 读取数值型 claim（int/float64/json.Number 均可）
func numericClaim(claims map[string]interface{}, key string) (int64, error) {
	v, ok := claims[key]
	if !ok {
		return 0, fmt.Errorf("缺少 %s", key)
	}
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case json.Number:
		return n.Int64()
	default:
		return 0, fmt.Errorf("%s 不是数值", key)
	}
}

// getJWKS 获取 JWKS 公钥集合（带内存缓存）。
//
// 缓存与失效策略：
//   - 成功缓存 TTL 10 分钟（jwksCacheTTL）。缓存有效期内直接命中，不发网络请求。
//   - TTL 过期后触发拉新；拉新成功即整体替换缓存。
//   - 拉新失败时：
//   - 若已有旧缓存，返回旧缓存（登录流程继续），并用 LastFetch 记录本次失败；
//     此后在退避窗口（jwksRefreshBackoff，含连续失败自动累积）内不再重复请求远端，
//     避免远端故障时高频重试；退避窗口过后再次尝试拉新。
//     注意：复用旧缓存仅意味着「签名校验仍按旧公钥进行」——若 id_token 由轮换后
//     的新私钥签发，会验签失败被拒（属预期）；绝不存在「绕过验签直接放行」的降级。
//   - 若无旧缓存，直接返回错误，登录被拒绝（fail closed）。
func (s *OAuth2Service) getJWKS(cfg *OAuth2Config) (*jwksSet, error) {
	s.verify.mu.Lock()
	defer s.verify.mu.Unlock()

	cached := s.verify.jwks
	now := time.Now()

	// 1. 缓存命中（TTL 内）
	if cached != nil && now.Sub(cached.FetchedAt) < jwksCacheTTL {
		return cached, nil
	}

	// 2. TTL 已过且在退避窗口内（上次拉新刚失败）：复用旧缓存，不重复请求远端
	if cached != nil && cached.ConsecFail > 0 && now.Sub(cached.LastFetch) < jwksRefreshBackoff {
		cached.ConsecFail++
		s.verify.lastErr = errors.New("JWKS 获取失败（退避期内复用旧缓存）")
		return cached, nil
	}

	// 3. 拉新
	fresh, err := s.fetchJWKS(cfg)
	if err != nil {
		if cached != nil {
			cached.LastFetch = now
			cached.ConsecFail++
			s.verify.lastErr = err
			return cached, nil
		}
		return nil, fmt.Errorf("获取 JWKS 失败: %w", err)
	}
	s.verify.jwks = fresh
	s.verify.lastErr = nil
	return fresh, nil
}

// RefreshJWKSNow 立即强制刷新 JWKS 缓存（供配置测试 / 手动触发使用）。
// 返回刷新后的密钥数量，失败时返回错误（此时清空旧缓存，避免继续使用可能已失效的密钥）。
func (s *OAuth2Service) RefreshJWKSNow(cfg *OAuth2Config) (int, error) {
	s.verify.mu.Lock()
	defer s.verify.mu.Unlock()
	fresh, err := s.fetchJWKS(cfg)
	if err != nil {
		s.verify.jwks = nil
		s.verify.lastErr = err
		return 0, err
	}
	s.verify.jwks = fresh
	s.verify.lastErr = nil
	return len(fresh.Keys), nil
}

// fetchJWKS 从 OIDC 发现的 jwks_uri 拉取并解析公钥集合。
func (s *OAuth2Service) fetchJWKS(cfg *OAuth2Config) (*jwksSet, error) {
	ep, err := s.resolveEndpoints(cfg)
	if err != nil {
		return nil, err
	}
	if ep.JWKSURI == "" {
		return nil, errors.New("未配置 jwks_uri（OIDC 发现未返回或未显式配置 user_info_url 时无法验签）")
	}
	req, err := http.NewRequest(http.MethodGet, ep.JWKSURI, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 JWKS 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient(cfg).Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 JWKS 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 JWKS 失败: HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Keys []*jwksKey `json:"keys"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 JWKS 失败: %w", err)
	}
	if len(parsed.Keys) == 0 {
		return nil, errors.New("JWKS 不含任何密钥")
	}
	return &jwksSet{Keys: parsed.Keys, FetchedAt: time.Now()}, nil
}

// mapUserInfo 按配置的声明字段从原始声明中提取用户信息
func (s *OAuth2Service) mapUserInfo(cfg *OAuth2Config, claims map[string]interface{}) (*oauthUserInfo, error) {
	str := func(key string) string {
		if key == "" {
			return ""
		}
		if v, ok := claims[key]; ok {
			if sv, ok := v.(string); ok {
				return strings.TrimSpace(sv)
			}
		}
		return ""
	}
	usernameClaim := cfg.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}
	displayClaim := cfg.DisplayNameClaim
	if displayClaim == "" {
		displayClaim = "name"
	}
	emailClaim := cfg.EmailClaim
	if emailClaim == "" {
		emailClaim = "email"
	}

	info := &oauthUserInfo{
		Subject:     str("sub"),
		Username:    str(usernameClaim),
		DisplayName: str(displayClaim),
		Email:       str(emailClaim),
	}
	// 用户名回退链：配置字段 → email 前缀 → sub
	if info.Username == "" {
		if info.Email != "" {
			info.Username = strings.SplitN(info.Email, "@", 2)[0]
		}
	}
	if info.Username == "" {
		info.Username = info.Subject
	}
	if info.Username == "" {
		return nil, errors.New("外部用户信息缺少可用于登录的用户名（请配置 username_claim）")
	}
	if info.DisplayName == "" {
		info.DisplayName = info.Username
	}
	if info.Subject == "" {
		info.Subject = info.Username
	}
	return info, nil
}

// Authenticate 用授权码完成认证，并映射为本地用户
func (s *OAuth2Service) Authenticate(code, redirectURL string) (*models.User, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("缺少授权码")
	}
	info, err := s.Exchange(code, redirectURL)
	if err != nil {
		return nil, err
	}
	return s.findOrCreateUser(info)
}

// providerName 返回用于 AuthRecord 的提供方标识
func (s *OAuth2Service) providerName(cfg *OAuth2Config) string {
	if cfg.ProviderName != "" {
		return "oauth2:" + cfg.ProviderName
	}
	if cfg.Issuer != "" {
		return "oauth2:" + cfg.Issuer
	}
	if u, err := url.Parse(cfg.AuthURL); err == nil && u.Host != "" {
		return "oauth2:" + u.Host
	}
	return "oauth2"
}

// findOrCreateUser 依据外部身份查找或创建本地用户，并维护认证关联记录
func (s *OAuth2Service) findOrCreateUser(info *oauthUserInfo) (*models.User, error) {
	cfg := s.GetConfig()
	provider := s.providerName(cfg)

	// 1. 已有关联记录：直接返回关联用户
	var record models.AuthRecord
	if err := s.db.Where("auth_type = ? AND external_id = ? AND provider_name = ?", "oauth2", info.Subject, provider).
		First(&record).Error; err == nil {
		var user models.User
		if err := s.db.Where("id = ?", record.UserID).First(&user).Error; err != nil {
			return nil, fmt.Errorf("关联用户不存在: %w", err)
		}
		if user.Disabled {
			return nil, errors.New("账户已被禁用")
		}
		return &user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询认证关联失败: %w", err)
	}

	username := sanitizeUsername(info.Username)

	// 2. 本地已存在同名用户：建立关联（视为账号绑定）
	var user models.User
	err := s.db.Where("username = ?", username).First(&user).Error
	if err == nil {
		if user.Disabled {
			return nil, errors.New("账户已被禁用")
		}
		s.linkRecord(&user, provider, info)
		return &user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	// 3. 本地不存在：按配置决定是否自动创建
	if !cfg.AutoCreateUser {
		return nil, errors.New("外部账号未关联本地用户，且未启用自动创建")
	}
	uuid, err := generateOAuthUUID()
	if err != nil {
		return nil, err
	}
	user = models.User{
		UUID:         uuid,
		Username:     s.uniqueUsername(username, info.Subject),
		PasswordHash: "",
		Role:         "user",
	}
	if err := s.db.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("创建本地用户失败: %w", err)
	}
	s.linkRecord(&user, provider, info)
	return &user, nil
}

// linkRecord 写入认证关联记录（同一用户重复登录时幂等跳过）
func (s *OAuth2Service) linkRecord(user *models.User, provider string, info *oauthUserInfo) {
	var existing models.AuthRecord
	if err := s.db.Where("user_id = ? AND auth_type = ? AND external_id = ? AND provider_name = ?",
		user.ID, "oauth2", info.Subject, provider).First(&existing).Error; err == nil {
		return
	}
	if err := s.db.Create(&models.AuthRecord{
		UserID:       user.ID,
		AuthType:     "oauth2",
		ExternalID:   info.Subject,
		ExternalName: info.DisplayName,
		ProviderName: provider,
	}).Error; err != nil {
		utils.Warn("写入 OAuth2 认证关联失败", utils.String("username", user.Username), utils.Err(err))
	}
}

// uniqueUsername 处理本地用户名冲突：同名但非同一外部身份时追加短标识
func (s *OAuth2Service) uniqueUsername(base, subject string) string {
	var count int64
	if err := s.db.Model(&models.User{}).Where("username = ?", base).Count(&count).Error; err != nil || count == 0 {
		return base
	}
	suffix := shortHash(subject)
	candidate := truncateUsername(base, 64-len(suffix)-1) + "_" + suffix
	return candidate
}

// sanitizeUsername 将外部用户名规整为符合本地约束（3-64 字符）的名称
func sanitizeUsername(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-', r == '@':
			return r
		default:
			return '_'
		}
	}, name)
	name = truncateUsername(name, 64)
	if len(name) < 3 {
		name = name + "_user"
		name = truncateUsername(name, 64)
	}
	return name
}

// truncateUsername 按字节上限截断用户名（避免截断多字节字符造成非法串）
func truncateUsername(name string, max int) string {
	if max <= 0 {
		return name
	}
	if len(name) <= max {
		return name
	}
	runes := []rune(name)
	for len(string(runes)) > max {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// shortHash 返回用于用户名去重的短哈希
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// generateOAuthUUID 生成 UUID v4
func generateOAuthUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成用户标识失败: %w", err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:]), nil
}

// TestConnection 测试 OAuth2 配置：校验凭据并解析端点（必要时执行 OIDC 发现）；
// 若配置了 issuer，额外拉取一次 JWKS 以确认 id_token 验签链路可用。
func (s *OAuth2Service) TestConnection() error {
	cfg := s.GetConfig()
	if cfg == nil {
		return errors.New("OAuth2 配置为空")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("Client ID / Client Secret 未配置")
	}
	if cfg.AuthURL == "" && cfg.Issuer == "" {
		return errors.New("授权端点与 Issuer 均未配置")
	}
	ep, err := s.resolveEndpoints(cfg)
	if err != nil {
		return err
	}
	if ep.AuthURL == "" || ep.TokenURL == "" {
		return errors.New("授权端点或令牌端点缺失")
	}
	// 配置了 issuer 才能拿到 jwks_uri 并校验；预拉一次缓存，早发现配置问题
	if ep.JWKSURI != "" {
		if _, err := s.RefreshJWKSNow(cfg); err != nil {
			return fmt.Errorf("JWKS 拉取失败（影响 id_token 验签）: %w", err)
		}
	}
	return nil
}
