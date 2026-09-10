package services

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"

	"github.com/google/uuid"
	"github.com/zeebo/xxh3"
	"gorm.io/gorm"
)

// CreateSessionReq 创建会话请求
type CreateSessionReq struct {
	Filename   string
	FileSize   int64
	Dir        string
	RootName   string
	TargetType models.TargetType
	UserID     string
	ClientIP   string
	// Code 访问码（仅临时上传传入，作为命名与分享/下载凭证）
	Code string
	// DeleteOnDownload 下载后自动删除（仅临时上传使用）
	DeleteOnDownload bool
}

// UploadChunkReq 上传分片请求
type UploadChunkReq struct {
	UploadID   uint
	ChunkIndex int
	ChunkData  io.ReadCloser
	ChunkSize  int64
	Checksum   string
}

// UploadStatus 上传状态
type UploadStatus struct {
	ID              uint                `json:"id"`
	FileName        string              `json:"fileName"`
	FileSize        int64               `json:"fileSize"`
	ChunkSize       int64               `json:"chunkSize"`
	Status          models.UploadStatus `json:"status"`
	TotalChunks     int                 `json:"totalChunks"`
	UploadedIndexes []int               `json:"uploadedIndexes"`
	Expired         bool                `json:"expired"`
	ExpiredAt       time.Time           `json:"expiredAt"`
}

// FinalizeResult 完成上传结果
type FinalizeResult struct {
	TargetPath string `json:"targetPath"`
	FileName   string `json:"fileName"`
	// ShareCode 仅私有上传回填（作为分享/下载凭证）。公开上传为空。
	ShareCode string `json:"shareCode,omitempty"`
}

// UploadSessionService 上传会话服务
type UploadSessionService struct {
	db          *gorm.DB
	sessionRepo *repositories.SessionRepository
	chunkRepo   *repositories.ChunkRepository
	recordRepo  repositories.AuditStore
	indexSvc    *index.Service
	chunkSize   int64
	storage     UploadStorage
	finalizer   UploadFinalizer
	// targetType 本服务负责清理的上传类型（每次仅处理归属自己的会话，
	// 避免统一会话表被多存储服务交叉清理时用错落盘策略）。
	targetType models.TargetType
	// sessionLocks 分片条纹锁：同一会话的分片/收尾/取消需互斥，不同会话并行。
	// 用固定 64 条条纹按 uploadId 取模，内存有界、无需清理，个别条纹碰撞仅轻微串行。
	sessionLocks [64]sync.Mutex
}

// lockSession 获取指定会话的条纹锁，返回释放函数。
func (s *UploadSessionService) lockSession(id uint) func() {
	mu := &s.sessionLocks[id%uint(len(s.sessionLocks))]
	mu.Lock()
	return mu.Unlock
}

// NewUploadSessionService 创建上传会话服务实例（公开上传的存储与收尾策略，清理范围为公开/遗留）
func NewUploadSessionService(db *gorm.DB, chunkSize int64, indexSvc *index.Service) *UploadSessionService {
	return &UploadSessionService{
		db:          db,
		sessionRepo: repositories.NewSessionRepository(db),
		chunkRepo:   repositories.NewChunkRepository(db),
		recordRepo:  repositories.NewRecordRepository(db),
		indexSvc:    indexSvc,
		chunkSize:   chunkSize,
		storage:     PublicStorage{},
		finalizer:   NewPublicFinalizer(db, indexSvc),
		targetType:  models.TargetTypeRegular,
	}
}

// NewUploadSessionServiceWithPolicies 创建指定落盘与收尾策略的上传会话服务实例，
// 供私有/临时等需要差异化策略的存储复用同一分片上传流程；targetType 标识本服务
// 负责清理的上传类型。
func NewUploadSessionServiceWithPolicies(db *gorm.DB, chunkSize int64, indexSvc *index.Service, storage UploadStorage, finalizer UploadFinalizer, targetType models.TargetType) *UploadSessionService {
	return &UploadSessionService{
		db:          db,
		sessionRepo: repositories.NewSessionRepository(db),
		chunkRepo:   repositories.NewChunkRepository(db),
		recordRepo:  repositories.NewRecordRepository(db),
		indexSvc:    indexSvc,
		chunkSize:   chunkSize,
		storage:     storage,
		finalizer:   finalizer,
		targetType:  targetType,
	}
}

