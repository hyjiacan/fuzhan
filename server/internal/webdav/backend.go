// Package webdav implements the service-layer Backend that WebDAV delegates
// to, so WebDAV uploads/delete/rename reuse the same permission, disk/quota,
// index, and hash rules as the Web upload system.
package webdav

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	webdavfs "fuzhan/internal/access/webdav"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/file"
	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

// privateRootName 私有文件在索引表中的根名（固定值，与 internal/file 约定一致）。
const privateRootName = "private"

// webdavBackend 实现 webdavfs.Backend：将 WebDAV 虚拟文件系统的
// 上传开关/扩展名/磁盘配额/索引哈希委托给 Web 上传体系。
type webdavBackend struct {
	db          *gorm.DB
	indexSvc    *index.Service
	privatePath string // 私有存储根路径（未启用时为空串）
}

// NewWebDAVBackend 创建 WebDAV 后端能力实现。
// privatePath 为私有存储根路径，未启用私有存储时传空串。
func NewWebDAVBackend(db *gorm.DB, indexSvc *index.Service, privatePath string) webdavfs.Backend {
	return &webdavBackend{db: db, indexSvc: indexSvc, privatePath: privatePath}
}

// IsAdmin 按用户 UUID 查询角色（未知返回 false）。
func (b *webdavBackend) IsAdmin(userUUID string) bool {
	if userUUID == "" || b.db == nil {
		return false
	}
	var user models.User
	if err := b.db.Select("role").Where("uuid = ?", userUUID).First(&user).Error; err != nil {
		return false
	}
	return user.Role == "admin"
}

// UploadEnabled 公共目录上传总开关（与 Web 上传一致，不豁免管理员）。
func (b *webdavBackend) UploadEnabled() bool {
	return appconfig.GlobalConfig.Upload.Enabled
}

// CheckDiskSpace 检查路径所在分区剩余空间。
func (b *webdavBackend) CheckDiskSpace(path string, required int64) error {
	return services.CheckDiskSpace(path, required)
}

// CheckPrivateQuota 检查用户私有存储配额（配额 <=0 视为不限制）。
func (b *webdavBackend) CheckPrivateQuota(userUUID string, addSize int64) error {
	if userUUID == "" {
		return nil
	}
	quota := appconfig.GlobalConfig.Storage.Private.PerUserQuota
	if quota <= 0 {
		return nil
	}
	used := file.GetUserUsedQuota(b.privatePath, userUUID)
	if used+addSize > quota {
		return fmt.Errorf("超出用户配额限制（已用：%d 字节，配额：%d 字节）", used, quota)
	}
	return nil
}

