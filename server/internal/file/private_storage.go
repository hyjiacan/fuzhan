package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// PrivateStorage 私有上传的落盘路径策略：
// 目标为 `{private}/users/{uid}/{Dir}/{真实文件名}`，上传中临时文件为同目录
// `. {basename}.uploading`。分享码不参与路径构造，仅作为 DB 中的下载凭证。
type PrivateStorage struct {
	basePath string
}

// NewPrivateStorage 创建私有上传的落盘路径策略。
func NewPrivateStorage(basePath string) *PrivateStorage {
	return &PrivateStorage{basePath: basePath}
}

// Paths 实现私有上传的路径构建与越界校验。
func (st *PrivateStorage) Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
	basePath := st.basePath
	if basePath == "" {
		err = fmt.Errorf("私有存储未配置")
		return
	}
	userDir := GetPrivateUserPath(basePath, session.UserID)

	cleanDir := session.TargetPath
	if cleanDir == "" || cleanDir == "/" {
		cleanDir = ""
	} else if d, cerr := cleanPrivateDir(cleanDir); cerr == nil {
		cleanDir = d
	} else {
		err = cerr
		return
	}

	dir := filepath.Join(userDir, cleanDir)
	if err = validateUnderUserDir(dir, userDir); err != nil {
		return
	}

	cleanFilename := filepath.Clean(session.FileName)
	if strings.Contains(cleanFilename, "..") || strings.Contains(cleanFilename, "/") || strings.Contains(cleanFilename, `\`) {
		err = fmt.Errorf("无效的文件名")
		return
	}

	targetPath = filepath.Join(dir, cleanFilename)
	uploadingPath = filepath.Join(dir, "."+cleanFilename+".uploading")
	if err = validateUnderUserDir(filepath.Dir(targetPath), userDir); err != nil {
		return
	}
	return
}

// cleanPrivateDir 清洗私有子目录段：禁止绝对路径/上级跳转，返回清理后的相对目录。
func cleanPrivateDir(d string) (string, error) {
	d = strings.TrimSpace(d)
	d = strings.Trim(d, `/\`)
	d = filepath.Clean(d)
	if d == "." || d == "" {
		return "", nil
	}
	if filepath.IsAbs(d) || d == ".." || strings.HasPrefix(d, ".."+string(os.PathSeparator)) ||
		strings.Contains(d, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("无效的目标路径")
	}
	return d, nil
}

// validateUnderUserDir 确保绝对路径位于用户私有目录内，防止越界。
func validateUnderUserDir(absDir, userDir string) error {
	absDir, err := filepath.Abs(absDir)
	if err != nil {
		return fmt.Errorf("路径解析失败")
	}
	absRoot, err := filepath.Abs(userDir)
	if err != nil {
		return fmt.Errorf("根路径解析失败")
	}
	if absDir != absRoot && !strings.HasPrefix(absDir, absRoot+string(os.PathSeparator)) {
		return fmt.Errorf("路径越界")
	}
	return nil
}

// PrivateFinalizer 私有上传的收尾策略：
// 不允许覆盖（每次上传都是独立文件，生成新分享码）；落盘后写 file_records_private
// （含分享码），清理会话与分片，触发哈希。不写公开操作记录。
type PrivateFinalizer struct {
	db          *gorm.DB
	indexSvc    *index.Service
	sessionRepo *repositories.SessionRepository
	chunkRepo   *repositories.ChunkRepository
}

// NewPrivateFinalizer 创建私有上传的收尾策略。
func NewPrivateFinalizer(db *gorm.DB, indexSvc *index.Service) *PrivateFinalizer {
	return &PrivateFinalizer{
		db:          db,
		indexSvc:    indexSvc,
		sessionRepo: repositories.NewSessionRepository(db),
		chunkRepo:   repositories.NewChunkRepository(db),
	}
}

// AllowsOverwrite 私有上传不允许覆盖同名文件（每次上传生成独立分享码）。
func (*PrivateFinalizer) AllowsOverwrite(_ *models.UploadSession, _ bool) bool {
	return false
}

// Complete 私有上传的完成收尾。
func (f *PrivateFinalizer) Complete(session *models.UploadSession, _ string) (*services.FinalizeResult, error) {
	shareCode, err := GenerateShareCode()
	if err != nil {
		return nil, fmt.Errorf("生成分享码失败: %w", err)
	}

	relPath := privateRelRecordPath(session)
	now := utils.Now()
	rec := models.FileRecordPrivate{
		ShareCode: shareCode,
	}
	rec.FileName = session.FileName
	rec.FilePath = relPath
	rec.RootName = privateRootName
	rec.FullPath = "/" + privateRootName + relPath
	rec.FileSize = session.FileSize
	rec.IsDir = false
	rec.Xxh3Hash = ""
	rec.HashStatus = "pending"
	rec.ModTime = now
	rec.LastSyncedAt = now
	rec.Status = models.FileStatusActive
	rec.OwnerID = session.UserID
	rec.CreatedAt = now
	rec.UpdatedAt = now

	if err := f.db.Create(&rec).Error; err != nil {
		return nil, fmt.Errorf("写入私有文件记录失败: %w", err)
	}

	_ = f.sessionRepo.UpdateStatus(session.ID, models.UploadStatusCompleted)
	_ = f.chunkRepo.DeleteBySessionID(session.ID)
	_ = f.sessionRepo.Delete(session.ID)

	if f.indexSvc != nil {
		// 触发哈希计算（后台执行，不阻塞）
		f.indexSvc.TriggerHash(context.Background())
	}

	return &services.FinalizeResult{
		TargetPath: relPath,
		FileName:   session.FileName,
		ShareCode:  shareCode,
	}, nil
}

// privateRelRecordPath 私有文件在索引表中的 file_path（带前导 /）。
func privateRelRecordPath(session *models.UploadSession) string {
	rel := "/" + session.FileName
	if dp := strings.Trim(strings.TrimSuffix(session.TargetPath, "/"), "/"); dp != "" {
		rel = "/" + dp + "/" + session.FileName
	}
	return rel
}