// CreateSession 创建上传会话
func (s *UploadSessionService) CreateSession(req *CreateSessionReq) (*models.UploadSession, error) {
	// 检查磁盘空间
	if req.FileSize <= 0 {
		return nil, fmt.Errorf("文件大小必须大于 0")
	}
	if err := s.checkDiskSpace(req.FileSize, req.RootName); err != nil {
		return nil, err
	}

	// 溢出安全: req.FileSize>0 已校验, 用 (FileSize-1)/chunkSize+1 求向上取整, 避免 FileSize+chunkSize-1 整数溢出
	totalChunks := int((req.FileSize-1)/s.chunkSize + 1)
	uploadID := uuid.New().String()

	session := &models.UploadSession{
		FileName:         req.Filename,
		FileSize:         req.FileSize,
		ChunkSize:        s.chunkSize,
		TotalChunks:      totalChunks,
		Status:           models.UploadStatusPending,
		TargetType:       req.TargetType,
		TargetPath:       req.Dir,
		TargetRoot:       req.RootName,
		UserID:           req.UserID,
		ClientIP:         req.ClientIP,
		ChunkDir:         uploadID,
		Code:             req.Code,
		DeleteOnDownload: req.DeleteOnDownload,
		ExpiredAt:        utils.Now().Add(24 * time.Hour),
	}

	if err := s.sessionRepo.Create(session); err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	return session, nil
}

