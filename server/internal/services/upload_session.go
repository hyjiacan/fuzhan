package services

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/storage"
	"fuzhan/internal/utils"
	xxh3pkg "fuzhan/pkg/xxh3"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// countingWriter 统计经其写入的字节数，用于流式落盘时获取实际写入量
type countingWriter struct {
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	cw.n += int64(len(p))
	return len(p), nil
}

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
	TargetPath  string `json:"targetPath"`
	FileName    string `json:"fileName"`
	Overwritten bool   `json:"overwritten"`
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
	// targetType 本服务负责清理的上传类型（每次仅清理归属自己的会话，
	// 避免统一会话表被多存储服务交叉清理时用错落盘策略）。
	targetType models.TargetType
	// storages 公开多根目录的 Storage 实例映射（rootName → Storage）。
	// 仅在远端存储后端接入分片上传降级时使用：UploadChunk 逐片写临时对象、
	// Finalize 按序合并。本地后端不注入（各 root 回退为 os 文件语义）。
	storages map[string]storage.Storage
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

// WithPublicStorages 注入公开多根目录的 Storage 映射（rootName → Storage）。
// 仅在远端存储后端下调用，使分片上传流程可按会话所属根目录选择 Storage 实例，
// 以"每片独立临时对象 + 最终合并"降级实现分片上传。本地后端无需调用。
func (s *UploadSessionService) WithPublicStorages(storages map[string]storage.Storage) *UploadSessionService {
	s.storages = storages
	return s
}

// storageFor 返回会话所属根目录的 Storage 实例。
// 优先使用注入的公开 Storage 映射；未注册时回退本地实现（单根/非远端场景），
// 行为与旧版 os 文件操作完全一致。
func (s *UploadSessionService) storageFor(session *models.UploadSession) (storage.Storage, bool) {
	if st, ok := s.storages[session.TargetRoot]; ok {
		return st, true
	}
	if rootPath, ok := appconfig.RootNames[session.TargetRoot]; ok {
		return storage.NewLocalStorage(rootPath), true
	}
	return nil, false
}

// validateUploadNoSymlink 校验上传落盘路径的每一层都不是符号链接（含 Windows 目录联接）。
// 上传会话创建、分片写入、完成落盘各校验一次，覆盖"会话创建后目标路径才被替换为链接"的窗口。
// 远端后端无本地路径语义，直接放行。
func (s *UploadSessionService) validateUploadNoSymlink(session *models.UploadSession, paths ...string) error {
	if IsRemoteSession(session) {
		return nil
	}
	root := s.storage.RootPath(session)
	if root == "" {
		return nil
	}
	for _, p := range paths {
		if err := utils.EnsureNoSymlinkPath(root, p); err != nil {
			return err
		}
	}
	return nil
}

