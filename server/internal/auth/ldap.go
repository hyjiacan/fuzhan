package auth

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/go-ldap/ldap/v3"
	"gorm.io/gorm"
)

// LDAPConfig LDAP 认证配置（与 fuzhan.yaml 的 auth.ldap 节点一一对应）
type LDAPConfig struct {
	Enabled            bool   `yaml:"enabled"`
	Host               string `yaml:"host"`
	Port               int    `yaml:"port"`
	UseSSL             bool   `yaml:"use_ssl"`
	BaseDN             string `yaml:"base_dn"`
	BindDN             string `yaml:"bind_dn"`
	BindPassword       string `yaml:"bind_password"`
	UserFilter         string `yaml:"user_filter"`
	SyncInterval       int    `yaml:"sync_interval"`
	AutoCreateUser     bool   `yaml:"auto_create_user"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// LDAPService LDAP 认证服务
type LDAPService struct {
	db  *gorm.DB
	cfg *LDAPConfig
	mu  sync.RWMutex
}

// NewLDAPService 创建 LDAP 服务
func NewLDAPService(db *gorm.DB, cfg *LDAPConfig) *LDAPService {
	return &LDAPService{db: db, cfg: cfg}
}

// UpdateConfig 热更新配置（配置保存后由 main 注册的 OnSave 钩子调用）
func (s *LDAPService) UpdateConfig(cfg *LDAPConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// GetConfig 获取当前配置
func (s *LDAPService) GetConfig() *LDAPConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// IsEnabled 检查 LDAP 是否启用
func (s *LDAPService) IsEnabled() bool {
	cfg := s.GetConfig()
	return cfg != nil && cfg.Enabled && cfg.Host != "" && cfg.BaseDN != ""
}

// dial 建立到 LDAP 服务器的连接（LDAPS 时使用 TLS 配置），统一处理超时
func (s *LDAPService) dial(cfg *LDAPConfig) (*ldap.Conn, error) {
	if cfg == nil || cfg.Host == "" {
		return nil, errors.New("LDAP 服务器地址未配置")
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	var (
		conn *ldap.Conn
		err  error
	)
	if cfg.UseSSL {
		tlsCfg := &tls.Config{
			ServerName:         cfg.Host, // 默认校验证书与主机名，防中间人
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}
		conn, err = ldap.DialURL("ldaps://"+addr, ldap.DialWithTLSConfig(tlsCfg))
	} else {
		conn, err = ldap.DialURL("ldap://" + addr)
	}
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 服务器失败: %w", err)
	}
	conn.SetTimeout(15 * time.Second)
	return conn, nil
}

// Authenticate 通过 LDAP 验证用户并返回本地用户
// 流程：建立连接 → 用服务账号绑定查询真实用户 DN → 用该 DN 绑定用户密码验证 → 查找或创建本地用户
func (s *LDAPService) Authenticate(username, password string) (*models.User, error) {
	cfg := s.GetConfig()
	if cfg == nil || !cfg.Enabled {
		return nil, errors.New("LDAP 未启用")
	}
	if username == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}

	conn, err := s.dial(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	userDN, attrs, err := s.resolveUserDN(conn, cfg, username)
	if err != nil {
		return nil, err
	}

	// 用解析出的用户 DN 绑定密码进行验证
	if err := conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("LDAP 认证失败: %w", err)
	}

	return s.findOrCreateUser(username, attrs)
}

// resolveUserDN 用服务账号绑定并按住 user_filter 搜索，解析用户真实 DN 与属性
// 未配置服务账号时尝试匿名查询；查询失败时不再猜测 DN，直接报错避免误导
func (s *LDAPService) resolveUserDN(conn *ldap.Conn, cfg *LDAPConfig, username string) (string, map[string]string, error) {
	// 服务账号绑定（仅用于目录查询）。未配置时尝试匿名绑定，以兼容允许匿名搜索的目录。
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return "", nil, fmt.Errorf("LDAP 服务账号绑定失败: %w", err)
		}
	} else if err := conn.Bind("", ""); err != nil {
		// 匿名绑定失败不致命，部分目录仍需显式匿名绑定后搜索
		_ = err
	}

	filter := s.buildUserFilter(cfg, username)
	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, 0, false,
		filter,
		[]string{"dn", "uid", "cn", "sAMAccountName", "mail", "displayName"},
		nil,
	)
	sr, err := conn.Search(req)
	if err != nil {
		return "", nil, fmt.Errorf("LDAP 目录查询失败: %w", err)
	}
	if len(sr.Entries) == 0 {
		return "", nil, fmt.Errorf("未在 LDAP 目录中找到用户 %s", username)
	}
	if len(sr.Entries) > 1 {
		return "", nil, fmt.Errorf("LDAP 目录中存在多个匹配用户，请细化 user_filter")
	}

	entry := sr.Entries[0]
	attrs := entryAttributes(entry)
	return entry.DN, attrs, nil
}

// entryAttributes 提取用户条目的关键属性用于本地建号与认证记录
func entryAttributes(entry *ldap.Entry) map[string]string {
	attrs := map[string]string{
		"dn":             entry.DN,
		"uid":            entry.GetAttributeValue("uid"),
		"cn":             entry.GetAttributeValue("cn"),
		"sAMAccountName": entry.GetAttributeValue("sAMAccountName"),
		"mail":           entry.GetAttributeValue("mail"),
		"displayName":    entry.GetAttributeValue("displayName"),
	}
	if attrs["cn"] == "" {
		attrs["cn"] = attrs["sAMAccountName"]
	}
	return attrs
}

// buildUserFilter 构造用户检索过滤器
//   - 未配置 user_filter 时默认 (uid=%s)，适用于常见 OpenLDAP 目录
//   - 配置了 user_filter 且含 %s 时，将 %s 替换为转义后的用户名（如 (sAMAccountName=%s) 适配 AD）
//   - 配置了 user_filter 但未含 %s 时按静态过滤器使用（此时需保证过滤结果唯一）
func (s *LDAPService) buildUserFilter(cfg *LDAPConfig, username string) string {
	escaped := ldap.EscapeFilter(username)
	f := strings.TrimSpace(cfg.UserFilter)
	if f == "" {
		return fmt.Sprintf("(uid=%s)", escaped)
	}
	if strings.Contains(f, "%s") {
		return strings.ReplaceAll(f, "%s", escaped)
	}
	return f
}

// findOrCreateUser 查找或创建本地用户
func (s *LDAPService) findOrCreateUser(username string, attrs map[string]string) (*models.User, error) {
	var user models.User
	result := s.db.Where("username = ?", username).First(&user)

	if result.Error == nil {
		return &user, nil
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询用户失败: %w", result.Error)
	}

	cfg := s.GetConfig()
	if cfg == nil || !cfg.AutoCreateUser {
		return nil, errors.New("LDAP 用户不存在本地且未启用自动创建")
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		utils.Error("生成 LDAP 用户 UUID 失败: crypto/rand.Read", utils.Err(err))
		return nil, fmt.Errorf("生成用户标识失败: %w", err)
	}
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])

	host := cfg.Host

	user = models.User{
		UUID:         uuid,
		Username:     username,
		PasswordHash: "",
		Role:         "user",
	}

	if err := s.db.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("创建本地用户失败: %w", err)
	}

	s.db.Create(&models.AuthRecord{
		UserID:       user.ID,
		AuthType:     "ldap",
		ExternalID:   attrs["dn"],
		ExternalName: firstNonEmpty(attrs["displayName"], attrs["cn"], attrs["uid"], username),
		ProviderName: host,
	})

	return &user, nil
}

// firstNonEmpty 返回第一个非空字符串，均空时返回 fallback
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// TestConnection 测试 LDAP 连接：建立连接，若配置了服务账号则执行真实绑定
func (s *LDAPService) TestConnection() error {
	cfg := s.GetConfig()
	if cfg == nil || cfg.Host == "" {
		return errors.New("LDAP 服务器地址未配置")
	}
	conn, err := s.dial(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return fmt.Errorf("服务账号绑定失败: %w", err)
		}
	}
	return nil
}

// usernameAttribute 从 user_filter 推导用于本地用户名的目录属性（默认 uid）
// 仅支持简单等值过滤器形如 (uid=%s) / (sAMAccountName=%s) 的推导；其它复杂过滤器回退 uid。
func (s *LDAPService) usernameAttribute(cfg *LDAPConfig) string {
	f := strings.TrimSpace(cfg.UserFilter)
	if strings.HasPrefix(f, "(") && strings.Contains(f, "=") {
		inner := strings.Trim(f, "()")
		if !strings.ContainsAny(inner, "&|!") {
			if i := strings.Index(inner, "="); i > 0 {
				if attr := strings.TrimSpace(inner[:i]); attr != "" {
					return attr
				}
			}
		}
	}
	return "uid"
}

// syncFilter 构造目录同步的枚举过滤器：将模板中的 %s 替换为 * 以遍历全部用户
func (s *LDAPService) syncFilter(cfg *LDAPConfig) string {
	f := strings.TrimSpace(cfg.UserFilter)
	if f == "" {
		return "(uid=*)"
	}
	if strings.Contains(f, "%s") {
		return strings.ReplaceAll(f, "%s", "*")
	}
	if strings.HasPrefix(f, "(") {
		return f
	}
	return "(uid=*)"
}

// SyncUsers 从 LDAP 目录全量同步用户到本地（缺失则自动创建）
func (s *LDAPService) SyncUsers() (int, error) {
	cfg := s.GetConfig()
	if cfg == nil || !cfg.Enabled {
		return 0, errors.New("LDAP 未启用")
	}

	conn, err := s.dial(cfg)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return 0, fmt.Errorf("LDAP 服务账号绑定失败: %w", err)
		}
	}

	usernameAttr := s.usernameAttribute(cfg)
	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, 0, false,
		s.syncFilter(cfg),
		[]string{"dn", "uid", "cn", "sAMAccountName", "mail", "displayName"},
		nil,
	)
	sr, err := conn.Search(req)
	if err != nil {
		return 0, fmt.Errorf("LDAP 目录同步查询失败: %w", err)
	}

	created := 0
	failed := 0
	for _, entry := range sr.Entries {
		username := entry.GetAttributeValue(usernameAttr)
		if username == "" {
			continue
		}
		if _, err := s.findOrCreateUser(username, entryAttributes(entry)); err != nil {
			failed++
			utils.Warn("LDAP 目录同步：创建本地用户失败",
				utils.String("username", username), utils.Err(err))
			continue
		}
		created++
	}
	if failed > 0 {
		return created, fmt.Errorf("LDAP 目录同步完成，但 %d 个用户创建失败", failed)
	}
	return created, nil
}

// StartSyncWorker 按 sync_interval 周期启动目录同步协程，直到 ctx 结束
// 每个周期从 GetConfig 重新读取配置，支持热更新；未启用时静默跳过。
func (s *LDAPService) StartSyncWorker(ctx context.Context) {
	go func() {
		for {
			cfg := s.GetConfig()
			interval := time.Duration(cfg.SyncInterval) * time.Second
			if interval <= 0 {
				interval = 3600 * time.Second
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			cur := s.GetConfig()
			if cur == nil || !cur.Enabled {
				continue
			}
			created, err := s.SyncUsers()
			if err != nil {
				utils.Warn("LDAP 目录同步失败", utils.Err(err))
			} else {
				utils.Info("LDAP 目录同步完成", utils.Int("created", created))
			}
		}
	}()
}
