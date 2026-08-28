package file

import (
    "context"
    "errors"
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/index"
    "fuzhan/internal/uploadutil"
    "fuzhan/internal/models"
    "fuzhan/internal/repositories"
    "fuzhan/internal/services"
    "fuzhan/internal/utils"
)

// PrivateUploadHandler 私有存储上传处理器
type PrivateUploadHandler struct {
    service     *services.UploadSessionService
    db          *gorm.DB
    chunkSize   int64
    privatePath string
    indexSvc    *index.Service
}

// NewPrivateUploadHandler 创建私有存储上传处理器
func NewPrivateUploadHandler(svc *services.UploadSessionService, db *gorm.DB, chunkSize int64, privateStoragePath string, indexSvc *index.Service) *PrivateUploadHandler {
    return &PrivateUploadHandler{
        service:     svc,
        db:          db,
        chunkSize:   chunkSize,
        privatePath: privateStoragePath,
        indexSvc:    indexSvc,
    }
}

func (h *PrivateUploadHandler) getUserUUID(c *gin.Context) (string, error) {
    uuidInterface, exists := c.Get("userUUID")
    if !exists {
        return "", fmt.Errorf("用户未认证")
    }
    uuid, ok := uuidInterface.(string)
    if !ok || uuid == "" {
        return "", fmt.Errorf("无效的用户UUID")
    }
    return uuid, nil
}

// getSessionRepo 创建一个新的 SessionRepository 实例
func (h *PrivateUploadHandler) getSessionRepo() *repositories.SessionRepository {
    return repositories.NewSessionRepository(h.db)
}

// getChunkRepo 创建一个新的 ChunkRepository 实例
func (h *PrivateUploadHandler) getChunkRepo() *repositories.ChunkRepository {
    return repositories.NewChunkRepository(h.db)
}

func (h *PrivateUploadHandler) getAndVerifySession(c *gin.Context, userID string) (*models.UploadSession, error) {
    var uploadIDStr string
    if c.Param("id") != "" {
        uploadIDStr = c.Param("id")
    } else if c.PostForm("uploadId") != "" {
        uploadIDStr = c.PostForm("uploadId")
    } else {
        // 前端 UploadManager 发送 JSON body，尝试解析
        var body struct {
            UploadID string `json:"uploadId"`
        }
        if err := c.ShouldBindJSON(&body); err == nil && body.UploadID != "" {
            uploadIDStr = body.UploadID
        }
    }
    if uploadIDStr == "" {
        return nil, fmt.Errorf("缺少会话ID")
    }

    uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
    if err != nil {
        return nil, fmt.Errorf("无效的会话ID")
    }

    session, err := h.getSessionRepo().GetByID(uint(uploadID))
    if err != nil {
        return nil, fmt.Errorf("会话不存在: %w", err)
    }
    if session.UserID != userID {
        return nil, fmt.Errorf("会话不存在: 用户不匹配")
    }
    return session, nil
}

