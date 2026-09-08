package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// FileIndexNotifier 文件名检索索引（如 Bluge 建议索引）的变更通知器。
// 文件记录增/删/改后同步更新检索索引，保证"搜索联想/拼写纠错"与文件记录目录一致。
// 可选注入：未设置时不产生任何副作用（不影响原有同步逻辑）。
type FileIndexNotifier interface {
	// IndexFile 新增或更新某条文件名的检索索引（幂等，按 fileID 覆盖）
	IndexFile(fileID int64, fileName string) error
	// Delete 删除某条文件名的检索索引（幂等）
	Delete(fileID int64) error
}

// Syncer 增量同步器
// 负责处理文件变更事件（上传、删除、移动），
// 将操作同步到 FileRecordPublic 索引表中。
type Syncer struct {
	db        *gorm.DB
	rootNames map[string]string
	retryBase time.Duration // 重试基期间隔
	maxRetry  int           // 最大重试次数

	notifier FileIndexNotifier // 可选：检索索引变更通知
}

// NewSyncer 创建增量同步器
func NewSyncer(db *gorm.DB, rootNames map[string]string) *Syncer {
	return &Syncer{
		db:        db,
		rootNames: rootNames,
		retryBase: time.Second,
		maxRetry:  3,
	}
}

// SetFileIndexNotifier 设置检索索引变更通知器（可选）
func (s *Syncer) SetFileIndexNotifier(n FileIndexNotifier) {
	s.notifier = n
}

// SyncFile 上传/创建文件时同步索引
// 如果记录已存在（如覆盖上传），则更新字段；
// 如果记录不存在，则新建。
// rootName: 根目录名称
// filePath: 相对于公开根目录的路径
func (s *Syncer) SyncFile(rootName, filePath string) error {
	return s.withRetry(func() error {
		return s.syncFileOnce(rootName, filePath)
	})
}

