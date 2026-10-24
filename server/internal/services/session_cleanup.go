package services

import (
	"context"
	"fmt"
	"os"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

// applyCleanupType 将清理查询限定到本服务负责的上传类型。
// 公开服务需同时覆盖公开与历史无类型的遗留会话。
func (s *UploadSessionService) applyCleanupType(q *gorm.DB) *gorm.DB {
	if s.targetType == models.TargetTypeRegular {
		return q.Where("target_type IN (?, '')", models.TargetTypeRegular)
	}
	return q.Where("target_type = ?", s.targetType)
}

// CleanupExpired 清理过期会话（仅清理归属本服务 targetType 的会话）
func (s *UploadSessionService) CleanupExpired() (int, error) {
	var sessions []models.UploadSession
	now := utils.Now()

	expiredQuery := s.db.Where("status IN ? AND expired_at < ?",
		[]models.UploadStatus{
			models.UploadStatusPending,
			models.UploadStatusInProgress,
			models.UploadStatusFailed,
		}, now).Scopes(s.applyCleanupType)
	if err := expiredQuery.Find(&sessions).Error; err != nil {
		return 0, fmt.Errorf("查询过期会话失败: %w", err)
	}

	// 清理已完成的旧会话（保留1小时）
	var completedSessions []models.UploadSession
	completedThreshold := now.Add(-1 * time.Hour)
	if err := s.db.Where("status = ? AND updated_at < ?",
		models.UploadStatusCompleted, completedThreshold).Scopes(s.applyCleanupType).Find(&completedSessions).Error; err != nil {
		utils.Error("查询已完成会话失败", utils.Err(err))
	} else {
		for _, sess := range completedSessions {
			_ = s.chunkRepo.DeleteBySessionID(sess.ID)
			_ = s.sessionRepo.Delete(sess.ID)
		}
		if len(completedSessions) > 0 {
			utils.Info("已清理已完成会话", utils.Int("count", len(completedSessions)))
		}
	}

	cleanedCount := 0
	for _, session := range sessions {
		// 远端后端：清理整个分片目录；本地后端：删除单 uploading 临时文件。
		if IsRemoteSession(&session) {
			if chunkDir, _, ok := s.storage.ChunkObjectPaths(&session, 0); ok {
				if st, sok := s.storageFor(&session); sok {
					if rmErr := st.RemoveAll(context.Background(), chunkDir); rmErr != nil {
						utils.Error("清理会话远端分片失败", utils.Int("session_id", int(session.ID)), utils.Err(rmErr))
					}
				}
			}
		} else if _, uploadingPath, perr := s.storage.Paths(&session); perr == nil {
			if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
				utils.Error("清理会话文件失败", utils.Int("session_id", int(session.ID)), utils.Err(rmErr))
			}
		}

		if err := s.sessionRepo.UpdateStatus(session.ID, models.UploadStatusExpired); err != nil {
			utils.Error("更新会话状态失败", utils.Int("session_id", int(session.ID)), utils.Err(err))
			continue
		}

		_ = s.chunkRepo.DeleteBySessionID(session.ID)
		cleanedCount++
	}

	if cleanedCount > 0 {
		utils.Info("已清理过期上传会话", utils.Int("count", cleanedCount))
	}

	return cleanedCount, nil
}