// CreateSession 创建上传会话
func (s *UploadSessionService) CreateSession(req *CreateSessionReq) (*models.UploadSession, error) {
	// 检查磁盘空间
	if req.FileSize <= 0 {
		return nil, fmt.Errorf("文件大小必须大于 0")
	}
	// 全局单文件大小上限（0 = 不限）。公开/私有/临时共用本流程，
	// 在此统一拦截，避免各入口只校验磁盘/配额而漏掉 max_file_size。
	if maxSize := appconfig.GlobalConfig.Upload.MaxFileSize; maxSize > 0 && req.FileSize > maxSize {
		return nil, fmt.Errorf("文件大小 %d 超过最大限制 %d", req.FileSize, maxSize)
	}
	// 远端存储后端不检查本地磁盘空间（空间属于对象存储侧，本地检查无意义）
	if !appconfig.StorageBackendIsRemote() {
		if err := s.checkDiskSpace(req.FileSize, req.RootName); err != nil {
			return nil, err
		}
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

	// 落盘前先做符号链接校验，避免上传写入链接指向的位置；远端后端不适用本地路径语义
	targetPath, uploadingPath, err := s.storage.Paths(session)
	if err != nil {
		return nil, fmt.Errorf("构建目标路径失败: %w", err)
	}
	if err := s.validateUploadNoSymlink(session, targetPath, uploadingPath); err != nil {
		return nil, err
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
	if err := s.validateUploadNoSymlink(session, uploadingPath); err != nil {
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

	// 远端后端：每片写入独立临时对象（对象存储无随机写，Close 时一次性提交）。
	if IsRemoteSession(session) {
		st, ok := s.storageFor(session)
		if !ok {
			return fmt.Errorf("远端存储实例未就绪")
		}
		return s.uploadChunkRemote(st, session, req, expectedChunkSize)
	}

	// 本地后端：单 uploading 文件随机写
	if err := EnsureUploadDir(uploadingPath); err != nil {
		return err
	}

	// 限制读取大小；多读 1 字节以检测超大分片
	limited := io.LimitReader(req.ChunkData, expectedChunkSize+1)

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

	if _, seekErr := f.Seek(offset, io.SeekStart); seekErr != nil {
		utils.Error("定位分片写入位置失败", utils.String("path", uploadingPath), utils.Int("chunkIndex", req.ChunkIndex), utils.Err(seekErr))
		return fmt.Errorf("定位分片写入位置失败")
	}

	// 流式写入：以 64KB 缓冲边写边算 xxh3，避免整片读入堆内存，
	// 并发大文件上传时显著降低峰值内存与 GC 压力。
	written, streamErr := streamChunkData(f, limited, req.Checksum)
	req.ChunkData.Close()

	if streamErr != nil {
		s.zeroOutChunk(f, offset, written)
		utils.Error("写入分片数据失败", utils.String("path", uploadingPath), utils.Int("chunkIndex", req.ChunkIndex), utils.Err(streamErr))
		return streamErr
	}

	// 检测超大分片（流式写入已多读 1 字节，超过即回滚本片）
	if written > expectedChunkSize {
		s.zeroOutChunk(f, offset, written)
		return fmt.Errorf("分片大小 %d 超过限制 %d", written, expectedChunkSize)
	}

	return s.recordUploadedChunk(session, req, written)
}

// uploadChunkRemote 远端存储后端的分片写入。
// 每片写入一个独立的临时对象（.chunks/{ChunkDir}/{index}.part），Close 时一次性提交；
// 乱序上传互不影响（对象存储无文件句柄/随机写）。失败或超大片时删除该分片对象回滚。
func (s *UploadSessionService) uploadChunkRemote(st storage.Storage, session *models.UploadSession, req *UploadChunkReq, expectedChunkSize int64) error {
	chunkDir, chunkObject, ok := s.storage.ChunkObjectPaths(session, req.ChunkIndex)
	if !ok {
		return fmt.Errorf("构建远端分片路径失败")
	}

	// 确保分片目录存在（S3/OSS 无目录为 no-op，WebDAV 需先建 collection）
	if err := st.MkdirAll(context.Background(), chunkDir); err != nil {
		utils.Error("创建远端分片目录失败", utils.String("dir", chunkDir), utils.Int("chunkIndex", req.ChunkIndex), utils.Err(err))
		return fmt.Errorf("创建远端分片目录失败")
	}

	w, err := st.OpenWriter(context.Background(), chunkObject)
	if err != nil {
		utils.Error("打开远端分片对象失败", utils.String("object", chunkObject), utils.Int("chunkIndex", req.ChunkIndex), utils.Err(err))
		return fmt.Errorf("打开远端分片对象失败")
	}

	limited := io.LimitReader(req.ChunkData, expectedChunkSize+1)
	written, streamErr := streamChunkData(w, limited, req.Checksum)
	req.ChunkData.Close()
	if streamErr == nil {
		streamErr = w.Close()
	}

	if streamErr != nil {
		_ = st.Remove(context.Background(), chunkObject)
		utils.Error("写入远端分片失败", utils.String("object", chunkObject), utils.Int("chunkIndex", req.ChunkIndex), utils.Err(streamErr))
		return fmt.Errorf("写入分片失败: %w", streamErr)
	}

	// 检测超大分片（多读 1 字节）；超量则删除本分片对象回滚
	if written > expectedChunkSize {
		_ = st.Remove(context.Background(), chunkObject)
		return fmt.Errorf("分片大小 %d 超过限制 %d", written, expectedChunkSize)
	}

	return s.recordUploadedChunk(session, req, written)
}

// streamChunkData 以流式方式将受限读取源写入 dst，边写边统计实际字节数；
// checksum 非空时校验 xxh3。返回实际写入字节数（可能含超大片多读的 1 字节）。
func streamChunkData(dst io.Writer, limited io.Reader, checksum string) (written int64, err error) {
	cw := &countingWriter{}
	if checksum != "" {
		tee := io.TeeReader(limited, io.MultiWriter(dst, cw))
		hashHex, herr := xxh3pkg.HashReader(tee)
		err = herr
		written = cw.n
		if err == nil && written > 0 {
			expectedHash, parseErr := strconv.ParseUint(checksum, 16, 64)
			if parseErr != nil {
				err = fmt.Errorf("无效的xxh3校验值: %s", checksum)
			} else {
				actualHash, _ := strconv.ParseUint(hashHex, 16, 64)
				if expectedHash != actualHash {
					err = fmt.Errorf("xxh3校验失败: 期望值=%016X, 实际值=%016X", expectedHash, actualHash)
				}
			}
		}
	} else {
		buf := make([]byte, 64*1024)
		written, err = io.CopyBuffer(io.MultiWriter(dst, cw), limited, buf)
	}
	return written, err
}

// recordUploadedChunk 记录已上传分片：更新会话状态与过期时间、写入分片记录（原子）。
func (s *UploadSessionService) recordUploadedChunk(session *models.UploadSession, req *UploadChunkReq, written int64) error {
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
	return txErr
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

	// 覆盖上传用 os.Rename 原子替换目标文件时，Windows 监听器会先上报目标路径的 Remove。
	// 落盘前把目标登记为"上传中"，watcher 对这段窗口内的 Remove 事件予以忽略，
	// 避免刚同步好的索引记录被误删（文件仍在磁盘上却从列表消失）。
	if s.indexSvc != nil && !IsRemoteSession(session) && session.TargetType == models.TargetTypeRegular {
		unregister := s.indexSvc.BeginUploadTarget(session.TargetRoot, uploadRelPath(session))
		defer unregister()
	}

	// 落盘前再次校验符号链接，防止会话创建后目标路径被替换为链接
	if err := s.validateUploadNoSymlink(session, targetPath, uploadingPath); err != nil {
		return nil, err
	}

	// 远端后端：分片为独立临时对象，按序流式合并为最终对象后再清理分片目录。
	// 本地后端保留"单 uploading 文件随机写 + 重命名"语义。
	if IsRemoteSession(session) {
		return s.finalizeRemote(session, targetPath, allowOverwrite, uploadedChunks)
	}

	if err := EnsureUploadDir(uploadingPath); err != nil {
		return nil, err
	}

	// 判断目标是否已存在：存在时按策略判定是否允许覆盖（仅当允许覆盖时才可替换目标）
	if _, err := os.Stat(targetPath); err == nil {
		if !s.finalizer.AllowsOverwrite(session, allowOverwrite) {
			return nil, ErrFileExists
		}
		// 标记本次上传为覆盖，供收尾写操作记录时标注
		session.Overwritten = true
		utils.Info("覆盖文件", utils.String("path", targetPath))
	}

	if _, err := os.Stat(uploadingPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("上传文件不存在，可能上传未完成")
	}

	// 用 os.Rename 原子替换目标：目标已存在时在 Windows/Linux 上均会直接覆盖为临时文件内容，
	// 不再「先删除再重命名」——那样在 Remove 到 Rename 之间存在窗口，期间进程崩溃或重命名失败
	// 会导致原文件短暂消失甚至永久丢失。改为单次重命名后：原文件在替换完成前始终在场，
	// 失败路径（如跨设备 EXDEV）下目标也未被触碰，仅回退为复制，不会因移除而丢失原文件。
	if err := os.Rename(uploadingPath, targetPath); err != nil {
		if copyErr := utils.CopyFile(uploadingPath, targetPath); copyErr != nil {
			utils.Error("完成上传失败", utils.String("src", uploadingPath), utils.String("dst", targetPath), utils.Err(copyErr))
			return nil, fmt.Errorf("完成上传失败")
		}
		_ = os.Remove(uploadingPath)
	}

	return s.finalizer.Complete(session, targetPath)
}

// finalizeRemote 远端存储后端的完成上传。
// 分片已各自落为 .chunks/{ChunkDir}/{index}.part 临时对象，按序流式合并为
// 最终对象（目标已存在时按策略判定覆盖），合并完成后删除整个分片目录。
func (s *UploadSessionService) finalizeRemote(session *models.UploadSession, targetPath string, allowOverwrite bool, uploadedChunks []int) (*FinalizeResult, error) {
	st, ok := s.storageFor(session)
	if !ok {
		return nil, fmt.Errorf("远端存储实例未就绪")
	}
	ctx := context.Background()

	if _, err := st.Stat(ctx, targetPath); err == nil {
		// 目标已存在：按策略判定是否允许覆盖
		if !s.finalizer.AllowsOverwrite(session, allowOverwrite) {
			return nil, ErrFileExists
		}
		// 覆盖：删除目标对象，随后合并写入
		if err := st.Remove(ctx, targetPath); err != nil && !storage.IsNotExist(err) {
			utils.Error("覆盖文件时删除失败", utils.String("path", targetPath), utils.Err(err))
			return nil, fmt.Errorf("无法覆盖已有文件: %w", err)
		}
		// 标记本次上传为覆盖，供收尾写操作记录时标注
		session.Overwritten = true
		utils.Info("覆盖文件", utils.String("path", targetPath))
	}

	chunkDir, _, _ := s.storage.ChunkObjectPaths(session, 0)
	w, err := st.OpenWriter(ctx, targetPath)
	if err != nil {
		return nil, fmt.Errorf("打开远端目标对象失败: %w", err)
	}

	// 按序合并所有分片（uploadedChunks 已校验覆盖 [0, TotalChunks-1] 且有序）
	for _, idx := range uploadedChunks {
		_, chunkObject, _ := s.storage.ChunkObjectPaths(session, idx)
		rc, oerr := st.Open(ctx, chunkObject)
		if oerr != nil {
			w.Close()
			_ = st.Remove(ctx, targetPath)
			utils.Error("打开远端分片对象失败", utils.String("object", chunkObject), utils.Err(oerr))
			return nil, fmt.Errorf("合并分片失败: %w", oerr)
		}
		if _, cerr := io.Copy(w, rc); cerr != nil {
			rc.Close()
			w.Close()
			_ = st.Remove(ctx, targetPath)
			utils.Error("合并远端分片失败", utils.String("object", chunkObject), utils.Err(cerr))
			return nil, fmt.Errorf("合并分片失败: %w", cerr)
		}
		rc.Close()
	}

	if err := w.Close(); err != nil {
		_ = st.Remove(ctx, targetPath)
		utils.Error("提交远端目标对象失败", utils.String("path", targetPath), utils.Err(err))
		return nil, fmt.Errorf("完成上传失败")
	}

	// 清理分片目录
	if chunkDir != "" {
		if rmErr := st.RemoveAll(ctx, chunkDir); rmErr != nil {
			utils.Warn("清理远端分片目录失败", utils.String("dir", chunkDir), utils.Err(rmErr))
		}
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
		// 远端后端：清理整个分片目录（每片独立临时对象）；
		// 本地后端：删除单 uploading 临时文件。
		if IsRemoteSession(session) {
			if chunkDir, _, ok := s.storage.ChunkObjectPaths(session, 0); ok {
				if st, sok := s.storageFor(session); sok {
					if rmErr := st.RemoveAll(context.Background(), chunkDir); rmErr != nil {
						utils.Warn("取消上传清理远端分片失败", utils.String("dir", chunkDir), utils.Err(rmErr))
					}
				}
			}
		} else if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
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
