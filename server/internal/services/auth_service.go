package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/pkg/jwt"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// LDAPAuthenticator LDAP 认证接口
// 避免 services 和 internal/auth 之间的循环依赖
type LDAPAuthenticator interface {
	IsEnabled() bool
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
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AuthResponse 认证响应
type AuthResponse struct {
	Token    string `json:"token"`
	UUID     string `json:"uuid"`
	Username string `json:"username"`
}

// Register 用户注册
func (s *AuthService) Register(req *RegisterRequest) (*AuthResponse, error) {
	// 独立注册开关优先检查（与私有存储启用状态解耦）
	if !appconfig.GlobalConfig.Account.AllowRegistration {
		return nil, errors.New("注册功能已关闭")
	}

	if !appconfig.GlobalConfig.Storage.Private.Enabled {
		return nil, errors.New("私有存储未启用，暂不支持注册")
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
	}

	if err := s.db.Create(user).Error; err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	token, err := jwt.GenerateJWT(uuid, req.Username, "user", false)
	if err != nil {
		return nil, fmt.Errorf("生成令牌失败: %w", err)
	}

	return &AuthResponse{
		Token:    token,
		UUID:     uuid,
		Username: req.Username,
	}, nil
}

// LoginByPassword 验证用户名密码（共享认证逻辑，WebDAV/FTP/Web API 共用）
// 支持认证链：LDAP → 本地密码
func (s *AuthService) LoginByPassword(username, password string) (*models.User, error) {
	// 第一步：尝试 LDAP 认证（如果启用）
	if s.ldapService != nil && s.ldapService.IsEnabled() {
		user, err := s.ldapService.Authenticate(username, password)
		if err == nil {
			return user, nil
		}
		// LDAP 失败时记录日志并降级到本地认证
		// 不直接返回错误，给本地认证机会
	}

	// 第二步：本地密码认证
	var user models.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	// LDAP 用户（无本地密码）不能通过本地密码登录
	if user.PasswordHash == "" {
		return nil, errors.New("该用户需要通过 LDAP 认证")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("用户名或密码错误")
	}

	return &user, nil
}

// Login 用户登录
func (s *AuthService) Login(req *LoginRequest) (*AuthResponse, error) {
	user, err := s.LoginByPassword(req.Username, req.Password)
	if err != nil {
		return nil, err
	}

	if !appconfig.GlobalConfig.Storage.Private.Enabled && user.Role != "admin" {
		return nil, errors.New("私有存储未启用，仅管理员可登录")
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
		Token:    token,
		UUID:     user.UUID,
		Username: user.Username,
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

	// LDAP 用户不允许修改密码
	if user.PasswordHash == "" {
		return errors.New("该用户通过 LDAP 认证，无法修改密码")
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("当前密码错误")
	}

	// 验证新密码长度
	if len(newPassword) < 6 {
		return errors.New("新密码至少需要6位")
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
