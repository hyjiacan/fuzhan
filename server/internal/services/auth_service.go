package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ErrAccountDisabled 账户被禁用（少数可直接展示给用户的认证失败原因）
var ErrAccountDisabled = errors.New("账户已被禁用")

// ErrInvalidCredentials 凭据错误：账号不存在、密码错误、认证源不匹配等统一使用该提示，
// 不向调用方暴露账号是否存在或归属哪种认证源。
var ErrInvalidCredentials = errors.New("用户名或密码错误")

// ErrAccountUnavailable 账号当前不可用：认证源停用、身份冲突、数据异常等统一使用该提示。
var ErrAccountUnavailable = errors.New("账号当前不可用，请联系管理员")

// ErrLDAPInvalidCredentials LDAP 明确拒绝凭据（用户不存在或密码错误），属正常登录失败
var ErrLDAPInvalidCredentials = errors.New("LDAP 凭据无效")

// 登录方式（login_logs.method）：成功记真实认证来源，失败记登录入口。
const (
	LoginMethodLocal        = "local"
	LoginMethodLDAP         = "ldap"
	LoginMethodOAuth2       = "oauth2"
	LoginMethodPasswordForm = "password_form"
	LoginMethodRegister     = "register"
)

// LDAPAuthenticator LDAP 认证接口
// 避免 services 和 internal/auth 之间的循环依赖
type LDAPAuthenticator interface {
	IsEnabled() bool
	// AllowsAutoCreate 是否允许在本地无同名账号时 JIT 建号
	AllowsAutoCreate() bool
	Authenticate(username, password string) (*models.User, error)
}

// AuthService 认证服务
type AuthService struct {
	db          *gorm.DB
	ldapService LDAPAuthenticator
}

// NewAuthService 创建认证服务实例
func NewAuthService(db *gorm.DB) *AuthService {
	return &AuthService{db: db}
}

// SetLDAPService 设置 LDAP 认证服务
func (s *AuthService) SetLDAPService(ldap LDAPAuthenticator) {
	s.ldapService = ldap
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username      string `json:"username" binding:"required,min=3,max=32"`
	Password      string `json:"password" binding:"required,min=8,max=128"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username      string `json:"username" binding:"required,max=64"`
	Password      string `json:"password" binding:"required,max=128"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

// authTokenTTLSeconds JWT 令牌有效期（秒），与 Register/Login 生成令牌的 Duration 保持一致
const authTokenTTLSeconds = 86400

// AuthResponse 认证响应
type AuthResponse struct {
	Token       string `json:"token"`
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Nickname    string `json:"nickname"`
	ExpiresIn   int    `json:"expires_in"`
}

// Register 用户注册
func (s *AuthService) Register(req *RegisterRequest) (*AuthResponse, error) {
	// 注册功能仅由「允许注册」开关控制（与私有存储启用状态解耦）
	if !appconfig.GlobalConfig.Account.AllowRegistration {
		return nil, errors.New("注册功能已关闭")
	}

	for _, reserved := range appconfig.GetReservedUsernames() {
		if strings.EqualFold(req.Username, reserved) {
			return nil, errors.New("该用户名不允许注册")
		}
	}

	var existingUser models.User
	if err := s.db.Where("username = ?", req.Username).First(&existingUser).Error; err == nil {
		return nil, errors.New("用户名已存在")
	}
	// 大小写不敏感兜底：避免 ZhangSan 与 zhangsan 被创建成两个账号
	if err := s.db.Where("LOWER(username) = ?", strings.ToLower(req.Username)).First(&existingUser).Error; err == nil {
		return nil, errors.New("用户名已存在")
	}

	uuid, err := generateUUID()
	if err != nil {
		return nil, fmt.Errorf("生成UUID失败: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	user := &models.User{
		UUID:         uuid,
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
		AuthSource:   models.AuthSourceLocal,
		CreatedVia:   models.CreatedViaSelfRegister,
	}

	if err := s.db.Create(user).Error; err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	token, err := jwt.GenerateJWT(uuid, req.Username, "user", false)
	if err != nil {
		return nil, fmt.Errorf("生成令牌失败: %w", err)
	}

	return &AuthResponse{
		Token:       token,
		UUID:        uuid,
		Username:    req.Username,
		DisplayName: user.DisplayName,
		Nickname:    user.Nickname,
		ExpiresIn:   authTokenTTLSeconds,
	}, nil
}

// LoginByPassword 验证用户名密码（共享认证逻辑，WebDAV/FTP/Web API 共用）。
// 认证路径由账号的 auth_source 决定：本地账号只校验本地密码，外部账号只走对应认证源，
// 不做「先试 LDAP、失败回退本地密码」的机会式认证。
func (s *AuthService) LoginByPassword(username, password string) (*models.User, error) {
	user, _, err := s.loginByPassword(username, password)
	return user, err
}

// loginByPassword 执行严格路由的密码登录，并返回实际使用的认证来源（local / ldap）。
func (s *AuthService) loginByPassword(username, password string) (*models.User, string, error) {
	username = strings.TrimSpace(username)
	user, err := s.findUserByUsername(username)
	if err != nil {
		return nil, "", err
	}

	// 已存在本地账号：按 auth_source 路由，认证源不匹配一律拒绝
	if user != nil {
		switch user.AuthSource {
		case "", models.AuthSourceLocal:
			if user.PasswordHash == "" {
				// 本地账号缺密码属数据异常，按不可用处理，不尝试其它认证源
				return nil, "", ErrAccountUnavailable
			}
			if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
				return nil, "", ErrInvalidCredentials
			}
			if user.Disabled {
				return nil, "", ErrAccountDisabled
			}
			return user, LoginMethodLocal, nil

		case models.AuthSourceLDAP:
			// 认证源停用或配置不完整时直接失败，不回退本地密码
			if s.ldapService == nil || !s.ldapService.IsEnabled() {
				return nil, "", ErrAccountUnavailable
			}
			return s.authenticateLDAP(username, password)

		case models.AuthSourceOAuth2:
			// 外部账号不允许密码登录，且不暴露账号所属认证源
			return nil, "", ErrInvalidCredentials

		default:
			return nil, "", ErrAccountUnavailable
		}
	}

	// 本地不存在：仅在 LDAP 启用且允许自动建号时尝试目录认证（成功后 JIT 建号）
	if s.ldapService != nil && s.ldapService.IsEnabled() && s.ldapService.AllowsAutoCreate() {
		return s.authenticateLDAP(username, password)
	}
	return nil, "", ErrInvalidCredentials
}

// authenticateLDAP 执行目录认证并把错误收敛为对外通用语义。
func (s *AuthService) authenticateLDAP(username, password string) (*models.User, string, error) {
	user, err := s.ldapService.Authenticate(username, password)
	if err != nil {
		return nil, "", normalizeLDAPLoginError(err)
	}
	if user.Disabled {
		return nil, "", ErrAccountDisabled
	}
	return user, LoginMethodLDAP, nil
}

// normalizeLDAPLoginError 将 LDAP 认证错误映射为对外提示：
// 凭据错误保持通用失败；目录不可用、身份冲突等统一为「账号当前不可用」。
func normalizeLDAPLoginError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrAccountDisabled):
		return ErrAccountDisabled
	case errors.Is(err, ErrLDAPInvalidCredentials):
		return ErrInvalidCredentials
	default:
		utils.Warn("LDAP 认证源不可用", utils.Err(err))
		return ErrAccountUnavailable
	}
}

