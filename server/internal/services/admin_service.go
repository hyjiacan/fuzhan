package services

import (
	"fmt"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserListResult 用户列表结果
type UserListResult struct {
	Users []UserItem `json:"users"`
	Total int64      `json:"total"`
}

// UserItem 用户列表项
type UserItem struct {
	UUID        string    `json:"uuid"`
	Username    string    `json:"username"`
	Role        string    `json:"role"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UsedStorage int64     `json:"usedStorage"`
	Quota       int64     `json:"quota"`
}

// SessionListResult 会话列表结果
type SessionListResult struct {
	Sessions []SessionItem `json:"sessions"`
	Total    int64         `json:"total"`
}

// SessionItem 会话列表项
type SessionItem struct {
	ID           uint   `json:"id"`
	FileName     string `json:"fileName"`
	FileSize     int64  `json:"fileSize"`
	UploadedSize int64  `json:"uploadedSize"`
	Status       string `json:"status"`
	TargetPath   string `json:"targetPath"`
	TargetRoot   string `json:"targetRoot"`
	CreatedAt    string `json:"createdAt"`
	ExpiredAt    string `json:"expiredAt"`
}

// AdminService 管理员服务
type AdminService struct {
	db *gorm.DB
}

// NewAdminService 创建管理员服务
func NewAdminService(db *gorm.DB) *AdminService {
	return &AdminService{db: db}
}

// ListUsers 获取用户列表
func (s *AdminService) ListUsers(page, pageSize int, query string) (*UserListResult, error) {
	var users []models.User
	var total int64

	dbQuery := s.db.Model(&models.User{}).Where("role != ?", "admin")
	if query != "" {
		likeQuery := "%" + query + "%"
		dbQuery = dbQuery.Where("username LIKE ? OR `uuid` LIKE ?", likeQuery, likeQuery)
	}

	dbQuery.Count(&total)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	if err := dbQuery.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		utils.Error("获取用户列表失败", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Err(err))
		return nil, fmt.Errorf("获取用户列表失败: %w", err)
	}

	items := make([]UserItem, len(users))
	perUserQuota := appconfig.GlobalConfig.Storage.Private.Quota.PerUserQuota
	for i, u := range users {
		// 查询用户已使用的私有存储空间
		var usedStorage int64
		if row := s.db.Model(&models.FileRecordPrivate{}).
			Select("COALESCE(SUM(file_size), 0)").
			Where("owner_id = ? AND status = ? AND is_dir = ?", u.UUID, models.FileStatusActive, false).
			Row(); row != nil {
			row.Scan(&usedStorage)
		}

		items[i] = UserItem{
			UUID:        u.UUID,
			Username:    u.Username,
			Role:        u.Role,
			Disabled:    u.Disabled,
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
			UsedStorage: usedStorage,
			Quota:       perUserQuota,
		}
	}

	utils.Info("获取用户列表成功", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Int64("total", total))
	return &UserListResult{Users: items, Total: total}, nil
}

// ResetPassword 重置用户密码
func (s *AdminService) ResetPassword(callerUUID, targetUUID, newPassword string) error {
	// 校验调用者是管理员
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("重置密码失败: 无权操作", utils.String("caller_uuid", callerUUID), utils.String("target_uuid", targetUUID))
		return fmt.Errorf("无权操作：仅管理员可重置密码")
	}

	var user models.User
	if err := s.db.Where("uuid = ?", targetUUID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.Warn("重置密码失败: 用户不存在", utils.String("target_uuid", targetUUID))
			return fmt.Errorf("用户不存在")
		}
		utils.Error("重置密码失败: 查询用户错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		utils.Error("重置密码失败: 密码加密错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("密码加密失败: %w", err)
	}

	user.PasswordHash = string(hashedPassword)
	if err := s.db.Save(&user).Error; err != nil {
		utils.Error("重置密码失败: 保存用户错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("更新密码失败: %w", err)
	}

	utils.Info("管理员重置用户密码成功", utils.String("admin", caller.Username), utils.String("target_user", user.Username))
	return nil
}

// ToggleUserStatus 启用/禁用用户
func (s *AdminService) ToggleUserStatus(uuid string, disabled bool) error {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.Warn("切换用户状态失败: 用户不存在", utils.String("uuid", uuid))
			return fmt.Errorf("用户不存在")
		}
		utils.Error("切换用户状态失败: 查询用户错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	if user.Role == "admin" {
		utils.Warn("切换用户状态失败: 不能禁用管理员账号", utils.String("uuid", uuid))
		return fmt.Errorf("不能禁用管理员账号")
	}

	user.Disabled = disabled
	if err := s.db.Save(&user).Error; err != nil {
		utils.Error("切换用户状态失败: 保存用户错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("更新用户状态失败: %w", err)
	}

	action := "启用"
	if disabled {
		action = "禁用"
	}
	utils.Info("切换用户状态成功", utils.String("user", user.Username), utils.String("action", action))
	return nil
}

// DeleteUser 删除用户
func (s *AdminService) DeleteUser(uuid string) error {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.Warn("删除用户失败: 用户不存在", utils.String("uuid", uuid))
			return fmt.Errorf("用户不存在")
		}
		utils.Error("删除用户失败: 查询用户错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	if user.Role == "admin" {
		utils.Warn("删除用户失败: 不能删除管理员账号", utils.String("uuid", uuid))
		return fmt.Errorf("不能删除管理员账号")
	}

	username := user.Username
	if err := s.db.Delete(&user).Error; err != nil {
		utils.Error("删除用户失败: 数据库错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("删除用户失败: %w", err)
	}

	utils.Info("删除用户成功", utils.String("username", username), utils.String("uuid", uuid))
	return nil
}

// ListSessions 获取会话列表
func (s *AdminService) ListSessions(page, pageSize int) (*SessionListResult, error) {
	var sessions []models.UploadSession
	var total int64

	s.db.Model(&models.UploadSession{}).Count(&total)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	if err := s.db.Order("created_at DESC").Preload("Chunks").Offset(offset).Limit(pageSize).Find(&sessions).Error; err != nil {
		utils.Error("获取会话列表失败", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Err(err))
		return nil, fmt.Errorf("获取会话列表失败: %w", err)
	}

	items := make([]SessionItem, len(sessions))
	for i, sess := range sessions {
		var uploadedSize int64
		for _, chunk := range sess.Chunks {
			uploadedSize += chunk.ChunkSize
		}

		items[i] = SessionItem{
			ID:           sess.ID,
			FileName:     sess.FileName,
			FileSize:     sess.FileSize,
			UploadedSize: uploadedSize,
			Status:       string(sess.Status),
			TargetPath:   sess.TargetPath,
			TargetRoot:   sess.TargetRoot,
			CreatedAt:    sess.CreatedAt.Format("2006-01-02 15:04:05"),
			ExpiredAt:    sess.ExpiredAt.Format("2006-01-02 15:04:05"),
		}
	}

	utils.Info("获取会话列表成功", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Int64("total", total))
	return &SessionListResult{Sessions: items, Total: total}, nil
}

// CleanupSessions 清理指定的会话（含分片文件和数据库记录）
func (s *AdminService) CleanupSessions(sessionIDs []uint) (int, error) {
	cleanedCount := 0
	var errs []error
	for _, sessionID := range sessionIDs {
		var session models.UploadSession
		if err := s.db.First(&session, sessionID).Error; err != nil {
			continue
		}

		// 删除分片记录
		if err := s.db.Where("session_id = ?", sessionID).Delete(&models.UploadedChunk{}).Error; err != nil {
			errs = append(errs, fmt.Errorf("删除分片记录失败(session=%d): %w", sessionID, err))
		}

		// 删除会话记录
		if err := s.db.Delete(&models.UploadSession{}, sessionID).Error; err != nil {
			errs = append(errs, fmt.Errorf("删除会话记录失败(session=%d): %w", sessionID, err))
		}

		cleanedCount++
	}

	if len(errs) > 0 {
		utils.Error("清理会话完成（部分失败）",
			utils.Int("cleaned", cleanedCount),
			utils.Int("errors", len(errs)),
			utils.Any("error_details", errs))
		return cleanedCount, fmt.Errorf("清理会话时发生 %d 个错误: %v", len(errs), errs)
	}

	utils.Info("清理会话完成", utils.Int("count", cleanedCount), utils.Int("session_ids", len(sessionIDs)))
	return cleanedCount, nil
}