// ValidateExtension 校验文件名是否符合扩展名白名单（空白名单放行）。
func (b *webdavBackend) ValidateExtension(filename string) error {
	allowed := appconfig.GlobalConfig.Storage.AllowedExtensions
	if len(allowed) == 0 {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || len(ext) <= 1 {
		return fmt.Errorf("禁止上传无扩展名的文件")
	}
	ext = ext[1:] // strip leading dot
	for _, a := range allowed {
		if a == ext {
			return nil
		}
	}
	return fmt.Errorf("不允许的文件类型: .%s", ext)
}

// SyncPublicFile 将公开文件同步进索引：写 file_records_public、标记最近已同步、
// 更新上传者 IP、触发哈希。
func (b *webdavBackend) SyncPublicFile(rootName, relPath, uploaderIP string) {
	if b.indexSvc == nil {
		return
	}
	if err := b.indexSvc.SyncFile(rootName, relPath); err != nil {
		utils.Warn("WebDAV上传后同步索引失败",
			utils.String("root", rootName),
			utils.String("path", relPath),
			utils.Err(err))
		return
	}
	b.indexSvc.MarkRecentlySynced(rootName, relPath)
	if uploaderIP != "" && b.db != nil {
		if err := b.db.Model(&models.FileRecordPublic{}).
			Where("root_name = ? AND file_path = ?", rootName, relPath).
			UpdateColumn("uploader_ip", uploaderIP).Error; err != nil {
			utils.Warn("WebDAV写入上传者IP失败",
				utils.String("root", rootName),
				utils.String("path", relPath),
				utils.Err(err))
		}
	}
	b.indexSvc.TriggerHash(context.Background())
}

// RemovePublicFile 软删除公开文件索引。
func (b *webdavBackend) RemovePublicFile(rootName, relPath string) {
	if b.indexSvc == nil {
		return
	}
	if err := b.indexSvc.RemoveFile(rootName, relPath); err != nil {
		utils.Warn("WebDAV删除后同步索引失败",
			utils.String("root", rootName),
			utils.String("path", relPath),
			utils.Err(err))
	}
}

// MovePublicFile 同步公开文件重命名/移动后的索引路径。
func (b *webdavBackend) MovePublicFile(rootName, oldRelPath, newRelPath string) {
	if b.indexSvc == nil {
		return
	}
	if err := b.indexSvc.MoveFile(rootName, oldRelPath, newRelPath); err != nil {
		utils.Warn("WebDAV重命名后同步索引失败",
			utils.String("root", rootName),
			utils.String("oldPath", oldRelPath),
			utils.String("newPath", newRelPath),
			utils.Err(err))
	}
}

// AddPrivateFile 写入私有文件记录（含分享码）并触发哈希。
// 真实大小配额检查在此时执行：超出则删除已落盘文件并返回错误。
func (b *webdavBackend) AddPrivateFile(userUUID, relPath, fileName string, size int64) error {
	if userUUID == "" || b.db == nil {
		b.removePrivateFile(userUUID, relPath)
		return fmt.Errorf("私有存储不可用")
	}
	if err := b.CheckPrivateQuota(userUUID, size); err != nil {
		b.removePrivateFile(userUUID, relPath)
		return err
	}
	shareCode, err := file.GenerateShareCode()
	if err != nil {
		b.removePrivateFile(userUUID, relPath)
		return fmt.Errorf("生成分享码失败: %w", err)
	}
	now := utils.Now()
	rec := models.FileRecordPrivate{
		ShareCode: shareCode,
	}
	rec.FileName = fileName
	rec.FilePath = relPath
	rec.RootName = privateRootName
	rec.FullPath = "/" + privateRootName + relPath
	rec.FileSize = size
	rec.IsDir = false
	rec.Xxh3Hash = ""
	rec.HashStatus = "pending"
	rec.ModTime = now
	rec.LastSyncedAt = now
	rec.Status = models.FileStatusActive
	rec.OwnerID = userUUID
	rec.CreatedAt = now
	rec.UpdatedAt = now

	// 同名文件覆盖/重复上传：命中既有活跃记录则复用其分享码做更新，避免产生重复记录
	var exist models.FileRecordPrivate
	existed := b.db.Where("owner_id = ? AND file_path = ? AND status = ?",
		userUUID, relPath, models.FileStatusActive).
		Order("id LIMIT 1").First(&exist).Error == nil
	if existed {
		shareCode = exist.ShareCode
		rec.ShareCode = shareCode
		updates := map[string]interface{}{
			"file_name":   fileName,
			"full_path":   "/" + privateRootName + relPath,
			"file_size":   size,
			"xxh3_hash":   "",
			"hash_status": "pending",
			"mod_time":    now,
			"updated_at":  now,
		}
		if err := b.db.Model(&models.FileRecordPrivate{}).
			Where("id = ?", exist.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新私有文件记录失败: %w", err)
		}
		if b.indexSvc != nil {
			b.indexSvc.TriggerHash(context.Background())
		}
		return nil
	}

	if err := b.db.Create(&rec).Error; err != nil {
		b.removePrivateFile(userUUID, relPath)
		return fmt.Errorf("写入私有文件记录失败: %w", err)
	}
	if b.indexSvc != nil {
		b.indexSvc.TriggerHash(context.Background())
	}
	return nil
}

// SoftDeletePrivateFile 软删除私有文件记录。
func (b *webdavBackend) SoftDeletePrivateFile(userUUID, relPath string) error {
	if b.db == nil {
		return fmt.Errorf("数据库未就绪")
	}
	now := utils.Now()
	return b.db.Model(&models.FileRecordPrivate{}).
		Where("owner_id = ? AND file_path = ? AND status = ?", userUUID, relPath, models.FileStatusActive).
		Updates(map[string]interface{}{
			"status":     models.FileStatusDeleted,
			"deleted_at": now,
			"updated_at": now,
		}).Error
}

// RenamePrivateFile 同步私有文件重命名/移动后的记录路径。
func (b *webdavBackend) RenamePrivateFile(userUUID, oldRelPath, newRelPath string) error {
	if b.db == nil {
		return fmt.Errorf("数据库未就绪")
	}
	now := utils.Now()
	return b.db.Model(&models.FileRecordPrivate{}).
		Where("owner_id = ? AND file_path = ? AND status = ?", userUUID, oldRelPath, models.FileStatusActive).
		Updates(map[string]interface{}{
			"file_path":  newRelPath,
			"full_path":  "/" + privateRootName + newRelPath,
			"updated_at": now,
		}).Error
}

// removePrivateFile 删除已落盘的私有文件（配额/记录写入失败时回滚）。
func (b *webdavBackend) removePrivateFile(userUUID, relPath string) {
	if b.privatePath == "" || userUUID == "" || relPath == "" || relPath == "/" {
		return
	}
	p := filepath.Join(b.privatePath, "users", userUUID, strings.TrimPrefix(relPath, "/"))
	_ = os.Remove(p)
}
