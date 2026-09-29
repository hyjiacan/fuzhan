package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

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

// OAuth2Service 外部 OAuth2/OIDC 认证服务
type OAuth2Service struct {
	db  *gorm.DB
	cfg *OAuth2Config
	mu  sync.RWMutex
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

// httpClient 构造外部请求客户端，按配置决定是否跳过 TLS 校验
func (s *OAuth2Service) httpClient(cfg *OAuth2Config) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
	}
	return &http.Client{Timeout: oauth2HTTPTimeout, Transport: transport}
}

// endpoints 已解析的端点集合
type endpoints struct {
	AuthURL     string
	TokenURL    string
	UserInfoURL string
	Issuer      string
}

// resolveEndpoints 解析实际使用的端点：显式配置优先；缺失时用 OIDC 发现补齐。
func (s *OAuth2Service) resolveEndpoints(cfg *OAuth2Config) (*endpoints, error) {
	ep := &endpoints{AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL, UserInfoURL: cfg.UserInfoURL, Issuer: cfg.Issuer}
	if ep.AuthURL != "" && ep.TokenURL != "" && ep.UserInfoURL != "" {
		return ep, nil
	}
	if cfg.Issuer == "" {
		if ep.AuthURL == "" || ep.TokenURL == "" {
			return nil, errors.New("OAuth2 授权/令牌端点未配置（可配置 issuer 自动发现）")
		}
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
	if ep.UserInfoURL != "" {
		claims, err = s.fetchUserInfo(cfg, ep.UserInfoURL, tokenResp.AccessToken)
		if err != nil {
			return nil, err
		}
	}
	// 用户信息端点缺失时回退到 id_token 载荷（OIDC 常见做法，此处不做签名校验，
	// 仅用于取用户名；令牌本身已通过 HTTPS 从令牌端点取得）
	if len(claims) == 0 && tokenResp.IDToken != "" {
		claims = decodeIDTokenClaims(tokenResp.IDToken)
	}
	if len(claims) == 0 {
		return nil, errors.New("未能获取外部用户信息（请配置 user_info_url）")
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

// decodeIDTokenClaims 解析 id_token 的 JWT 载荷（不校验签名，仅提取声明）
func decodeIDTokenClaims(idToken string) map[string]interface{} {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
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

// TestConnection 测试 OAuth2 配置：校验凭据并解析端点（必要时执行 OIDC 发现）
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
	return nil
}
