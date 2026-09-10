package temp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

// Finalizer 临时上传的收尾策略：
// 目标文件按码哈希命名、唯一且不可被覆盖；落盘后写 temp_files 与 file_records_temp
// （码 32hex 作为分享/下载凭证，与存储布局解耦），清理会话与分片并触发哈希。
type Finalizer struct {
	db                *gorm.DB
	indexSvc          *index.Service
	sessionRepo       *repositories.SessionRepository
	chunkRepo         *repositories.ChunkRepository
	basePath          string
	defaultExpireDays int
}

// NewFinalizer 创建临时上传的收尾策略。
func NewFinalizer(db *gorm.DB, indexSvc *index.Service, basePath string, defaultExpireDays int) *Finalizer {
	return &Finalizer{
		db:                db,
		indexSvc:          indexSvc,
		sessionRepo:       repositories.NewSessionRepository(db),
		chunkRepo:         repositories.NewChunkRepository(db),
		basePath:          basePath,
		defaultExpireDays: defaultExpireDays,
	}
}

// AllowsOverwrite 临时上传的文件名由访问码哈希派生、全场唯一，无需覆盖。
func (*Finalizer) AllowsOverwrite(_ *models.UploadSession, _ bool) bool {
	return false
}

// Complete 临时上传的完成收尾。
func (f *Finalizer) Complete(session *models.UploadSession, targetPath string) (*services.FinalizeResult, error) {
	now := utils.Now()

	// 计算临时文件过期时间（会话过期只约束上传过程，产物有效期用配置的默认天数）
	expireDays := f.defaultExpireDays
	if expireDays <= 0 {
		expireDays = 7
	}
	expiredAt := now.Add(time.Duration(expireDays) * 24 * time.Hour)

	cleanDir := ""
	if dp := strings.Trim(strings.TrimSuffix(session.TargetPath, "/"), `/\`); dp != "" {
		cleanDir = dp
	}

	tempFile := &models.TempFile{
		Code:             session.Code,
		Filename:         session.FileName,
		FileSize:         session.FileSize,
		FilePath:         targetPath,
		ClientIP:         session.ClientIP,
		Dir:              cleanDir,
		DeleteOnDownload: session.DeleteOnDownload,
		ExpiredAt:        expiredAt,
	}
	if err := f.db.Create(tempFile).Error; err != nil {
		os.Remove(targetPath)
		return nil, fmt.Errorf("保存临时文件记录失败: %w", err)
	}

	// 同步到临时文件索引表（与简单上传一致，码作为 file_path）
	tempRecord := models.FileRecordTemp{
		FileRecordBase: models.FileRecordBase{
			FileName:     session.FileName,
			FilePath:     session.Code,
			RootName:     "temp",
			FullPath:     "/temp/" + session.Code,
			FileSize:     session.FileSize,
			IsDir:        false,
			ModTime:      now,
			Status:       models.FileStatusActive,
			OwnerID:      session.ClientIP,
			LastSyncedAt: now,
		},
	}
	if err := f.db.Create(&tempRecord).Error; err != nil {
		utils.Warn("同步临时文件索引失败", utils.String("code", session.Code), utils.Err(err))
	}

	_ = f.sessionRepo.UpdateStatus(session.ID, models.UploadStatusCompleted)
	_ = f.chunkRepo.DeleteBySessionID(session.ID)
	_ = f.sessionRepo.Delete(session.ID)

	if f.indexSvc != nil {
		// 触发哈希计算（后台执行，不阻塞）
		f.indexSvc.TriggerHash(context.Background())
	}

	return &services.FinalizeResult{
		TargetPath: session.Code,
		FileName:   session.FileName,
		ShareCode:  session.Code,
	}, nil
}