// findUserByUsername 按用户名查找本地用户：先精确匹配，再按大小写不敏感匹配。
// 未命中返回 (nil, nil)，数据库错误才返回 error。
func (s *AuthService) findUserByUsername(username string) (*models.User, error) {
	var user models.User
	err := s.db.Where("username = ?", username).First(&user).Error
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if err := s.db.Where("LOWER(username) = ?", strings.ToLower(username)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	return &user, nil
}

// UserFacingLoginError 返回可展示给用户的登录失败提示。
// 只保留「禁用」「账号不可用」两类明确语义，其余（凭据错误、认证源细节、内部错误）统一为通用提示。
func UserFacingLoginError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrAccountUnavailable):
		return ErrAccountUnavailable.Error()
	case errors.Is(err, ErrAccountDisabled):
		return ErrAccountDisabled.Error()
	default:
		return ErrInvalidCredentials.Error()
	}
}

// IsAdminUsername 判断用户名是否为管理员账号。
// 供认证失败定级使用：管理员账号属于高价值身份，单次失败即升级为安全事件。
func (s *AuthService) IsAdminUsername(username string) bool {
	username = strings.TrimSpace(username)
	if username == "" || s.db == nil {
		return false
	}
	var count int64
	if err := s.db.Model(&models.User{}).
		Where("username = ? AND role = ?", username, "admin").
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// Login 用户登录，返回会话、实际认证来源（local/ldap）与错误。
// 调用方据此把真实来源写入 login_logs.method，避免统一记成 local。
func (s *AuthService) Login(req *LoginRequest) (*AuthResponse, string, error) {
	user, method, err := s.loginByPassword(req.Username, req.Password)
	if err != nil {
		return nil, "", err
	}

	role := user.Role
	if role == "" {
		role = "user"
	}
	token, err := jwt.GenerateJWT(user.UUID, user.Username, role, user.Disabled)
	if err != nil {
		return nil, "", fmt.Errorf("生成令牌失败: %w", err)
	}

	return &AuthResponse{
		Token:       token,
		UUID:        user.UUID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Nickname:    user.Nickname,
		ExpiresIn:   authTokenTTLSeconds,
	}, method, nil
}

// LoginByUser 为已完成外部认证（OAuth2/OIDC）的用户签发本地令牌
func (s *AuthService) LoginByUser(user *models.User) (*AuthResponse, error) {
	if user == nil {
		return nil, errors.New("用户信息为空")
	}
	if user.Disabled {
		return nil, ErrAccountDisabled
	}

	role := user.Role
	if role == "" {
		role = "user"
	}
	token, err := jwt.GenerateJWT(user.UUID, user.Username, role, user.Disabled)
	if err != nil {
		return nil, fmt.Errorf("生成令牌失败: %w", err)
	}
	return &AuthResponse{
		Token:       token,
		UUID:        user.UUID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Nickname:    user.Nickname,
		ExpiresIn:   authTokenTTLSeconds,
	}, nil
}

// GetCurrentUser 获取当前用户信息
func (s *AuthService) GetCurrentUser(c *gin.Context) (*models.User, error) {
	uuidInterface, exists := c.Get("userUUID")
	if !exists {
		return nil, errors.New("用户未认证")
	}

	uuid, ok := uuidInterface.(string)
	if !ok {
		return nil, errors.New("无效的用户信息")
	}

	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	return &user, nil
}

// ChangePassword 修改用户密码
func (s *AuthService) ChangePassword(uuid, oldPassword, newPassword string) error {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}

	// 仅本地账号支持修改密码；外部账号凭证由认证源管理
	if user.AuthSource != "" && user.AuthSource != models.AuthSourceLocal {
		return errors.New("该账号使用外部认证，无法修改密码")
	}
	if user.PasswordHash == "" {
		return errors.New("该账号未设置本地密码")
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("当前密码错误")
	}

	// 验证新密码长度
	if len(newPassword) < 8 {
		return errors.New("新密码至少需要8位")
	}

	// 更新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}

	user.PasswordHash = string(hashedPassword)
	if err := s.db.Save(&user).Error; err != nil {
		return fmt.Errorf("保存密码失败: %w", err)
	}

	return nil
}

