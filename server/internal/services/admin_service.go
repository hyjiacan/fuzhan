package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/zeebo/xxh3"
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
	Stats    SessionStats  `json:"stats"`
}

// SessionStats 会话的全局聚合统计（与分页无关，用于统计卡片）
type SessionStats struct {
	TotalSessions  int64 `json:"totalSessions"`
	ZombieSessions int64 `json:"zombieSessions"`
	TotalSize      int64 `json:"totalSize"`
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

	// 一次性聚合本页所有用户的私有存储用量，避免每用户一条查询（N+1）
	usageByOwner := make(map[string]int64)
	if len(users) > 0 {
		uuids := make([]any, len(users))
		for i, u := range users {
			uuids[i] = u.UUID
		}
		var usages []struct {
			OwnerID string
			Sum     int64
		}
		if err := s.db.Model(&models.FileRecordPrivate{}).
			Select("owner_id, COALESCE(SUM(file_size), 0) AS sum").
			Where("owner_id IN ? AND status = ? AND is_dir = ?", uuids, models.FileStatusActive, false).
			Group("owner_id").
			Scan(&usages).Error; err != nil {
			utils.Error("查询用户存储用量失败", utils.Err(err))
		}
		for _, u := range usages {
			usageByOwner[u.OwnerID] = u.Sum
		}
	}

	for i, u := range users {
		items[i] = UserItem{
			UUID:        u.UUID,
			Username:    u.Username,
			Role:        u.Role,
			Disabled:    u.Disabled,
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
			UsedStorage: usageByOwner[u.UUID],
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

	// 全局聚合统计（与分页无关）：总会话数、僵尸会话数、已占空间
	stats := SessionStats{TotalSessions: total}
	var zombieCount int64
	s.db.Model(&models.UploadSession{}).Where(
		"status IN ?", []string{
			string(models.UploadStatusPending),
			string(models.UploadStatusInProgress),
			string(models.UploadStatusExpired),
		},
	).Count(&zombieCount)
	stats.ZombieSessions = zombieCount
	if err := s.db.Model(&models.UploadedChunk{}).
		Select("COALESCE(SUM(chunk_size), 0)").
		Scan(&stats.TotalSize).Error; err != nil {
		utils.Error("统计会话占用空间失败", utils.Err(err))
	}

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
	return &SessionListResult{Sessions: items, Total: total, Stats: stats}, nil
}

// CleanupSessions 清理指定的会话（含分片文件、数据库记录及残留的落盘上传中文件）
func (s *AdminService) CleanupSessions(sessionIDs []uint) (int, error) {
	cleanedCount := 0
	var errs []error
	for _, sessionID := range sessionIDs {
		var session models.UploadSession
		if err := s.db.First(&session, sessionID).Error; err != nil {
			continue
		}

		// 清理落盘的 uploadin 残留文件（按 targetType 推导真实路径）
		if uploadingPath, perr := resolveUploadingPath(&session); perr == nil {
			if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
				errs = append(errs, fmt.Errorf("删除上传中残留文件失败(session=%d): %w", sessionID, rmErr))
			}
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

// resolveUploadingPath 按会话的上传类型推导 uploadin 残留文件的落盘路径。
// 与各存储策略的命名规则一致：公开/私有为目标文件同目录下的 `.{basename}.uploading`，
// 临时为 `.{码哈希}.uploading`。路径推导失败时返回 error（由调用方决定是否忽略）。
func resolveUploadingPath(session *models.UploadSession) (string, error) {
	switch session.TargetType {
	case models.TargetTypePrivate:
		basePath := appconfig.GlobalConfig.Storage.Private.Path
		if basePath == "" {
			return "", fmt.Errorf("私有存储未配置")
		}
		userDir := filepath.Join(basePath, "users", session.UserID)
		dir := joinUnderRoot(userDir, session.TargetPath)
		if dir == "" {
			return "", fmt.Errorf("私有路径越界")
		}
		return filepath.Join(dir, "."+session.FileName+".uploading"), nil
	case models.TargetTypeTemp:
		basePath := appconfig.GlobalConfig.Storage.Temp.Path
		if basePath == "" || session.Code == "" {
			return "", fmt.Errorf("临时存储未配置或缺少访问码")
		}
		dir := joinUnderRoot(basePath, session.TargetPath)
		if dir == "" {
			return "", fmt.Errorf("临时路径越界")
		}
		hash := xxh3.Hash([]byte(session.Code + "fuzhan-secret"))
		safeFilename := fmt.Sprintf("%016x", hash)
		return filepath.Join(dir, "."+safeFilename+".uploading"), nil
	default: // 公开（包含历史无 target_type 的遗留会话）
		_, uploadingPath, perr := (PublicStorage{}).Paths(session)
		if perr != nil {
			return "", perr
		}
		return uploadingPath, nil
	}
}

// joinUnderRoot 将会话的相对子目录安全拼接到根目录下，返回空字符串表示越界/非法。
// 拒绝绝对路径、上级跳转及包含路径分隔符陷阱的相对片段。
func joinUnderRoot(root, rel string) string {
	rel = strings.TrimSpace(rel)
	rel = strings.Trim(rel, `/\`)
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == "" {
		return root
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) ||
		strings.Contains(cleaned, ".."+string(os.PathSeparator)) {
		return ""
	}
	joined := filepath.Join(root, cleaned)
	absRoot, _ := filepath.Abs(root)
	absJoined, _ := filepath.Abs(joined)
	if absRoot == "" || absJoined == "" ||
		(absJoined != absRoot && !strings.HasPrefix(absJoined, absRoot+string(os.PathSeparator))) {
		return ""
	}
	return joined
}

// CleanupAllZombieSessions 后台重新查询全部僵尸（过期/待处理/上传中）会话并清理。
// 与按 ID 清理不同，由后端决定具体清理哪些，前端无需关心分页。
func (s *AdminService) CleanupAllZombieSessions() (int, error) {
	var ids []uint
	if err := s.db.Model(&models.UploadSession{}).
		Where("status IN ?", []string{
			string(models.UploadStatusPending),
			string(models.UploadStatusInProgress),
			string(models.UploadStatusExpired),
		}).
		Pluck("id", &ids).Error; err != nil {
		utils.Error("查询僵尸会话失败", utils.Err(err))
		return 0, fmt.Errorf("查询僵尸会话失败: %w", err)
	}
	if len(ids) == 0 {
		utils.Info("一键清理：没有需要清理的僵尸会话")
		return 0, nil
	}

	cleaned, err := s.CleanupSessions(ids)
	if err != nil {
		utils.Error("一键清理僵尸会话失败",
			utils.Int("matched", len(ids)),
			utils.Int("cleaned", cleaned),
			utils.Err(err))
		return cleaned, err
	}
	return cleaned, nil
}