// UploadChunk 上传分片
func (s *UploadSessionService) UploadChunk(req *UploadChunkReq) error {
	unlock := s.lockSession(req.UploadID)
	defer unlock()

	session, err := s.sessionRepo.GetByID(req.UploadID)
	if err != nil {
		return fmt.Errorf("会话不存在: %w", err)
	}

	// 检查会话状态
	if session.Status == models.UploadStatusExpired ||
		session.Status == models.UploadStatusCompleted ||
		session.Status == models.UploadStatusCancelled {
		return fmt.Errorf("会话状态不允许上传")
	}

	if session.ExpiredAt.Before(utils.Now()) {
		_ = s.sessionRepo.UpdateStatus(session.ID, models.UploadStatusExpired)
		return fmt.Errorf("会话已过期")
	}

	if req.ChunkIndex < 0 || req.ChunkIndex >= session.TotalChunks {
		return fmt.Errorf("无效的分片索引: %d", req.ChunkIndex)
	}

	_, uploadingPath, err := s.storage.Paths(session)
	if err != nil {
		return fmt.Errorf("构建目标路径失败: %w", err)
	}
	if err := EnsureUploadDir(uploadingPath); err != nil {
		return err
	}

	// 计算预期分片大小
	offset := int64(req.ChunkIndex) * s.chunkSize
	expectedChunkSize := s.chunkSize
	if req.ChunkIndex == session.TotalChunks-1 {
		expectedChunkSize = session.FileSize - offset
	}
	if expectedChunkSize <= 0 {
		return fmt.Errorf("无效的分片大小: %d", expectedChunkSize)
	}

	// 限制读取大小；多读 1 字节以检测超大分片
	limitedReader := io.LimitReader(req.ChunkData, expectedChunkSize+1)
	chunkData, readErr := io.ReadAll(limitedReader)
	req.ChunkData.Close()
	if readErr != nil {
		return fmt.Errorf("读取分片数据失败: %w", readErr)
	}

	written := int64(len(chunkData))
	if written > expectedChunkSize {
		return fmt.Errorf("分片大小 %d 超过限制 %d", written, expectedChunkSize)
	}

	// xxh3 校验
	if req.Checksum != "" && len(chunkData) > 0 {
		actualHash := xxh3.Hash(chunkData)
		expectedHash, parseErr := strconv.ParseUint(req.Checksum, 16, 64)
		if parseErr != nil {
			return fmt.Errorf("无效的xxh3校验值: %s", req.Checksum)
		}
		if expectedHash != actualHash {
			return fmt.Errorf("xxh3校验失败: 期望值=%016X, 实际值=%016X", expectedHash, actualHash)
		}
	}

	// 创建/打开上传文件并写入
	if err := s.ensureUploadingFile(uploadingPath, session.FileSize); err != nil {
		utils.Error("创建上传文件失败", utils.String("path", uploadingPath), utils.Int64("size", session.FileSize), utils.Err(err))
		return fmt.Errorf("创建上传文件失败")
	}

	f, openErr := os.OpenFile(uploadingPath, os.O_RDWR|os.O_CREATE, 0644)
	if openErr != nil {
		utils.Error("打开上传文件失败", utils.String("path", uploadingPath), utils.Err(openErr))
		return fmt.Errorf("打开上传文件失败：%s", classifyUploadFileError(openErr))
	}
	defer f.Close()

	if _, writeErr := f.WriteAt(chunkData, offset); writeErr != nil {
		s.zeroOutChunk(f, offset, written)
		utils.Error("写入分片数据失败", utils.String("path", uploadingPath), utils.Int("chunkIndex", req.ChunkIndex), utils.Err(writeErr))
		return fmt.Errorf("写入分片数据失败")
	}

	// 记录分片（使用 DB 事务确保状态更新和分片记录原子写入）
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		// 更新会话状态
		if session.Status == models.UploadStatusPending {
			if err := tx.Model(&models.UploadSession{}).Where("id = ?", session.ID).Update("status", models.UploadStatusInProgress).Error; err != nil {
				return fmt.Errorf("更新会话状态失败: %w", err)
			}
		}
		// 自动刷新过期时间
		if err := tx.Model(&models.UploadSession{}).Where("id = ?", session.ID).Update("expired_at", utils.Now().Add(24*time.Hour)).Error; err != nil {
			return fmt.Errorf("刷新过期时间失败: %w", err)
		}
		// 记录分片
		chunk := &models.UploadedChunk{
			SessionID:  session.ID,
			ChunkIndex: req.ChunkIndex,
			ChunkSize:  written,
		}
		if err := tx.Create(chunk).Error; err != nil {
			return fmt.Errorf("保存分片记录失败: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return txErr
	}

	return nil
}

// GetStatus 获取上传状态
func (s *UploadSessionService) GetStatus(uploadID uint) (*UploadStatus, error) {
	session, err := s.sessionRepo.GetByID(uploadID)
	if err != nil {
		return nil, fmt.Errorf("会话不存在: %w", err)
	}

	uploadedIndexes, err := s.chunkRepo.GetUploadedIndexes(session.ID)
	if err != nil {
		return nil, fmt.Errorf("获取已上传分片索引失败: %w", err)
	}

	expired := session.Status == models.UploadStatusExpired ||
		(session.ExpiredAt.Before(utils.Now()) && session.Status != models.UploadStatusCompleted)

	return &UploadStatus{
		ID:              session.ID,
		FileName:        session.FileName,
		FileSize:        session.FileSize,
		ChunkSize:       session.ChunkSize,
		Status:          session.Status,
		TotalChunks:     session.TotalChunks,
		UploadedIndexes: uploadedIndexes,
		Expired:         expired,
		ExpiredAt:       session.ExpiredAt,
	}, nil
}

// ErrFileExists 表示目标文件已存在（无法覆盖时返回）
var ErrFileExists = fmt.Errorf("目标文件已存在")

// Finalize 完成上传
// allowOverwrite 为 true 时，若目标文件已存在则覆盖（仅限管理员）。
// 覆盖判定与落盘后的业务收尾委托给 UploadFinalizer 策略。
func (s *UploadSessionService) Finalize(uploadID uint, allowOverwrite bool) (*FinalizeResult, error) {
	unlock := s.lockSession(uploadID)
	defer unlock()

	session, err := s.sessionRepo.GetByID(uploadID)
	if err != nil {
		return nil, fmt.Errorf("会话不存在: %w", err)
	}

	uploadedChunks, err := s.chunkRepo.GetUploadedIndexes(session.ID)
	if err != nil {
		return nil, fmt.Errorf("获取分片信息失败: %w", err)
	}
	if len(uploadedChunks) != session.TotalChunks {
		return nil, fmt.Errorf("分片未全部上传: %d/%d", len(uploadedChunks), session.TotalChunks)
	}
	// 验证分片索引覆盖 [0, TotalChunks-1] 且无重复
	for i, idx := range uploadedChunks {
		if idx != i {
			return nil, fmt.Errorf("分片索引不完整或重复: 期望索引 %d, 实际 %d", i, idx)
		}
	}

	targetPath, uploadingPath, err := s.storage.Paths(session)
	if err != nil {
		return nil, fmt.Errorf("构建目标路径失败: %w", err)
	}
	if err := EnsureUploadDir(uploadingPath); err != nil {
		return nil, err
	}

	if _, err := os.Stat(targetPath); err == nil {
		// 目标已存在：按策略判定是否允许覆盖
		if !s.finalizer.AllowsOverwrite(session, allowOverwrite) {
			return nil, ErrFileExists
		}
		// 覆盖：先删除目标文件，再重命名临时文件
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			utils.Error("覆盖文件时删除失败", utils.String("path", targetPath), utils.Err(err))
			return nil, fmt.Errorf("无法覆盖已有文件: %w", err)
		}
		utils.Info("覆盖文件", utils.String("path", targetPath))
	}

	if _, err := os.Stat(uploadingPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("上传文件不存在，可能上传未完成")
	}

	// 重命名上传中的文件为正式文件名
	if err := os.Rename(uploadingPath, targetPath); err != nil {
		if copyErr := utils.CopyFile(uploadingPath, targetPath); copyErr != nil {
			utils.Error("完成上传失败", utils.String("src", uploadingPath), utils.String("dst", targetPath), utils.Err(copyErr))
			return nil, fmt.Errorf("完成上传失败")
		}
		_ = os.Remove(uploadingPath)
	}

	return s.finalizer.Complete(session, targetPath)
}

