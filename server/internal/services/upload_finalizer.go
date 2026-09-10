package services

import (
	"context"
	"fmt"
	"strings"

	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"fuzhan/pkg/pathutils"
	"gorm.io/gorm"
)

// UploadFinalizer 分片上传在"uploading 已落为正式文件"后的收尾策略。
// 分片上传的传输流程（会话/分片/续传/落盘）与业务无关；差异收敛到收尾：
// 目标已存在时是否允许覆盖，以及落盘后写哪张索引/操作记录/元数据、是否触发哈希。
type UploadFinalizer interface {
	// AllowsOverwrite 目标文件已存在时是否允许覆盖。
	// allowOverwrite 为调用方传入的放宽标志（公开上传中来自管理员身份）。
	AllowsOverwrite(session *models.UploadSession, allowOverwrite bool) bool

	// Complete 在 uploading 已重命名为 targetPath 之后执行收尾：
	// 写业务记录/索引/元数据、清理会话与分片、触发哈希。
	// 返回客户可见结果。
	Complete(session *models.UploadSession, targetPath string) (*FinalizeResult, error)
}

// PublicFinalizer 公开上传的收尾策略：
// 目标存在时按"管理员或上传者IP一致"放行覆盖；落盘后写操作记录、同步公开索引、
// 记录上传者IP、触发哈希。与上传流程解耦，便于私有等存储复用同一流程。
type PublicFinalizer struct {
	db          *gorm.DB
	recordRepo  repositories.AuditStore
	indexSvc    *index.Service
	sessionRepo *repositories.SessionRepository
	chunkRepo   *repositories.ChunkRepository
}

// NewPublicFinalizer 创建公开上传的收尾策略。
func NewPublicFinalizer(db *gorm.DB, indexSvc *index.Service) *PublicFinalizer {
	return &PublicFinalizer{
		db:          db,
		recordRepo:  repositories.NewRecordRepository(db),
		indexSvc:    indexSvc,
		sessionRepo: repositories.NewSessionRepository(db),
		chunkRepo:   repositories.NewChunkRepository(db),
	}
}

// AllowsOverwrite 公开上传：管理员放行；非管理员仅当上传者自身IP与现有记录一致时放行。
func (f *PublicFinalizer) AllowsOverwrite(session *models.UploadSession, allowOverwrite bool) bool {
	if allowOverwrite {
		return true
	}
	if session.ClientIP == "" {
		return false
	}
	idxRelPath := uploadRelPath(session)
	var existing models.FileRecordPublic
	if err := f.db.Where("root_name = ? AND file_path = ? AND status = ?",
		session.TargetRoot, idxRelPath, models.FileStatusActive).First(&existing).Error; err == nil &&
		existing.UploaderIP != "" && existing.UploaderIP == session.ClientIP {
		utils.Info("上传者IP一致，允许覆盖", utils.String("path", session.FileName))
		return true
	}
	return false
}

// Complete 公开上传的完成收尾。
func (f *PublicFinalizer) Complete(session *models.UploadSession, targetPath string) (*FinalizeResult, error) {
	_ = f.sessionRepo.UpdateStatus(session.ID, models.UploadStatusCompleted)

	// 创建上传记录
	relativePath := session.TargetPath
	if relativePath == "/" || relativePath == "" {
		relativePath = session.FileName
	} else {
		relativePath = strings.TrimSuffix(relativePath, "/")
		if !strings.HasPrefix(relativePath, "/") {
			relativePath = "/" + relativePath
		}
		relativePath = relativePath + "/" + session.FileName
	}
	record := &models.OperationRecord{
		FileName:   session.FileName,
		FileSize:   session.FileSize,
		FilePath:   relativePath,
		FullPath:   "/" + session.TargetRoot + "/" + strings.TrimPrefix(relativePath, "/"),
		RootName:   session.TargetRoot,
		FileType:   pathutils.GetFileType(session.FileName),
		ClientIP:   session.ClientIP,
		UserID:     session.UserID,
		UploadType: session.TargetType,
		UploadTime: utils.Now(),
	}
	if err := f.recordRepo.Create(record); err != nil {
		return nil, fmt.Errorf("创建上传记录失败: %w", err)
	}

	// 清理分片记录和会话记录
	_ = f.chunkRepo.DeleteBySessionID(session.ID)
	_ = f.sessionRepo.Delete(session.ID)

	// 同步到文件索引表（仅公共目录上传）
	if f.indexSvc != nil {
		if err := f.indexSvc.SyncFile(session.TargetRoot, relativePath); err != nil {
			utils.Warn("上传后同步索引失败",
				utils.String("root", session.TargetRoot),
				utils.String("path", relativePath),
				utils.Err(err))
		} else {
			f.indexSvc.MarkRecentlySynced(session.TargetRoot, relativePath)
			if record.ID > 0 {
				if fid, rerr := f.recordRepo.ResolvePublicFileID(session.TargetRoot, record.FullPath); rerr == nil && fid > 0 {
					if uerr := f.recordRepo.UpdateFileRecordID(record.ID, fid); uerr != nil {
						utils.Warn("上传记录关联索引ID失败", utils.Int64("record", int64(record.ID)), utils.Err(uerr))
					}
				}
			}
		}
		// 记录公开文件的上传者IP（覆盖上传会重新绑定归属）
		if session.ClientIP != "" {
			if uerr := f.db.Model(&models.FileRecordPublic{}).
				Where("root_name = ? AND file_path = ?", session.TargetRoot, uploadRelPath(session)).
				UpdateColumn("uploader_ip", session.ClientIP).Error; uerr != nil {
				utils.Warn("写入上传者IP失败",
					utils.String("root", session.TargetRoot),
					utils.String("path", uploadRelPath(session)),
					utils.Err(uerr))
			}
		}
		// 触发哈希计算（后台执行，不阻塞）
		f.indexSvc.TriggerHash(context.Background())
	}

	return &FinalizeResult{
		TargetPath: relativePath,
		FileName:   session.FileName,
	}, nil
}

// uploadRelPath 计算目标文件在索引表中的 file_path（带前导 /），
// 供覆盖权限查询与归属写入复用。
func uploadRelPath(session *models.UploadSession) string {
	rel := "/" + session.FileName
	if dp := strings.Trim(strings.TrimSuffix(session.TargetPath, "/"), "/"); dp != "" {
		rel = "/" + dp + "/" + session.FileName
	}
	return rel
}
