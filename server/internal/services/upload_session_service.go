package services

import (
    "context"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
    "github.com/zeebo/xxh3"
    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/models"
    "fuzhan/pkg/pathutils"
    "fuzhan/internal/repositories"
    "fuzhan/internal/utils"
    "fuzhan/internal/index"
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
}

// UploadSessionService 上传会话服务
type UploadSessionService struct {
    db          *gorm.DB
    sessionRepo *repositories.SessionRepository
    chunkRepo   *repositories.ChunkRepository
    recordRepo  repositories.AuditStore
    indexSvc    *index.Service
    chunkSize   int64
    mu          sync.Mutex
}

// NewUploadSessionService 创建上传会话服务实例
func NewUploadSessionService(db *gorm.DB, chunkSize int64, indexSvc *index.Service) *UploadSessionService {
    return &UploadSessionService{
        db:          db,
        sessionRepo: repositories.NewSessionRepository(db),
        chunkRepo:   repositories.NewChunkRepository(db),
        recordRepo:  repositories.NewRecordRepository(db),
        indexSvc:    indexSvc,
        chunkSize:   chunkSize,
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
        FileName:    req.Filename,
        FileSize:    req.FileSize,
        ChunkSize:   s.chunkSize,
        TotalChunks: totalChunks,
        Status:      models.UploadStatusPending,
        TargetType:  req.TargetType,
        TargetPath:  req.Dir,
        TargetRoot:  req.RootName,
        UserID:      req.UserID,
        ClientIP:    req.ClientIP,
        ChunkDir:    uploadID,
        ExpiredAt:   time.Now().Add(24 * time.Hour),
    }

    if err := s.sessionRepo.Create(session); err != nil {
        return nil, fmt.Errorf("创建会话失败: %w", err)
    }

    return session, nil
}