// Resume 续传会话
func (s *UploadSessionService) Resume(uploadID uint) (*UploadStatus, error) {
	session, err := s.sessionRepo.GetByID(uploadID)
	if err != nil {
		return nil, fmt.Errorf("会话不存在: %w", err)
	}

	if session.Status == models.UploadStatusCompleted || session.Status == models.UploadStatusCancelled {
		return nil, fmt.Errorf("会话已完成或已取消，无法续传")
	}

	session.ExpiredAt = utils.Now().Add(24 * time.Hour)
	session.Status = models.UploadStatusInProgress
	if err := s.sessionRepo.Update(session); err != nil {
		return nil, fmt.Errorf("续传失败: %w", err)
	}

	uploadedIndexes, err := s.chunkRepo.GetUploadedIndexes(session.ID)
	if err != nil {
		return nil, fmt.Errorf("获取已上传分片索引失败: %w", err)
	}

	return &UploadStatus{
		ID:              session.ID,
		FileName:        session.FileName,
		FileSize:        session.FileSize,
		ChunkSize:       session.ChunkSize,
		Status:          session.Status,
		TotalChunks:     session.TotalChunks,
		UploadedIndexes: uploadedIndexes,
		Expired:         false,
		ExpiredAt:       session.ExpiredAt,
	}, nil
}

// Cancel 取消上传
func (s *UploadSessionService) Cancel(uploadID uint) error {
	unlock := s.lockSession(uploadID)
	defer unlock()

	session, err := s.sessionRepo.GetByID(uploadID)
	if err != nil {
		return fmt.Errorf("会话不存在: %w", err)
	}

	_, uploadingPath, err := s.storage.Paths(session)
	if err == nil {
		if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
			utils.Warn("取消上传删除文件失败", utils.String("path", uploadingPath), utils.Err(rmErr))
		}
	}

	_ = s.chunkRepo.DeleteBySessionID(session.ID)
	_ = s.sessionRepo.Delete(session.ID)

	return nil
}

// VerifyOwner 校验调用方是否为会话持有者，防止凭可枚举的 uploadId 劫持会话。
// 公开与临时上传以客户端 IP 为身份；私有上传由专属 handler 按 user_id 校验，本方法放行。
func (s *UploadSessionService) VerifyOwner(uploadID uint, clientIP string) error {
	session, err := s.sessionRepo.GetByID(uploadID)
	if err != nil {
		return fmt.Errorf("会话不存在: %w", err)
	}
	if session.TargetType != models.TargetTypePrivate && session.ClientIP != "" && session.ClientIP != clientIP {
		return fmt.Errorf("无权操作该上传会话")
	}
	return nil
}
