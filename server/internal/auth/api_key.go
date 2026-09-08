package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ApiKeyService API Key 管理服务
type ApiKeyService struct {
	db *gorm.DB
}

// NewApiKeyService 创建 API Key 服务
func NewApiKeyService(db *gorm.DB) *ApiKeyService {
	return &ApiKeyService{db: db}
}

// CreateApiKeyRequest 创建 API Key 请求
type CreateApiKeyRequest struct {
	Name      string `json:"name" binding:"required,max=128"`
	UserID    uint   `json:"userId" binding:"required"`
	Scopes    string `json:"scopes"`
	ExpiresIn int    `json:"expiresIn"` // 过期时间（秒），0 表示永不过期
}

// CreateApiKeyResponse 创建 API Key 响应
type CreateApiKeyResponse struct {
	ApiKey models.ApiKey `json:"apiKey"`
	RawKey string        `json:"rawKey,omitempty"` // 创建时返回明文密钥（仅一次）
}

// CreateApiKey 创建新的 API Key
func (s *ApiKeyService) CreateApiKey(req CreateApiKeyRequest) (*CreateApiKeyResponse, error) {
	keyID := generateKeyID()
	rawKey := generateRawKey()

	// 使用 bcrypt 对密钥进行密码学安全哈希存储
	hashedKey, err := hashApiKey(rawKey)
	if err != nil {
		utils.Error("创建 API Key 失败: 哈希错误", utils.String("name", req.Name), utils.Err(err))
		return nil, fmt.Errorf("创建 API Key 失败: %w", err)
	}

	now := utils.Now()
	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := now.Add(time.Duration(req.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	// 设置默认 scope
	scopes := req.Scopes
	if scopes == "" {
		scopes = "open_api:reader"
	}

	apiKey := models.ApiKey{
		KeyID:     keyID,
		ApiKey:    hashedKey,
		Name:      req.Name,
		UserID:    req.UserID,
		Scopes:    scopes,
		Status:    "active",
		ExpiresAt: expiresAt,
	}

	if err := s.db.Create(&apiKey).Error; err != nil {
		utils.Error("创建 API Key 失败", utils.String("name", req.Name), utils.Int("user_id", int(req.UserID)), utils.Err(err))
		return nil, fmt.Errorf("创建 API Key 失败: %w", err)
	}

	utils.Info("创建 API Key 成功", utils.String("name", req.Name), utils.String("key_id", keyID), utils.Int("user_id", int(req.UserID)), utils.String("scopes", scopes))
	return &CreateApiKeyResponse{
		ApiKey: apiKey,
		RawKey: fmt.Sprintf("%s:%s", keyID, rawKey), // keyID:rawKey 格式
	}, nil
}

// ValidateApiKey 验证 API Key 并返回对应的 scope
func (s *ApiKeyService) ValidateApiKey(rawKey string) (uint, string, error) {
	// 格式: keyID:rawSecret (keyID 为 8 字符 hex，rawSecret 为 64 字符 hex，加上冒号共 73 字符)
	if len(rawKey) < 73 || !strings.Contains(rawKey, ":") {
		utils.Warn("API Key 认证失败: 无效的格式", utils.String("key_length", fmt.Sprintf("%d", len(rawKey))))
		return 0, "", fmt.Errorf("无效的 API Key 格式")
	}

	// 分离 keyID 和 secret
	parts := strings.SplitN(rawKey, ":", 2)
	if len(parts) != 2 {
		utils.Warn("API Key 认证失败: 格式错误（缺少分隔符）")
		return 0, "", fmt.Errorf("无效的 API Key 格式")
	}
	keyID := parts[0]
	secret := parts[1]

	// 根据 keyID 查找
	var key models.ApiKey
	if err := s.db.Where("key_id = ? AND status = ?", keyID, "active").First(&key).Error; err != nil {
		utils.Warn("API Key 认证失败: Key 无效或已禁用", utils.String("key_id", keyID))
		return 0, "", fmt.Errorf("API Key 无效或已过期")
	}

	// 检查是否过期
	if key.ExpiresAt != nil && key.ExpiresAt.Before(utils.Now()) {
		utils.Warn("API Key 认证失败: Key 已过期", utils.String("key_id", keyID), zap.Time("expired_at", *key.ExpiresAt))
		return 0, "", fmt.Errorf("API Key 已过期")
	}

	// 使用 bcrypt 验证哈希
	if err := bcrypt.CompareHashAndPassword([]byte(key.ApiKey), []byte(secret)); err != nil {
		utils.Warn("API Key 认证失败: 密钥不匹配", utils.String("key_id", keyID))
		return 0, "", fmt.Errorf("API Key 无效")
	}

	// 更新最后使用时间（异步非阻塞，带超时）
	go func(kid uint, kID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		now := utils.Now()
		if err := s.db.WithContext(ctx).Model(&models.ApiKey{}).Where("id = ?", kid).
			Update("last_used_at", &now).Error; err != nil {
			utils.Warn("更新 API Key 最后使用时间失败", utils.String("key_id", kID), utils.Err(err))
		}
	}(key.ID, keyID)

	utils.Info("API Key 认证成功", utils.String("key_id", keyID), utils.String("scopes", key.Scopes), utils.Int("user_id", int(key.UserID)))
	return key.UserID, key.Scopes, nil
}

// ListApiKeys 分页查询 API Key 列表
func (s *ApiKeyService) ListApiKeys(page, pageSize int) ([]models.ApiKey, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	var total int64
	if err := s.db.Model(&models.ApiKey{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var keys []models.ApiKey
	if err := s.db.Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&keys).Error; err != nil {
		return nil, 0, err
	}

	if keys == nil {
		keys = make([]models.ApiKey, 0)
	}
	return keys, total, nil
}

// GetApiKey 获取单个 API Key
func (s *ApiKeyService) GetApiKey(id uint) (*models.ApiKey, error) {
	var key models.ApiKey
	if err := s.db.First(&key, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("API Key 不存在: %d", id)
		}
		return nil, err
	}
	return &key, nil
}

// UpdateApiKeyStatus 更新 API Key 状态（启用/禁用）
func (s *ApiKeyService) UpdateApiKeyStatus(id uint, status string) error {
	if status != "active" && status != "disabled" {
		utils.Warn("更新 API Key 状态失败: 无效的状态值", utils.Int("id", int(id)), utils.String("status", status))
		return fmt.Errorf("无效的状态值: %s", status)
	}

	result := s.db.Model(&models.ApiKey{}).Where("id = ?", id).
		Update("status", status)
	if result.Error != nil {
		utils.Error("更新 API Key 状态失败", utils.Int("id", int(id)), utils.Err(result.Error))
		return result.Error
	}
	if result.RowsAffected == 0 {
		utils.Warn("更新 API Key 状态失败: Key 不存在", utils.Int("id", int(id)))
		return fmt.Errorf("API Key 不存在: %d", id)
	}

	utils.Info("更新 API Key 状态成功", utils.Int("id", int(id)), utils.String("status", status))
	return nil
}

// DeleteApiKey 删除 API Key
func (s *ApiKeyService) DeleteApiKey(id uint) error {
	result := s.db.Delete(&models.ApiKey{}, id)
	if result.Error != nil {
		utils.Error("删除 API Key 失败", utils.Int("id", int(id)), utils.Err(result.Error))
		return result.Error
	}
	if result.RowsAffected == 0 {
		utils.Warn("删除 API Key 失败: Key 不存在", utils.Int("id", int(id)))
		return fmt.Errorf("API Key 不存在: %d", id)
	}

	utils.Info("删除 API Key 成功", utils.Int("id", int(id)))
	return nil
}

// GetUserApiKeys 查询用户的所有 API Key
func (s *ApiKeyService) GetUserApiKeys(userID uint) ([]models.ApiKey, error) {
	var keys []models.ApiKey
	if err := s.db.Where("user_id = ?", userID).
		Order("created_at DESC").Find(&keys).Error; err != nil {
		return nil, err
	}
	if keys == nil {
		keys = make([]models.ApiKey, 0)
	}
	return keys, nil
}

// generateKeyID 生成唯一 Key ID
func generateKeyID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		utils.Fatal("生成 Key ID 失败: crypto/rand.Read", utils.Err(err))
	}
	return "ak_" + hex.EncodeToString(b)[:28]
}

// generateRawKey 生成原始密钥
func generateRawKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		utils.Fatal("生成 API Key 失败: crypto/rand.Read", utils.Err(err))
	}
	return hex.EncodeToString(b)
}

// hashApiKey 使用 bcrypt 对 API Key 进行密码学安全哈希
func hashApiKey(key string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("哈希 API Key 失败: %w", err)
	}
	return string(hash), nil
}
