package ftp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ftpfs "fuzhan/internal/access/ftp"
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

// ftpBackend 实现 ftpfs.FtpBackend：将 FTP 虚拟文件系统的权限/索引/配额
// 委托给 Web 上传体系，避免 FTP 成为绕过权限模型的旁路。
type ftpBackend struct {
	db          *gorm.DB
	indexSvc    *index.Service
	privatePath string // 私有存储根路径（未启用时为空串）
}

// NewFtpBackend 创建 FTP 后端能力实现。
// privatePath 为私有存储根路径，未启用私有存储时传空串。
func NewFtpBackend(db *gorm.DB, indexSvc *index.Service, privatePath string) ftpfs.FtpBackend {
	return &ftpBackend{db: db, indexSvc: indexSvc, privatePath: privatePath}
}

// IsAdmin 按用户 UUID 查询角色（匿名/未知返回 false）。
func (b *ftpBackend) IsAdmin(userUUID string) bool {
	if userUUID == "" || b.db == nil {
		return false
	}
	var user models.User
	if err := b.db.Select("role").Where("uuid = ?", userUUID).First(&user).Error; err != nil {
		return false
	}
	return user.Role == "admin"
}

// PublicUploaderIP 返回公开文件索引记录中的上传者 IP（无记录返回空串）。
func (b *ftpBackend) PublicUploaderIP(rootName, relPath string) string {
	if b.db == nil || rootName == "" || relPath == "" || relPath == "/" {
		return ""
	}
	var rec models.FileRecordPublic
	if err := b.db.Select("uploader_ip").
		Where("root_name = ? AND file_path = ? AND status = ? AND deleted_at IS NULL",
			rootName, relPath, models.FileStatusActive).
		First(&rec).Error; err != nil {
		return ""
	}
	return rec.UploaderIP
}

// UploadEnabled 公共目录上传总开关（与 Web 上传一致，不豁免管理员）。
func (b *ftpBackend) UploadEnabled() bool {
	return appconfig.GlobalConfig.Upload.Enabled
}

// CheckDiskSpace 检查路径所在分区剩余空间。
func (b *ftpBackend) CheckDiskSpace(path string, required int64) error {
	return services.CheckDiskSpace(path, required)
}

// CheckPrivateQuota 检查用户私有存储配额（配额 <=0 视为不限制）。
func (b *ftpBackend) CheckPrivateQuota(userUUID string, addSize int64) error {
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

// SyncPublicFile 将公开文件同步进索引：写 file_records_public、标记最近已同步
// （防止文件监听器重复处理）、更新上传者 IP、触发哈希。
func (b *ftpBackend) SyncPublicFile(rootName, relPath, uploaderIP string) {
	if b.indexSvc == nil {
		return
	}
	if err := b.indexSvc.SyncFile(rootName, relPath); err != nil {
		utils.Warn("FTP上传后同步索引失败",
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
			utils.Warn("FTP写入上传者IP失败",
				utils.String("root", rootName),
				utils.String("path", relPath),
				utils.Err(err))
		}
	}
	b.indexSvc.TriggerHash(context.Background())
}

// RemovePublicFile 软删除公开文件索引。
func (b *ftpBackend) RemovePublicFile(rootName, relPath string) {
	if b.indexSvc == nil {
		return
	}
	if err := b.indexSvc.RemoveFile(rootName, relPath); err != nil {
		utils.Warn("FTP删除后同步索引失败",
			utils.String("root", rootName),
			utils.String("path", relPath),
			utils.Err(err))
	}
}

// MovePublicFile 同步公开文件重命名/移动后的索引路径。
func (b *ftpBackend) MovePublicFile(rootName, oldRelPath, newRelPath string) {
	if b.indexSvc == nil {
		return
	}
	if err := b.indexSvc.MoveFile(rootName, oldRelPath, newRelPath); err != nil {
		utils.Warn("FTP重命名后同步索引失败",
			utils.String("root", rootName),
			utils.String("oldPath", oldRelPath),
			utils.String("newPath", newRelPath),
			utils.Err(err))
	}
}

// AddPrivateFile 写入私有文件记录（含分享码）并触发哈希。
// 真实大小配额检查在此时执行：超出则删除已落盘文件并返回错误，
// 由调用方（recordingFile.Close）将错误回传 FTP 客户端。
func (b *ftpBackend) AddPrivateFile(userUUID, relPath, fileName string, size int64) error {
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
func (b *ftpBackend) SoftDeletePrivateFile(userUUID, relPath string) error {
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
func (b *ftpBackend) RenamePrivateFile(userUUID, oldRelPath, newRelPath string) error {
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
func (b *ftpBackend) removePrivateFile(userUUID, relPath string) {
	if b.privatePath == "" || userUUID == "" || relPath == "" || relPath == "/" {
		return
	}
	p := filepath.Join(b.privatePath, "users", userUUID, strings.TrimPrefix(relPath, "/"))
	_ = os.Remove(p)
}