// syncFileOnce 执行单次同步（不含重试）
func (s *Syncer) syncFileOnce(rootName, filePath string) error {
	rootPath, ok := s.rootNames[rootName]
	if !ok {
		return fmt.Errorf("根目录不存在: %s", rootName)
	}

	fullPath := filepath.Join(rootPath, strings.TrimPrefix(filePath, "/"))
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %w", err)
	}

	now := utils.Now()
	modTime := info.ModTime()

	// 标准化路径分隔符
	dbPath := filepath.ToSlash(filePath)
	// 标准化 file_path：确保有前导 /，FullPath 格式：/rootName/path
	if !strings.HasPrefix(dbPath, "/") {
		dbPath = "/" + dbPath
	}

	// 记录 ID（新增或更新后），事务提交后同步检索索引
	var fileID int64

	err = s.db.Transaction(func(tx *gorm.DB) error {
		// === 写入 file_records_public ===
		var record models.FileRecordPublic
		result := tx.Where("root_name = ? AND file_path = ?",
			rootName, dbPath).First(&record)

		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				rec := models.FileRecordPublic{
					FileRecordBase: models.FileRecordBase{
						FileName:     info.Name(),
						FilePath:     dbPath,
						RootName:     rootName,
						FullPath:     "/" + rootName + dbPath,
						FileSize:     fileSizeFromInfo(info),
						IsDir:        info.IsDir(),
						ModTime:      modTime,
						Status:       models.FileStatusActive,
						LastSyncedAt: now,
					},
				}
				if err := tx.Create(&rec).Error; err != nil {
					return err
				}
				fileID = int64(rec.ID)
			} else {
				return result.Error
			}
		} else {
			updates := map[string]interface{}{
				"file_name":      info.Name(),
				"file_size":      fileSizeFromInfo(info),
				"is_dir":         info.IsDir(),
				"mod_time":       modTime,
				"status":         models.FileStatusActive,
				"last_synced_at": now,
				"updated_at":     now,
				"deleted_at":     nil,
				// full_path 同步刷新（/rootName/filePath），保持与 file_path 一致
				"full_path": "/" + rootName + dbPath,
			}
			if err := tx.Model(&record).Updates(updates).Error; err != nil {
				return err
			}
			fileID = int64(record.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 索引同步（幂等，按 fileID 覆盖）；未注入 notifier 时无副作用
	if s.notifier != nil && fileID > 0 {
		if nerr := s.notifier.IndexFile(fileID, info.Name()); nerr != nil {
			utils.Warn("同步文件名检索索引失败",
				utils.Int64("id", fileID),
				utils.String("name", info.Name()),
				utils.Err(nerr))
		}
	}
	return nil
}

// RemoveFile 删除文件时软删除索引记录
// filePath: 相对于公开根目录的路径
func (s *Syncer) RemoveFile(rootName, filePath string) error {
	return s.withRetry(func() error {
		return s.removeFileOnce(rootName, filePath)
	})
}

// removeFileOnce 执行单次删除（不含重试）
func (s *Syncer) removeFileOnce(rootName, filePath string) error {
	dbPath := filepath.ToSlash(filePath)
	// 标准化 file_path：确保有前导 /
	if !strings.HasPrefix(dbPath, "/") {
		dbPath = "/" + dbPath
	}
	now := utils.Now()
	updates := map[string]interface{}{
		"status":     models.FileStatusDeleted,
		"updated_at": now,
	}

	// 收集被软删除记录的 ID，事务提交后同步删除检索索引条目
	var affectedIDs []int64

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 如果是目录, 递归软删除其下所有文件
		isDir, err := s.isDirectory(rootName, dbPath)
		if err != nil {
			utils.Warn("检查目录状态失败, 按文件处理",
				utils.String("path", dbPath),
				utils.String("root", rootName),
				utils.Err(err))
		}

		var rows []models.FileRecordPublic
		if isDir {
			prefix := strings.TrimSuffix(dbPath, "/") + "/"
			if err := tx.Model(&models.FileRecordPublic{}).
				Where("root_name = ? AND (file_path = ? OR file_path LIKE ?) AND status = ?",
					rootName, dbPath, prefix+"%", models.FileStatusActive).
				Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) > 0 {
				ids := make([]uint, 0, len(rows))
				for _, r := range rows {
					ids = append(ids, r.ID)
				}
				if err := tx.Model(&models.FileRecordPublic{}).
					Where("id IN ?", ids).
					Updates(updates).Error; err != nil {
					return err
				}
			}
		} else {
			if err := tx.Model(&models.FileRecordPublic{}).
				Where("root_name = ? AND file_path = ? AND status = ?",
					rootName, dbPath, models.FileStatusActive).
				Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) > 0 {
				ids := make([]uint, 0, len(rows))
				for _, r := range rows {
					ids = append(ids, r.ID)
				}
				if err := tx.Model(&models.FileRecordPublic{}).
					Where("id IN ?", ids).
					Updates(updates).Error; err != nil {
					return err
				}
			}
		}

		for _, r := range rows {
			affectedIDs = append(affectedIDs, int64(r.ID))
		}
		return nil
	})
	if err != nil {
		return err
	}

	if s.notifier != nil {
		for _, id := range affectedIDs {
			if derr := s.notifier.Delete(id); derr != nil {
				utils.Warn("删除文件名检索索引失败",
					utils.Int64("id", id),
					utils.Err(derr))
			}
		}
	}
	return nil
}

// MoveFile 移动/重命名文件时更新索引
func (s *Syncer) MoveFile(rootName, oldPath, newPath string) error {
	return s.withRetry(func() error {
		return s.moveFileOnce(rootName, oldPath, newPath)
	})
}