// DeleteAccount 自助注销当前账号：校验密码后清除该用户的私有/临时文件与关联数据，
// 最后删除用户行。登录日志与操作记录作为审计数据保留。
func (s *AuthService) DeleteAccount(uuid, password string) error {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}

	if user.Role == "admin" {
		return errors.New("管理员账号不支持自助注销")
	}

	// 外部账号没有本地密码，无法二次确认身份
	if user.AuthSource != "" && user.AuthSource != models.AuthSourceLocal {
		return errors.New("该账号使用外部认证，无法自助注销")
	}
	if user.PasswordHash == "" {
		return errors.New("该账号未设置本地密码")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return errors.New("密码错误")
	}

	if err := CleanupUserAssets(s.db, &user); err != nil {
		return err
	}
	if err := s.db.Delete(&user).Error; err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}
	utils.Info("用户已注销", utils.String("username", user.Username), utils.String("uuid", user.UUID))
	return nil
}

// UpdateProfile 更新当前用户资料（姓名、昵称）；用户名一经创建不可修改
func (s *AuthService) UpdateProfile(uuid, displayName, nickname string) (*models.User, error) {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	displayName = strings.TrimSpace(displayName)
	nickname = strings.TrimSpace(nickname)

	if len([]rune(displayName)) > 64 {
		return nil, errors.New("姓名过长")
	}
	if len([]rune(nickname)) > 32 {
		return nil, errors.New("昵称过长")
	}

	user.DisplayName = displayName
	user.Nickname = nickname

	if err := s.db.Save(&user).Error; err != nil {
		return nil, fmt.Errorf("保存用户资料失败: %w", err)
	}

	return &user, nil
}

// RefreshTokenIfNeeded 检查 token 是否快过期，如果是则返回新 token
func (s *AuthService) RefreshTokenIfNeeded(c *gin.Context) (string, error) {
	uuidInterface, exists := c.Get("userUUID")
	if !exists {
		return "", nil
	}

	uuid, ok := uuidInterface.(string)
	if !ok {
		return "", nil
	}

	tokenString := c.GetHeader("Authorization")
	if tokenString == "" {
		return "", nil
	}
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	claims, err := jwt.ParseJWT(tokenString)
	if err != nil {
		return "", nil
	}

	refreshThreshold := 6 * time.Hour
	if time.Until(claims.ExpiresAt.Time) > refreshThreshold {
		return "", nil
	}

	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		return "", nil
	}

	newToken, err := jwt.GenerateJWTWithExpiry(uuid, user.Username, user.Role, user.Disabled, 7*24*time.Hour)
	if err != nil {
		return "", err
	}

	return newToken, nil
}

// generateUUID 生成UUID v4
func generateUUID() (string, error) {
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		return "", err
	}

	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}