// CreateSession 创建私有文件上传会话
func (h *PrivateUploadHandler) CreateSession(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, err.Error())
        return
    }

    var req struct {
        Filename string `json:"filename" binding:"required"`
        FileSize int64  `json:"fileSize" binding:"required,min=1"`
        Dir      string `json:"dir"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
        return
    }

    // 检查磁盘空间
    if err := services.CheckDiskSpace(h.privatePath, req.FileSize); err != nil {
        utils.HandleBadRequest(c, err.Error(), nil)
        return
    }

    // 检查用户配额
    quota := appconfig.GlobalConfig.Storage.Private.PerUserQuota
    if quota > 0 {
        used, err := h.getUserUsedSpace(userID)
        if err == nil && used+req.FileSize > quota {
            utils.HandleBadRequest(c, fmt.Sprintf("超出用户配额限制（已用：%d 字节，配额：%d 字节）", used, quota), nil)
            return
        }
    }

    // 验证 Dir 路径安全性

    if req.Dir != "" {
        cleanDir := filepath.Clean(req.Dir)
        if strings.HasPrefix(cleanDir, "..") || strings.HasPrefix(cleanDir, "/") || strings.HasPrefix(cleanDir, "\\") {
            utils.HandleBadRequest(c, "不允许的路径", nil)
            return
        }
    }


    totalChunks := int((req.FileSize - 1)/h.chunkSize + 1)
    // 防御: userID 为短字符串时避免切片越界 panic（userID 通常为 UUID，但仍需兜底）
    shortID := userID
    if len(shortID) > 8 {
        shortID = shortID[:8]
    }
    uploadID := fmt.Sprintf("pv_%s_%d", shortID, time.Now().UnixNano())

    session := &models.UploadSession{
        FileName:    req.Filename,
        FileSize:    req.FileSize,
        ChunkSize:   h.chunkSize,
        TotalChunks: totalChunks,
        Status:      models.UploadStatusPending,
        TargetType:  models.TargetTypePrivate,
        TargetPath:  req.Dir,
        TargetRoot:  userID,
        UserID:      userID,
        ChunkDir:    uploadID,
        ExpiredAt:   time.Now().Add(24 * time.Hour), // 上传分片会话有效期固定 24 小时
    }

    if err := h.getSessionRepo().Create(session); err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建会话失败", nil)
        return
    }

    utils.HandleSuccess(c, http.StatusCreated, "会话创建成功", gin.H{
        "uploadId":    session.ID,
        "chunkSize":   h.chunkSize,
        "totalChunks": totalChunks,
        "expiredAt":   session.ExpiredAt,
    })
}

// GetSession 获取私有会话状态
func (h *PrivateUploadHandler) GetSession(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, err.Error())
        return
    }

    session, err := h.getAndVerifySession(c, userID)
    if err != nil {
        utils.HandleNotFound(c, err.Error())
        return
    }

    uploadedIndexes, _ := h.getChunkRepo().GetUploadedIndexes(session.ID)
    expired := session.ExpiredAt.Before(time.Now()) && session.Status != models.UploadStatusCompleted

    utils.HandleSuccess(c, http.StatusOK, "", gin.H{
        "session": gin.H{
            "id":              session.ID,
            "fileName":        session.FileName,
            "fileSize":        session.FileSize,
            "status":          session.Status,
            "totalChunks":     session.TotalChunks,
            "uploadedIndexes": uploadedIndexes,
            "expired":         expired,
            "expiredAt":       session.ExpiredAt,
        },
    })
}

// ResumeSession 续传私有会话
func (h *PrivateUploadHandler) ResumeSession(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, err.Error())
        return
    }

    session, err := h.getAndVerifySession(c, userID)
    if err != nil {
        utils.HandleNotFound(c, err.Error())
        return
    }

    if session.Status == models.UploadStatusCompleted || session.Status == models.UploadStatusCancelled {
        utils.HandleBadRequest(c, "会话已完成或已取消", nil)
        return
    }

    session.ExpiredAt = time.Now().Add(24 * time.Hour)
    session.Status = models.UploadStatusInProgress
    if err := h.getSessionRepo().Update(session); err != nil {
        utils.Error("更新会话状态失败", utils.Err(err))
    }

    uploadedIndexes, _ := h.getChunkRepo().GetUploadedIndexes(session.ID)
    utils.HandleSuccess(c, http.StatusOK, "会话已恢复", gin.H{
        "uploadId":        session.ID,
        "uploadedIndexes": uploadedIndexes,
        "expiredAt":       session.ExpiredAt,
    })
}

// CancelSession 取消私有会话
func (h *PrivateUploadHandler) CancelSession(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, err.Error())
        return
    }

    session, err := h.getAndVerifySession(c, userID)
    if err != nil {
        utils.HandleNotFound(c, err.Error())
        return
    }

    // 删除临时上传文件
    uploadingPath := filepath.Join(h.privatePath, fmt.Sprintf("%s.%d.uploading", session.ChunkDir, session.ID))
    os.Remove(uploadingPath)

    if err := h.getChunkRepo().DeleteBySessionID(session.ID); err != nil {
        utils.Error("删除分片记录失败", utils.Err(err))
    }
    if err := h.getSessionRepo().Delete(session.ID); err != nil {
        utils.Error("删除会话记录失败", utils.Err(err))
    }

    utils.HandleSuccess(c, http.StatusOK, "会话已取消", nil)
}

// UploadChunk 上传私有分片
func (h *PrivateUploadHandler) UploadChunk(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, err.Error())
        return
    }

    uploadIDStr := c.PostForm("uploadId")
    chunkIndexStr := c.PostForm("chunkIndex")
    checksum := c.PostForm("checksum")

    uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
    if err != nil {
        utils.HandleBadRequest(c, "无效的会话ID", nil)
        return
    }

    session, err := h.getSessionRepo().GetByID(uint(uploadID))
    if err != nil || session.UserID != userID {
        utils.HandleNotFound(c, "会话不存在")
        return
    }

    if session.Status == models.UploadStatusCompleted || session.Status == models.UploadStatusCancelled {
        utils.HandleBadRequest(c, "会话状态不允许上传", nil)
        return
    }

    if session.ExpiredAt.Before(time.Now()) {
        h.getSessionRepo().UpdateStatus(session.ID, models.UploadStatusExpired)
        utils.HandleBadRequest(c, "会话已过期", nil)
        return
    }

    chunkIndex, err := strconv.Atoi(chunkIndexStr)
    if err != nil || chunkIndex < 0 || chunkIndex >= session.TotalChunks {
        utils.HandleBadRequest(c, "无效的分片索引", nil)
        return
    }

    file, err := c.FormFile("chunk")
    if err != nil {
        utils.HandleBadRequest(c, "未找到分片文件", nil)
        return
    }

    // 构建上传文件路径
    uploadingPath := filepath.Join(h.privatePath, fmt.Sprintf("%s.%d.uploading", session.ChunkDir, session.ID))

    // 打开上传的分片文件
    srcFile, err := file.Open()
    if err != nil {
        utils.HandleInternalServerError(c, "打开分片文件失败")
        return
    }
    defer srcFile.Close()

    // 使用共享函数写入分片数据
    written, err := uploadutil.WriteChunkToUploading(
        uploadingPath, session.FileSize,
        chunkIndex, h.chunkSize, session.TotalChunks,
        file.Size, srcFile, checksum,
    )
    if err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
        return
    }

    if session.Status == models.UploadStatusPending {
        session.Status = models.UploadStatusInProgress
        if err := h.getSessionRepo().Update(session); err != nil {
            utils.Error("更新会话状态失败", utils.Err(err))
        }
    }

    if err := h.getChunkRepo().Create(&models.UploadedChunk{
        SessionID:     session.ID,
        ChunkIndex:    chunkIndex,
        ChunkSize:     written,
        ChunkChecksum: checksum,
    }); err != nil {
        utils.Error("创建分片记录失败", utils.Err(err))
        // 回滚已写入的分片数据
        zeroOffset := int64(chunkIndex) * h.chunkSize
        if f, reopenErr := os.OpenFile(uploadingPath, os.O_RDWR, 0644); reopenErr == nil {
            services.ZeroOutChunk(f, zeroOffset, written)
            f.Close()
        }
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "保存分片记录失败", nil)
        return
    }

    utils.HandleSuccess(c, http.StatusOK, "分片上传成功", gin.H{"chunkIndex": chunkIndex})
}

// FinalizeSession 完成私有上传
func (h *PrivateUploadHandler) FinalizeSession(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, err.Error())
        return
    }

    session, err := h.getAndVerifySession(c, userID)
    if err != nil {
        utils.HandleNotFound(c, err.Error())
        return
    }

    if session.Status == models.UploadStatusCompleted {
        utils.HandleBadRequest(c, "会话已完成", nil)
        return
    }

    uploadedIndexes, _ := h.getChunkRepo().GetUploadedIndexes(session.ID)
    if len(uploadedIndexes) != session.TotalChunks {
        utils.HandleBadRequest(c, fmt.Sprintf("分片不完整: %d/%d", len(uploadedIndexes), session.TotalChunks), nil)
        return
    }

    shareCode, err := GenerateShareCode()
    if err != nil {
        utils.HandleInternalServerError(c, "生成分享码失败")
        return
    }

    userPath := GetPrivateUserPath(appconfig.GlobalConfig.Storage.Private.Path, userID)
    fileDir := userPath
    if session.TargetPath != "" && session.TargetPath != "/" {
        fileDir = filepath.Join(fileDir, session.TargetPath)
        // 验证路径不越权
        absTarget, _ := filepath.Abs(fileDir)
        absRoot, _ := filepath.Abs(userPath)
        if !strings.HasPrefix(absTarget, absRoot) {
            utils.HandleBadRequest(c, "不允许的路径", nil)
            return
        }
    }
    fileDir = filepath.Join(fileDir, shareCode)
    if err := os.MkdirAll(fileDir, 0755); err != nil {
        utils.Error("创建文件目录失败", utils.String("dir", fileDir), utils.Err(err))
    }

    finalPath := filepath.Join(fileDir, "data")

    // 重命名 .uploading 文件为最终文件
    uploadingPath := filepath.Join(h.privatePath, fmt.Sprintf("%s.%d.uploading", session.ChunkDir, session.ID))
    if err := os.Rename(uploadingPath, finalPath); err != nil {
        var linkErr *os.LinkError
        if errors.As(err, &linkErr) {
            if copyErr := utils.CopyFile(uploadingPath, finalPath); copyErr != nil {
                utils.HandleErrorCompat(c, http.StatusInternalServerError, "完成上传失败: "+copyErr.Error(), nil)
                return
            }
            if rmErr := os.Remove(uploadingPath); rmErr != nil {
                utils.Warn("跨设备复制后删除源文件失败", utils.String("path", uploadingPath), utils.Err(rmErr))
            }
        } else {
            utils.HandleErrorCompat(c, http.StatusInternalServerError, "完成上传失败: "+err.Error(), nil)
            return
        }
    }
    if err := h.getChunkRepo().DeleteBySessionID(session.ID); err != nil {
        utils.Error("删除分片记录失败", utils.Err(err))
    }

    meta := &PrivateFileMeta{
        Filename:   session.FileName,
        UploadTime: time.Now(),
        FileSize:   session.FileSize,
        Owner:      userID,
        Code:       shareCode,
    }
    SavePrivateFileMetadata(fileDir, meta)

    // 同步写入 file_records_private（最佳努力，失败不影响主流程）
    if err := h.db.Create(&models.FileRecordPrivate{
        FileRecordBase: models.FileRecordBase{
            FileName:     session.FileName,
            FilePath:     shareCode,
            RootName:     "private",
            FullPath:     "private/" + shareCode,
            FileSize:     session.FileSize,
            IsDir:        false,
            ModTime:      time.Now(),
            Status:       models.FileStatusActive,
            OwnerID:      userID,
            LastSyncedAt: time.Now(),
        },
    }).Error; err != nil {
        utils.Warn("同步私有文件索引失败", utils.String("file", session.FileName), utils.Err(err))
    }

    // 触发哈希计算（后台执行，不阻塞）
    if h.indexSvc != nil {
        h.indexSvc.TriggerHash(context.Background())
    }

    session.Status = models.UploadStatusCompleted
    if err := h.getSessionRepo().Update(session); err != nil {
        utils.Warn("更新会话状态失败", utils.Err(err))
    }
    if err := h.getSessionRepo().Delete(session.ID); err != nil {
        utils.Error("删除会话记录失败", utils.Err(err))
    }

    utils.HandleSuccess(c, http.StatusOK, "上传完成", gin.H{
        "code":     shareCode,
        "filename": session.FileName,
        "fileSize": session.FileSize,
    })
}

// QuotaHandler 处理私有存储配额查询
func (h *PrivateUploadHandler) QuotaHandler(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, "请先登录")
        return
    }

    // 获取用户配额配置（使用配置值）
    quota := appconfig.GlobalConfig.Storage.Private.PerUserQuota

    // 计算用户已使用空间
    used, err := h.getUserUsedSpace(userID)
    if err != nil {
        utils.HandleInternalServerError(c, "获取空间使用情况失败")
        return
    }

    utils.HandleSuccess(c, http.StatusOK, "", gin.H{
        "used":  used,
        "quota": quota,
    })
}

// getUserUsedSpace 获取用户已使用的空间
// 从 file_records_private 查询，索引未就绪则返回 0
func (h *PrivateUploadHandler) getUserUsedSpace(userID string) (int64, error) {
    var total int64
    err := h.db.Table("file_records_private").
        Where("owner_id = ? AND status = ? AND is_dir = ?",
            userID, "active", false).
        Select("COALESCE(SUM(file_size), 0)").
        Scan(&total).Error
    return total, err
}