// moveFileOnce 执行单次移动（不含重试）
func (s *Syncer) moveFileOnce(rootName, oldPath, newPath string) error {
	oldDbPath := filepath.ToSlash(oldPath)
	newDbPath := filepath.ToSlash(newPath)
	// 标准化 file_path：确保有前导 /
	if !strings.HasPrefix(oldDbPath, "/") {
		oldDbPath = "/" + oldDbPath
	}
	if !strings.HasPrefix(newDbPath, "/") {
		newDbPath = "/" + newDbPath
	}
	now := utils.Now()
	moveUpdates := map[string]interface{}{
		"file_name": filepath.Base(strings.TrimSuffix(newDbPath, "/")),
		"file_path": newDbPath,
		// full_path 同步更新：/rootName/newDbPath
		"full_path":      "/" + rootName + newDbPath,
		"updated_at":     now,
		"last_synced_at": now,
	}

	var topID int64

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 查找旧记录
		var record models.FileRecordPublic
		result := tx.Where("root_name = ? AND file_path = ? AND status = ?",
			rootName, oldDbPath, models.FileStatusActive).First(&record)
		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				return fmt.Errorf("索引记录不存在: %s/%s", rootName, oldPath)
			}
			return result.Error
		}
		topID = int64(record.ID)

		if record.IsDir {
			prefix := strings.TrimSuffix(oldDbPath, "/") + "/"
			subExpr := gorm.Expr("REPLACE(file_path, ?, ?)", oldDbPath+"/", newDbPath+"/")
			// 子文件的 full_path 需要按新的 file_path 重新拼接 /rootName + ...
			// 使用 REPLACE 同时替换 file_path 和 full_path（full_path = /rootName + file_path）
			subFullPathExpr := gorm.Expr(
				"REPLACE(full_path, ?, ?)",
				"/"+rootName+oldDbPath+"/",
				"/"+rootName+newDbPath+"/",
			)

			// 更新目录自身
			if err := tx.Model(&models.FileRecordPublic{}).
				Where("id = ?", record.ID).
				Updates(moveUpdates).Error; err != nil {
				return err
			}
			// 更新子文件
			return tx.Model(&models.FileRecordPublic{}).
				Where("root_name = ? AND file_path LIKE ? AND status = ?",
					rootName, prefix+"%", models.FileStatusActive).
				Updates(map[string]interface{}{
					"file_path":      subExpr,
					"full_path":      subFullPathExpr,
					"updated_at":     now,
					"last_synced_at": now,
				}).Error
		}

		// 文件移动: 更新路径
		return tx.Model(&record).Updates(moveUpdates).Error
	})
	if err != nil {
		return err
	}

	// 移动/重命名后，按最新记录状态重入检索索引（以覆盖旧文件名为准）
	if s.notifier != nil && topID > 0 {
		var refreshed models.FileRecordPublic
		if rerr := s.db.Where("id = ?", topID).First(&refreshed).Error; rerr == nil {
			if ierr := s.notifier.IndexFile(topID, refreshed.FileName); ierr != nil {
				utils.Warn("同步移动后文件名检索索引失败",
					utils.Int64("id", topID),
					utils.Err(ierr))
			}
		}
	}
	return nil
}

// withRetry 带指数退避的重试包装
func (s *Syncer) withRetry(fn func() error) error {
	var err error
	delays := []time.Duration{
		s.retryBase,
		s.retryBase * 5,
		s.retryBase * 30,
	}

	for i := 0; i <= s.maxRetry; i++ {
		if err = fn(); err == nil {
			return nil
		}
		utils.Warn("索引同步失败, 准备重试",
			utils.Int("attempt", i+1),
			utils.Int("max", s.maxRetry+1),
			utils.Err(err))

		if i < len(delays) {
			time.Sleep(delays[i])
		}
	}
	return fmt.Errorf("索引同步重试 %d 次后仍然失败: %w", s.maxRetry+1, err)
}

// isDirectory 检查 file_records_public 中是否为目录
func (s *Syncer) isDirectory(rootName, dbPath string) (bool, error) {
	var count int64
	err := s.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND file_path = ? AND is_dir = ? AND status = ?",
			rootName, dbPath, true, models.FileStatusActive).
		Count(&count).Error
	return count > 0, err
}