// UploadChunk 上传分片
func (s *UploadSessionService) UploadChunk(req *UploadChunkReq) error {
    s.mu.Lock()
    defer s.mu.Unlock()

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

    if session.ExpiredAt.Before(time.Now()) {
        _ = s.sessionRepo.UpdateStatus(session.ID, models.UploadStatusExpired)
        return fmt.Errorf("会话已过期")
    }

    if req.ChunkIndex < 0 || req.ChunkIndex >= session.TotalChunks {
        return fmt.Errorf("无效的分片索引: %d", req.ChunkIndex)
    }

    _, uploadingPath, err := s.buildTargetPath(session)
    if err != nil {
        return fmt.Errorf("构建目标路径失败: %w", err)
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
        if err := tx.Model(&models.UploadSession{}).Where("id = ?", session.ID).Update("expired_at", time.Now().Add(24*time.Hour)).Error; err != nil {
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
        (session.ExpiredAt.Before(time.Now()) && session.Status != models.UploadStatusCompleted)

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
// allowOverwrite 为 true 时，若目标文件已存在则覆盖（仅限管理员）
func (s *UploadSessionService) Finalize(uploadID uint, allowOverwrite bool) (*FinalizeResult, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

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

    targetPath, uploadingPath, err := s.buildTargetPath(session)
    if err != nil {
        return nil, fmt.Errorf("构建目标路径失败: %w", err)
    }

    // 目标文件在索引表中的 file_path（带前导 /），供覆盖权限与归属写入使用
    idxRelPath := "/" + session.FileName
    if dp := strings.Trim(strings.TrimSuffix(session.TargetPath, "/"), "/"); dp != "" {
        idxRelPath = "/" + dp + "/" + session.FileName
    }

    if _, err := os.Stat(targetPath); err == nil {
        // 覆盖权限：管理员放行，或（上传者IP与现有文件上传者IP一致）本人放行
        if !allowOverwrite && session.ClientIP != "" {
            var existing models.FileRecordPublic
            if serr := s.db.Where("root_name = ? AND file_path = ? AND status = ?",
                session.TargetRoot, idxRelPath, models.FileStatusActive).First(&existing).Error; serr == nil &&
                existing.UploaderIP != "" && existing.UploaderIP == session.ClientIP {
                allowOverwrite = true
                utils.Info("上传者IP一致，允许覆盖", utils.String("path", targetPath))
            }
        }
        if !allowOverwrite {
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

    _ = s.sessionRepo.UpdateStatus(session.ID, models.UploadStatusCompleted)

    // 创建上传记录
    relativePath := session.TargetPath
    if relativePath == "/" || relativePath == "" {
        relativePath = session.FileName
    } else {
        // 标准化：移除尾部斜杠
        relativePath = strings.TrimSuffix(relativePath, "/")
        // 确保有前导 /
        if !strings.HasPrefix(relativePath, "/") {
            relativePath = "/" + relativePath
        }
        relativePath = relativePath + "/" + session.FileName
    }
    record := &models.OperationRecord{
        FileName:   session.FileName,
        FileSize:   session.FileSize,
        FilePath:   relativePath,
        // FullPath = /RootName/FilePath（确保根目录文件也有分隔符）
        FullPath:   "/" + session.TargetRoot + "/" + strings.TrimPrefix(relativePath, "/"),
        RootName:   session.TargetRoot,
        FileType:   pathutils.GetFileType(session.FileName),
        ClientIP:   session.UserID,
        UserID:     session.UserID,
        UploadType: session.TargetType,
        UploadTime: time.Now(),
    }
    if err := s.recordRepo.Create(record); err != nil {
        return nil, fmt.Errorf("创建上传记录失败: %w", err)
    }

    // 清理分片记录和会话记录
    _ = s.chunkRepo.DeleteBySessionID(session.ID)
    _ = s.sessionRepo.Delete(session.ID)

    // 同步到文件索引表（仅公共目录上传）
	if (session.TargetType != models.TargetTypeTemp && session.TargetType != models.TargetTypePrivate) && s.indexSvc != nil {
        if err := s.indexSvc.SyncFile(session.TargetRoot, relativePath); err != nil {
            utils.Warn("上传后同步索引失败",
                utils.String("root", session.TargetRoot),
                utils.String("path", relativePath),
                utils.Err(err))
        } else {
            // 标记为最近已同步，防止 watcher 重复处理
            s.indexSvc.MarkRecentlySynced(session.TargetRoot, relativePath)
        }
        // 记录公开文件的上传者IP（覆盖上传会重新绑定归属）
        if session.ClientIP != "" {
            if uerr := s.db.Model(&models.FileRecordPublic{}).
                Where("root_name = ? AND file_path = ?", session.TargetRoot, idxRelPath).
                UpdateColumn("uploader_ip", session.ClientIP).Error; uerr != nil {
                utils.Warn("写入上传者IP失败",
                    utils.String("root", session.TargetRoot),
                    utils.String("path", idxRelPath),
                    utils.Err(uerr))
            }
        }
        // 触发哈希计算（后台执行，不阻塞）
        s.indexSvc.TriggerHash(context.Background())
    }

    return &FinalizeResult{
        TargetPath: relativePath,
        FileName:   session.FileName,
    }, nil
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

    session.ExpiredAt = time.Now().Add(24 * time.Hour)
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
    s.mu.Lock()
    defer s.mu.Unlock()

    session, err := s.sessionRepo.GetByID(uploadID)
    if err != nil {
        return fmt.Errorf("会话不存在: %w", err)
    }

    _, uploadingPath, err := s.resolvePaths(session)
    if err == nil {
        if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
            utils.Warn("取消上传删除文件失败", utils.String("path", uploadingPath), utils.Err(rmErr))
        }
    }

    _ = s.chunkRepo.DeleteBySessionID(session.ID)
    _ = s.sessionRepo.Delete(session.ID)

    return nil
}

// ListByUser 根据用户ID和存储类型查询上传会话
func (s *UploadSessionService) ListByUser(userID string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
    var sessions []models.UploadSession
    query := s.db.Model(&models.UploadSession{}).
        Where("target_type = ?", targetType).
        Where("user_id = ?", userID)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
    return sessions, total, err
}

// ListTempByUser 根据用户IP查询临时上传会话
func (s *UploadSessionService) ListTempByUser(clientIP string, page, pageSize int) ([]map[string]interface{}, int64, error) {
    type TempSession struct {
        ID        uint
        Filename  string
        FileSize  int64
        Status    string
        CreatedAt time.Time
        ExpiredAt time.Time
    }

    var sessions []TempSession
    var total int64

    tableName := "temp_upload_sessions"
    countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE client_ip = ?", tableName)
    if err := s.db.Raw(countQuery, clientIP).Scan(&total).Error; err != nil {
        return nil, 0, err
    }

    dataQuery := fmt.Sprintf("SELECT id, filename, file_size, 'in_progress' as status, created_at, expired_at FROM %s WHERE client_ip = ? ORDER BY created_at DESC LIMIT ? OFFSET ?", tableName)
    if err := s.db.Raw(dataQuery, clientIP, pageSize, (page-1)*pageSize).Scan(&sessions).Error; err != nil {
        return nil, 0, err
    }

    result := make([]map[string]interface{}, len(sessions))
    for i, s := range sessions {
        result[i] = map[string]interface{}{
            "id":        s.ID,
            "fileName":  s.Filename,
            "fileSize":  s.FileSize,
            "status":    s.Status,
            "createdAt": s.CreatedAt,
            "expiredAt": s.ExpiredAt,
        }
    }
    return result, total, nil
}

// CleanupExpired 清理过期会话
func (s *UploadSessionService) CleanupExpired() error {
    var sessions []models.UploadSession
    now := time.Now()

    if err := s.db.Where("`status` IN ? AND expired_at < ?",
        []models.UploadStatus{
            models.UploadStatusPending,
            models.UploadStatusInProgress,
            models.UploadStatusFailed,
        }, now).Find(&sessions).Error; err != nil {
        return fmt.Errorf("查询过期会话失败: %w", err)
    }

    // 清理已完成的旧会话（保留1小时）
    var completedSessions []models.UploadSession
    completedThreshold := now.Add(-1 * time.Hour)
    if err := s.db.Where("`status` = ? AND updated_at < ?",
        models.UploadStatusCompleted, completedThreshold).Find(&completedSessions).Error; err != nil {
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
        _, uploadingPath, err := s.resolvePaths(&session)
        if err == nil {
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

    return nil
}

// resolvePaths 构建并验证目标文件路径和上传中文件路径（不创建目录）
func (s *UploadSessionService) resolvePaths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
    rootPath := appconfig.RootNames[session.TargetRoot]
    if rootPath == "" {
        err = fmt.Errorf("无效的目标根目录")
        return
    }

    cleanDir := session.TargetPath
    if cleanDir == "" || cleanDir == "/" {
        cleanDir = ""
    } else {
        cleanDir = filepath.Clean(cleanDir)
    }
    if filepath.IsAbs(cleanDir) || cleanDir == ".." || (len(cleanDir) > 256 && cleanDir != "") {
        err = fmt.Errorf("无效的目标路径")
        return
    }

    targetDir := filepath.Join(rootPath, cleanDir)
    absTargetDir, absErr := filepath.Abs(targetDir)
    if absErr != nil {
        err = fmt.Errorf("路径解析失败")
        return
    }
    absRootPath, absErr := filepath.Abs(rootPath)
    if absErr != nil {
        err = fmt.Errorf("根路径解析失败")
        return
    }
    if absTargetDir != absRootPath && !strings.HasPrefix(absTargetDir, absRootPath+string(os.PathSeparator)) {
        err = fmt.Errorf("路径越界")
        return
    }

    cleanFilename := filepath.Clean(session.FileName)
    if strings.Contains(cleanFilename, "..") || strings.Contains(cleanFilename, "/") || strings.Contains(cleanFilename, "\\") {
        err = fmt.Errorf("无效的文件名")
        return
    }

    targetPath = filepath.Join(targetDir, cleanFilename)
    uploadingPath = filepath.Join(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".uploading")

    absTarget, _ := filepath.Abs(targetPath)
    if !strings.HasPrefix(absTarget, absRootPath+string(os.PathSeparator)) {
        err = fmt.Errorf("路径越界")
        return
    }

    return
}

// buildTargetPath 构建目标文件路径、上传中文件路径并创建目标目录
func (s *UploadSessionService) buildTargetPath(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
    targetPath, uploadingPath, err = s.resolvePaths(session)
    if err != nil {
        return
    }

    targetDir := filepath.Dir(targetPath)
    if err = os.MkdirAll(targetDir, 0755); err != nil {
        err = fmt.Errorf("创建目标目录失败: %w", err)
        return
    }

    return
}

// classifyUploadFileError 将打开上传文件时的系统错误归类为可读提示
func classifyUploadFileError(err error) string {
    if err == nil {
        return ""
    }
    msg := err.Error()
    l := strings.ToLower(msg)
    switch {
    case strings.Contains(l, "virus") || strings.Contains(l, "potentially unwanted") ||
        strings.Contains(l, "malware") || strings.Contains(l, "threat"):
        return "文件被安全软件拦截（疑似病毒或潜在有害软件），请将该目录加入安全软件信任/排除列表后重试"
    case strings.Contains(l, "being used by another process") || strings.Contains(l, "sharing violation") ||
        strings.Contains(l, "0x80070020"):
        return "文件正被其他进程占用，请关闭相关程序后重试"
    case strings.Contains(l, "access is denied") || strings.Contains(l, "0x80070005") ||
        strings.Contains(l, "permission denied"):
        return "无权限写入该目录，请检查目录权限"
    case strings.Contains(l, "disk full") || strings.Contains(l, "no space") || strings.Contains(l, "0x80070070"):
        return "磁盘空间不足，请清理后重试"
    default:
        return msg
    }
}

// ensureUploadingFile 创建上传文件并预分配空间
func (s *UploadSessionService) ensureUploadingFile(path string, size int64) error {
    file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
    if err != nil {
        if os.IsExist(err) {
            return nil
        }
        utils.Warn("创建上传文件失败", utils.String("path", path), utils.Err(err))
        return fmt.Errorf("无法创建上传文件")
    }
    defer file.Close()

    if err := preAllocate(file, size); err != nil {
        os.Remove(path)
        return err
    }

    return nil
}

// zeroOutChunk 清空已写入的分片数据，用于错误回滚
func (s *UploadSessionService) zeroOutChunk(f *os.File, offset int64, size int64) {
    ZeroOutChunk(f, offset, size)
}

// checkDiskSpace 检查磁盘剩余空间是否足够
func (s *UploadSessionService) checkDiskSpace(required int64, rootName string) error {
    checkPath := ""
    if rootName != "" {
        if rootPath, ok := appconfig.RootNames[rootName]; ok {
            checkPath = rootPath
        }
    }
    if checkPath == "" {
        // 无根目录时跳过磁盘空间检查
        return nil
    }
    return CheckDiskSpace(checkPath, required)
}


