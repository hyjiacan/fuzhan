package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

// CleanupUserAssets 清除用户持有的私有/临时文件与关联数据。
// 必须在删除 users 行之前调用：多数关联表按用户 UUID 归属。
// 登录日志与操作记录属审计数据，不在清理范围内。
func CleanupUserAssets(db *gorm.DB, user *models.User) error {
	if db == nil || user == nil || strings.TrimSpace(user.UUID) == "" {
		return fmt.Errorf("用户信息不完整")
	}
	uuid := user.UUID

	// 1. 私有存储：整个用户目录（{private}/users/{uuid}）随账号一并删除。
	// 私有/临时存储恒为本地落盘，与公开存储的后端类型无关。
	if basePath := strings.TrimSpace(appconfig.GlobalConfig.Storage.Private.Path); basePath != "" {
		userDir := filepath.Join(basePath, "users", uuid)
		if err := os.RemoveAll(userDir); err != nil && !os.IsNotExist(err) {
			utils.Warn("注销用户: 删除私有文件目录失败",
				utils.String("uuid", uuid), utils.String("path", userDir), utils.Err(err))
		}
	}

	// 2. 临时文件：仅清理登录用户上传的部分（匿名上传无 user_id，保持原有过期清理策略）
	var tempFiles []models.TempFile
	if err := db.Where("user_id = ?", uuid).Find(&tempFiles).Error; err != nil {
		return fmt.Errorf("查询用户临时文件失败: %w", err)
	}
	codes := make([]string, 0, len(tempFiles))
	for i := range tempFiles {
		if tempFiles[i].FilePath != "" {
			if err := os.Remove(tempFiles[i].FilePath); err != nil && !os.IsNotExist(err) {
				utils.Warn("注销用户: 删除临时文件失败",
					utils.String("uuid", uuid), utils.String("path", tempFiles[i].FilePath), utils.Err(err))
			}
		}
		if tempFiles[i].Code != "" {
			codes = append(codes, tempFiles[i].Code)
		}
	}

	// 3. 未完成的上传会话：清理落盘的 uploading 残留与分片
	var sessions []models.UploadSession
	if err := db.Unscoped().Where("user_id = ?", uuid).Find(&sessions).Error; err != nil {
		return fmt.Errorf("查询用户上传会话失败: %w", err)
	}
	sessionIDs := make([]uint, 0, len(sessions))
	for i := range sessions {
		sessionIDs = append(sessionIDs, sessions[i].ID)
		if uploadingPath, perr := resolveUploadingPath(&sessions[i]); perr == nil {
			if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
				utils.Warn("注销用户: 删除上传中残留文件失败",
					utils.Int("session_id", int(sessions[i].ID)), utils.Err(rmErr))
			}
		}
	}

	// 4. URL 上传任务：清理半截下载文件与任务记录
	var urlTasks []models.URLDownloadTask
	if err := db.Where("user_id = ?", uuid).Find(&urlTasks).Error; err == nil {
		for i := range urlTasks {
			if urlTasks[i].TempFilePath != "" {
				if rmErr := os.Remove(urlTasks[i].TempFilePath); rmErr != nil && !os.IsNotExist(rmErr) {
					utils.Warn("注销用户: 删除URL上传临时文件失败",
						utils.String("task_id", urlTasks[i].ID), utils.Err(rmErr))
				}
			}
		}
	} else {
		utils.Warn("注销用户: 查询URL上传任务失败", utils.String("uuid", uuid), utils.Err(err))
	}

	// 5. 数据库关联记录（与文件清理解耦，尽量在单个事务内完成）
	return db.Transaction(func(tx *gorm.DB) error {
		if len(codes) > 0 {
			if err := tx.Unscoped().
				Where("root_name = ? AND file_path IN ?", "temp", codes).
				Delete(&models.FileRecordTemp{}).Error; err != nil {
				return fmt.Errorf("清理临时文件索引失败: %w", err)
			}
		}
		if err := tx.Unscoped().Where("owner_id = ?", uuid).
			Delete(&models.FileRecordPrivate{}).Error; err != nil {
			return fmt.Errorf("清理私有文件记录失败: %w", err)
		}
		if err := tx.Where("user_id = ?", uuid).Delete(&models.TempFile{}).Error; err != nil {
			return fmt.Errorf("清理临时文件记录失败: %w", err)
		}
		if len(sessionIDs) > 0 {
			if err := tx.Where("session_id IN ?", sessionIDs).
				Delete(&models.UploadedChunk{}).Error; err != nil {
				return fmt.Errorf("清理上传分片记录失败: %w", err)
			}
		}
		if err := tx.Unscoped().Where("user_id = ?", uuid).
			Delete(&models.UploadSession{}).Error; err != nil {
			return fmt.Errorf("清理上传会话失败: %w", err)
		}
		if err := tx.Where("user_id = ?", uuid).Delete(&models.URLDownloadTask{}).Error; err != nil {
			return fmt.Errorf("清理URL上传任务失败: %w", err)
		}
		// 以下三张表以自增主键关联用户
		if user.ID != 0 {
			if err := tx.Where("user_id = ?", user.ID).Delete(&models.ApiKey{}).Error; err != nil {
				return fmt.Errorf("清理API密钥失败: %w", err)
			}
			if err := tx.Where("user_id = ?", user.ID).Delete(&models.OAuthClient{}).Error; err != nil {
				return fmt.Errorf("清理OAuth客户端失败: %w", err)
			}
			if err := tx.Where("user_id = ?", user.ID).Delete(&models.AuthRecord{}).Error; err != nil {
				return fmt.Errorf("清理外部认证记录失败: %w", err)
			}
		}
		return nil
	})
}
