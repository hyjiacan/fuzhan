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
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"github.com/go-ldap/ldap/v3"
	"gorm.io/gorm"
)

// LDAPConfig LDAP 认证配置（与 fuzhan.yaml 的 auth.ldap 节点一一对应）
type LDAPConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	UseSSL       bool   `yaml:"use_ssl"`
	BaseDN       string `yaml:"base_dn"`
	BindDN       string `yaml:"bind_dn"`
	BindPassword string `yaml:"bind_password"`
	UserFilter   string `yaml:"user_filter"`
	// UsernameAttribute 本地用户名取值的目录属性；留空时按 user_filter 推导，回退 uid
	UsernameAttribute  string `yaml:"username_attribute"`
	SyncInterval       int    `yaml:"sync_interval"`
	AutoCreateUser     bool   `yaml:"auto_create_user"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// LDAPService LDAP 认证服务
type LDAPService struct {
	db   *gorm.DB
	cfg  *LDAPConfig
	mu   sync.RWMutex
	wake chan struct{} // 配置热更新时唤醒同步协程，立即按新配置执行一轮同步
}

// NewLDAPService 创建 LDAP 服务
func NewLDAPService(db *gorm.DB, cfg *LDAPConfig) *LDAPService {
	return &LDAPService{db: db, cfg: cfg, wake: make(chan struct{}, 1)}
}

// UpdateConfig 热更新配置（配置保存后由 main 注册的 OnSave 钩子调用）
func (s *LDAPService) UpdateConfig(cfg *LDAPConfig) {
	if cfg == nil {
		return
	}
	s.mu.Lock()
	changed := s.cfg == nil || *s.cfg != *cfg
	s.cfg = cfg
	s.mu.Unlock()
	if !changed || !cfg.Enabled || !cfg.AutoCreateUser {
		return
	}
	// 仅在 LDAP 相关配置变化且需要同步时非阻塞唤醒，避免任意配置保存都触发目录查询
	select {
	case s.wake <- struct{}{}:
	default:
	}
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
	// 端口未配置时按协议补默认值，避免拼出 host:0 连接失败
	port := cfg.Port
	if port <= 0 {
		port = 389
		if cfg.UseSSL {
			port = 636
		}
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	// 同时限制 TCP 建连和后续操作超时，避免目录不可达时请求长时间挂起
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var (
		conn *ldap.Conn
		err  error
	)
	if cfg.UseSSL {
		tlsCfg := &tls.Config{
			ServerName:         cfg.Host, // 默认校验证书与主机名，防中间人
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}
		conn, err = ldap.DialURL("ldaps://"+addr,
			ldap.DialWithDialer(dialer), ldap.DialWithTLSConfig(tlsCfg))
	} else {
		conn, err = ldap.DialURL("ldap://"+addr, ldap.DialWithDialer(dialer))
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
		return nil, fmt.Errorf("%w: %v", services.ErrLDAPInvalidCredentials, err)
	}

	// 本地用户名以目录中的用户名属性为准，属性缺失时回退登录输入值
	return s.findOrCreateUser(firstNonEmpty(attrs["username"], username), attrs)
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
		s.entryAttributeList(cfg),
		nil,
	)
	sr, err := conn.Search(req)
	if err != nil {
		return "", nil, fmt.Errorf("LDAP 目录查询失败: %w", err)
	}
	if len(sr.Entries) == 0 {
		return "", nil, fmt.Errorf("%w: 未在 LDAP 目录中找到用户 %s", services.ErrLDAPInvalidCredentials, username)
	}
	nameAttr := s.usernameAttribute(cfg)
	if len(sr.Entries) > 1 {
		// 目录里存在同名条目（如个人账户与同名测试/迁移账户并存）时，
		// 依次按候选用户名属性挑出唯一条目，仍无法消歧才报错并列出候选 DN 便于定位
		for _, attr := range s.usernameAttributeCandidates(cfg) {
			if entry := pickUniqueEntry(sr.Entries, attr, username); entry != nil {
				utils.Info("LDAP 登录：多条匹配记录已按属性消歧",
					utils.String("username", username), utils.String("attribute", attr),
					utils.String("dn", entry.DN))
				return entry.DN, entryAttributes(entry, nameAttr), nil
			}
		}
		return "", nil, fmt.Errorf("LDAP 目录中存在 %d 条匹配记录，请细化 user_filter 或配置 username_attribute（当前 base_dn=%q，过滤器 %s）；候选 DN: %s",
			len(sr.Entries), cfg.BaseDN, filter, joinEntryDNs(sr.Entries, 5))
	}

	entry := sr.Entries[0]
	attrs := entryAttributes(entry, nameAttr)
	return entry.DN, attrs, nil
}

// pickUniqueEntry 在多个检索结果中按用户名属性挑出唯一条目：
// 先要求属性值完全相等，再退化为忽略大小写相等；任一阶段命中多条都视为无法消歧。
func pickUniqueEntry(entries []*ldap.Entry, attr, username string) *ldap.Entry {
	pick := func(equal func(a, b string) bool) *ldap.Entry {
		var hit *ldap.Entry
		for _, e := range entries {
			if !equal(e.GetAttributeValue(attr), username) {
				continue
			}
			if hit != nil {
				return nil
			}
			hit = e
		}
		return hit
	}
	if hit := pick(func(a, b string) bool { return a == b }); hit != nil {
		return hit
	}
	return pick(strings.EqualFold)
}

// joinEntryDNs 拼接候选条目 DN，便于在错误信息中直接定位重复数据
func joinEntryDNs(entries []*ldap.Entry, limit int) string {
	dns := make([]string, 0, len(entries))
	for i, e := range entries {
		if i >= limit {
			dns = append(dns, "...")
			break
		}
		dns = append(dns, e.DN)
	}
	return strings.Join(dns, ", ")
}

// entryAttributeList 返回检索需要拉取的属性列表，确保包含当前用户名属性
func (s *LDAPService) entryAttributeList(cfg *LDAPConfig) []string {
	attrs := []string{"dn", "uid", "cn", "sAMAccountName", "mail", "displayName", "givenName", "sn"}
	nameAttr := s.usernameAttribute(cfg)
	for _, a := range attrs {
		if strings.EqualFold(a, nameAttr) {
			return attrs
		}
	}
	return append(attrs, nameAttr)
}

// entryAttributes 提取用户条目的关键属性用于本地建号与认证记录
// usernameAttr 为本地用户名取值属性（见 usernameAttribute）
func entryAttributes(entry *ldap.Entry, usernameAttr string) map[string]string {
	attrs := map[string]string{
		"dn":             entry.DN,
		"uid":            entry.GetAttributeValue("uid"),
		"cn":             entry.GetAttributeValue("cn"),
		"sAMAccountName": entry.GetAttributeValue("sAMAccountName"),
		"mail":           entry.GetAttributeValue("mail"),
		"displayName":    entry.GetAttributeValue("displayName"),
		"givenName":      entry.GetAttributeValue("givenName"),
		"sn":             entry.GetAttributeValue("sn"),
	}
	if attrs["cn"] == "" {
		attrs["cn"] = attrs["sAMAccountName"]
	}
	// 姓名回退链：displayName → cn → givenName+sn；sAMAccountName 是登录名，不作为姓名
	attrs["name"] = firstNonEmpty(
		attrs["displayName"],
		attrs["cn"],
		strings.TrimSpace(attrs["givenName"]+" "+attrs["sn"]),
		attrs["uid"],
	)
	// 用户名回退链：配置的用户名属性 → uid → sAMAccountName → mail
	attrs["username"] = firstNonEmpty(
		entry.GetAttributeValue(usernameAttr),
		attrs["uid"],
		attrs["sAMAccountName"],
		attrs["mail"],
	)
	return attrs
}

// buildUserFilter 构造用户检索过滤器
//   - 未配置 user_filter 时默认按用户名属性过滤（默认为 (uid=%s)），配置了 username_attribute 则用它
//   - 配置了 user_filter 且含 %s 时，将 %s 替换为转义后的用户名（如 (sAMAccountName=%s) 适配 AD）
//   - 配置了 user_filter 但未含 %s 时按静态过滤器使用（此时需保证过滤结果唯一）
func (s *LDAPService) buildUserFilter(cfg *LDAPConfig, username string) string {
	escaped := ldap.EscapeFilter(username)
	f := strings.TrimSpace(cfg.UserFilter)
	if f == "" {
		return fmt.Sprintf("(%s=%s)", s.usernameAttribute(cfg), escaped)
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
		if user.Disabled {
			return nil, services.ErrAccountDisabled
		}
		// 历史用户可能没有姓名，登录时用目录中的姓名补齐
		s.fillDisplayName(&user, attrs)
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
		DisplayName:  attrs["name"],
		PasswordHash: "",
		Role:         "user",
	}

	if err := s.db.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("创建本地用户失败: %w", err)
	}

	if err := s.db.Create(&models.AuthRecord{
		UserID:       user.ID,
		AuthType:     "ldap",
		ExternalID:   attrs["dn"],
		ExternalName: firstNonEmpty(attrs["displayName"], attrs["cn"], attrs["name"], attrs["uid"], username),
		ProviderName: host,
	}).Error; err != nil {
		// 关联记录失败不影响本次登录（用户已创建），但需记录以便排查
		utils.Warn("写入 LDAP 认证关联失败",
			utils.String("username", username), utils.Err(err))
	}

	return &user, nil
}

// fillDisplayName 在本地用户缺少姓名时用目录属性补齐，失败只告警不阻断登录
func (s *LDAPService) fillDisplayName(user *models.User, attrs map[string]string) {
	if user.DisplayName != "" || attrs["name"] == "" {
		return
	}
	if err := s.db.Model(user).Update("display_name", attrs["name"]).Error; err != nil {
		utils.Warn("LDAP 用户姓名补写失败",
			utils.String("username", user.Username), utils.Err(err))
		return
	}
	user.DisplayName = attrs["name"]
}

// firstNonEmpty 返回第一个非空字符串，全部为空时返回空串
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
	// 配置了 Base DN 时做一次 base 范围查询，验证目录存在且账号有读取权限，
	// 避免仅连接成功但目录配置错误仍提示“连接成功”。
	if cfg.BaseDN != "" {
		req := ldap.NewSearchRequest(
			cfg.BaseDN,
			ldap.ScopeBaseObject,
			ldap.NeverDerefAliases,
			1, 0, false,
			"(objectClass=*)",
			[]string{"dn"},
			nil,
		)
		if _, err := conn.Search(req); err != nil {
			return fmt.Errorf("Base DN 校验失败: %w", err)
		}
	}
	return nil
}

// usernameAttribute 本地用户名取值的目录属性。
// 优先使用显式配置的 username_attribute；未配置时从 user_filter 推导（默认 uid），
// 仅支持简单等值过滤器形如 (uid=%s) / (sAMAccountName=%s) 的推导；其它复杂过滤器回退 uid。
func (s *LDAPService) usernameAttribute(cfg *LDAPConfig) string {
	if cfg == nil {
		return "uid"
	}
	if attr := strings.TrimSpace(cfg.UsernameAttribute); attr != "" {
		return attr
	}
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

// usernameAttributeCandidates 返回多条匹配记录时用于消歧的属性候选顺序：
// 优先当前用户名属性，其次是常见的 uid / sAMAccountName / mail，去重并保持顺序。
func (s *LDAPService) usernameAttributeCandidates(cfg *LDAPConfig) []string {
	attrs := []string{s.usernameAttribute(cfg), "uid", "sAMAccountName", "mail"}
	seen := make(map[string]bool, len(attrs))
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		key := strings.ToLower(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// syncFilter 构造目录同步的枚举过滤器：将模板中的 %s 替换为 * 以遍历全部用户
func (s *LDAPService) syncFilter(cfg *LDAPConfig) string {
	f := strings.TrimSpace(cfg.UserFilter)
	if f == "" {
		return "(" + s.usernameAttribute(cfg) + "=*)"
	}
	if strings.Contains(f, "%s") {
		return strings.ReplaceAll(f, "%s", "*")
	}
	if strings.HasPrefix(f, "(") {
		return f
	}
	return "(" + s.usernameAttribute(cfg) + "=*)"
}

// SyncUsers 从 LDAP 目录同步用户到本地，仅创建本地缺失的账户。
// 未启用自动创建时没有同步意义，直接返回 0；已在本地存在的用户不会重复创建。
func (s *LDAPService) SyncUsers() (int, error) {
	cfg := s.GetConfig()
	if cfg == nil || !s.IsEnabled() {
		return 0, errors.New("LDAP 未启用或配置不完整")
	}
	if !cfg.AutoCreateUser {
		return 0, nil
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
		s.entryAttributeList(cfg),
		nil,
	)
	// 使用分页查询，避免目录条目超过服务端单次返回上限（如 AD 默认 1000 条）时同步被截断
	sr, err := conn.SearchWithPaging(req, 500)
	if err != nil {
		return 0, fmt.Errorf("LDAP 目录同步查询失败: %w", err)
	}

	created := 0
	failed := 0
	for _, entry := range sr.Entries {
		username := entry.GetAttributeValue(usernameAttr)
		attrs := entryAttributes(entry, usernameAttr)
		if username == "" {
			username = attrs["username"]
		}
		if username == "" {
			continue
		}
		// 先判断本地是否已存在：同步只负责补建缺失账户，避免把“已存在”计为新建
		var existing models.User
		err := s.db.Where("username = ?", username).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			failed++
			utils.Warn("LDAP 目录同步：查询本地用户失败",
				utils.String("username", username), utils.Err(err))
			continue
		}
		if err == nil {
			// 已存在的用户不回补建，但在姓名缺失时用目录属性补齐
			s.fillDisplayName(&existing, attrs)
			continue
		}
		if _, err := s.findOrCreateUser(username, attrs); err != nil {
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

// StartSyncWorker 按 sync_interval 周期启动目录同步协程，直到 ctx 结束。
// 启动及配置保存后立即执行一轮同步，之后按间隔循环；未启用或未开启自动创建时跳过。
func (s *LDAPService) StartSyncWorker(ctx context.Context) {
	go func() {
		for {
			cfg := s.GetConfig()
			if cfg != nil && cfg.Enabled && cfg.AutoCreateUser {
				created, err := s.SyncUsers()
				if err != nil {
					utils.Warn("LDAP 目录同步失败", utils.Err(err))
				} else {
					utils.Info("LDAP 目录同步完成", utils.Int("created", created))
				}
			}

			interval := 3600 * time.Second
			if cfg != nil && cfg.SyncInterval > 0 {
				interval = time.Duration(cfg.SyncInterval) * time.Second
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			case <-s.wake:
				// 配置已热更新，立即进入下一轮，按新配置同步
				timer.Stop()
			}
		}
	}()
}
