package auth

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/asn1"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// LDAPConfig LDAP 认证配置
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

// UpdateConfig 热更新配置
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
	return cfg != nil && cfg.Enabled && cfg.Host != ""
}

// Authenticate 通过 LDAP 验证用户
func (s *LDAPService) Authenticate(username, password string) (*models.User, error) {
	cfg := s.GetConfig()
	if !cfg.Enabled {
		return nil, errors.New("LDAP 未启用")
	}
	if username == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}

	userDN := s.buildUserDN(username)

	attrs, err := s.ldapBind(userDN, password)
	if err != nil {
		return nil, fmt.Errorf("LDAP 认证失败: %w", err)
	}

	user, err := s.findOrCreateUser(username, attrs)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// buildUserDN 构建用户 DN
// 默认使用 uid= 作为 RDN 属性，如果 baseDN 包含 ou= 且没有 dc= 则使用 cn=
func (s *LDAPService) buildUserDN(username string) string {
	cfg := s.GetConfig()
	// 默认使用 uid= (最通用的 LDAP 命名属性)
	if strings.Contains(cfg.BaseDN, "dc=") {
		return fmt.Sprintf("uid=%s,%s", username, cfg.BaseDN)
	}
	if strings.Contains(cfg.BaseDN, "o=") {
		return fmt.Sprintf("uid=%s,%s", username, cfg.BaseDN)
	}
	if strings.Contains(cfg.BaseDN, "ou=") {
		return fmt.Sprintf("uid=%s,%s", username, cfg.BaseDN)
	}
	return fmt.Sprintf("cn=%s,%s", username, cfg.BaseDN)
}

// ldapBind 执行 LDAP 简单绑定认证
func (s *LDAPService) ldapBind(bindDN, password string) (map[string]string, error) {
	cfg := s.GetConfig()
	// net.JoinHostPort 自动处理 IPv6 地址（例如 [::1]:389）
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	var conn net.Conn
	var err error

	// 统一构造 TLS 配置：默认校验证书与主机名（防中间人），
	// 仅当显式配置 insecure_skip_verify=true 时跳过（自签名/测试环境）。
	newTLSConfig := func() *tls.Config {
		t := &tls.Config{ServerName: cfg.Host}
		t.InsecureSkipVerify = cfg.InsecureSkipVerify
		return t
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if cfg.UseSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, newTLSConfig())
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 服务器失败: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(15 * time.Second))

	if err := s.sendBindRequest(conn, bindDN, password); err != nil {
		return nil, err
	}

	if err := s.readBindResponse(conn); err != nil {
		return nil, err
	}

	attrs := make(map[string]string)
	parts := strings.SplitN(bindDN, ",", 2)
	if len(parts) > 0 {
		kv := strings.SplitN(parts[0], "=", 2)
		if len(kv) == 2 {
			attrs["uid"] = kv[1]
			attrs["cn"] = kv[1]
		}
	}

	return attrs, nil
}

// encodeASN1Length 编码 ASN.1 长度字段（支持多字节长度）
func encodeASN1Length(length int) []byte {
	if length < 128 {
		return []byte{byte(length)}
	}
	// 多字节长度：第一字节高位置1，低7位表示后续字节数
	var bytes []byte
	for length > 0 {
		bytes = append([]byte{byte(length & 0xff)}, bytes...)
		length >>= 8
	}
	return append([]byte{byte(0x80 | len(bytes))}, bytes...)
}

// sendBindRequest 发送 LDAP Bind 请求
func (s *LDAPService) sendBindRequest(conn net.Conn, bindDN, password string) error {
	messageID, _ := asn1.Marshal(int(1))
	version, _ := asn1.Marshal(int(3))
	name, _ := asn1.Marshal(bindDN)

	// 简单认证密码使用 context-specific tag [0] (0x80)
	pwdBytes := []byte(password)
	auth := []byte{0x80}
	auth = append(auth, encodeASN1Length(len(pwdBytes))...)
	auth = append(auth, pwdBytes...)

	// 构建 BindRequest (Application 0, tag 0x60, constructed)
	bindReqContent := append(version, name...)
	bindReqContent = append(bindReqContent, auth...)
	bindReq := append([]byte{0x60}, encodeASN1Length(len(bindReqContent))...)
	bindReq = append(bindReq, bindReqContent...)

	// LDAPMessage 序列
	ldapMsg := append([]byte{0x30}, encodeASN1Length(len(messageID)+len(bindReq))...)
	ldapMsg = append(ldapMsg, messageID...)
	ldapMsg = append(ldapMsg, bindReq...)

	_, err := conn.Write(ldapMsg)
	return err
}

// readBindResponse 读取 LDAP Bind 响应
func (s *LDAPService) readBindResponse(conn net.Conn) error {
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("读取 LDAP 响应失败: %w", err)
	}

	resp := buf[:n]

	// 查找 BindResponse 标签 (0x61 = Application 1, constructed)
	for i := 0; i < len(resp)-4; i++ {
		if resp[i] == 0x61 {
			// 在 BindResponse 内查找 resultCode (0x0a 01 00 = Integer 0 success)
			for j := i; j < len(resp)-2; j++ {
				if resp[j] == 0x0a && resp[j+1] == 0x01 {
					resultCode := int(resp[j+2])
					if resultCode == 0 {
						return nil
					}
					return fmt.Errorf("LDAP 认证失败 (错误码: %d)", resultCode)
				}
			}
		}
	}

	return errors.New("无法解析 LDAP 绑定结果")
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
	if !cfg.AutoCreateUser {
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
		ExternalName: username,
		ProviderName: host,
	})

	return &user, nil
}

// TestConnection 测试 LDAP 连接
func (s *LDAPService) TestConnection() error {
	cfg := s.GetConfig()
	// net.JoinHostPort 自动处理 IPv6 地址（例如 [::1]:389）
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	var conn net.Conn
	var err error

	// 见 ldapBind：默认校验证书与主机名，仅按配置跳过
	newTLSConfig := func() *tls.Config {
		t := &tls.Config{ServerName: cfg.Host}
		t.InsecureSkipVerify = cfg.InsecureSkipVerify
		return t
	}

	if cfg.UseSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, newTLSConfig())
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("连接 LDAP 服务器失败: %w", err)
	}
	conn.Close()
	return nil
}
