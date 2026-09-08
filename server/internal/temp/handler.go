package temp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zeebo/xxh3"
	"gorm.io/gorm"

	configPkg "fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/uploadutil"
	"fuzhan/internal/utils"
)

// TempUploadSession 临时文件上传会话
type TempUploadSession struct {
	ID               uint      `gorm:"primaryKey"`
	UploadID         string    `gorm:"uniqueIndex;size:36;not null"`
	Code             string    `gorm:"size:8;not null"`
	Filename         string    `gorm:"size:255;not null"`
	FileSize         int64     `gorm:"not null"`
	FilePath         string    `gorm:"size:512"`
	ClientIP         string    `gorm:"size:45;index"`
	TotalChunks      int       `gorm:"not null"`
	UploadedSize     int64     `gorm:"default:0"`
	ExpiredAt        time.Time `gorm:"index"`
	Dir              string    `gorm:"size:512"`
	DeleteOnDownload bool      `gorm:"default:false"`
	CreatedAt        time.Time
}

// ChunkUploadRecord 分片上传记录
type ChunkUploadRecord struct {
	ID         uint   `gorm:"primaryKey"`
	SessionID  uint   `gorm:"index;not null"`
	ChunkIndex int    `gorm:"not null"`
	Checksum   string `gorm:"size:16"`
	CreatedAt  time.Time
}

// Handler 临时文件处理器
type Handler struct {
	db          *gorm.DB
	config      configPkg.TempConfig
	tempService *services.TempFileService
	indexSvc    *index.Service
}

// NewHandler 创建临时文件处理器
func NewHandler(db *gorm.DB, config configPkg.TempConfig, tempService *services.TempFileService, indexSvc *index.Service) *Handler {
	return &Handler{db: db, config: config, tempService: tempService, indexSvc: indexSvc}
}

// getClientIP 获取客户端IP
func (h *Handler) getClientIP(c *gin.Context) string {
	return utils.GetClientIP(c)
}

// isPathWithinRoot 判断 target 是否在 root 目录范围内（按边界校验，而非字符串前缀）。
// 用于临时文件/分片上传目录的越权防护，避免前缀相同但实际越界（如 /data/temp 与 /data/temp_other）的情况。
func isPathWithinRoot(root, target string) bool {
	absRoot, err1 := filepath.Abs(root)
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hexCodePattern 8位十六进制访问码正则
var hexCodePattern = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

// isValidHexCode 检查是否为有效的8位十六进制访问码
func isValidHexCode(code string) bool {
	return hexCodePattern.MatchString(code)
}

// validateCode 校验分享码格式
func validateCode(code string) bool {
	return code != "" && len(code) == 8 && isValidHexCode(code)
}

// ListHandler 处理临时文件列表
func (h *Handler) ListHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	downloadCode := c.Query("downloadCode")
	if downloadCode != "" && !validateCode(downloadCode) {
		utils.HandleBadRequest(c, "无效的下载码格式", nil)
		return
	}

	ip := h.getClientIP(c)
	dirPath := c.Query("path")
	// URL 解码，处理中文和特殊字符
	dirPath, _ = url.QueryUnescape(dirPath)

	result, err := h.tempService.List(ip, downloadCode, dirPath)
	if err != nil {
		utils.HandleInternalServerError(c, "获取文件列表失败")
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", models.TempListResponse{
		Files:   result.Files,
		Subdirs: result.Subdirs,
		Quota:   result.Quota,
	})
}

// QuotaHandler 处理配额查询
func (h *Handler) QuotaHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	ip := h.getClientIP(c)
	used, err := h.tempService.GetQuotaUsage(ip)
	if err != nil {
		utils.HandleInternalServerError(c, "获取配额信息失败")
		return
	}

	limit, _ := configPkg.ParseQuotaString(h.config.Quota.PerIP)
	utils.HandleSuccess(c, http.StatusOK, "", models.TempQuotaInfo{
		Used:  used,
		Limit: limit,
	})
}

// ClientIPHandler 处理获取客户端IP
func (h *Handler) ClientIPHandler(c *gin.Context) {
	ip := h.getClientIP(c)
	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"ip": ip,
	})
}

// UploadHandler 处理临时文件上传
func (h *Handler) UploadHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	file, handler, err := c.Request.FormFile("file")
	if err != nil {
		utils.HandleBadRequest(c, "获取文件失败", nil)
		return
	}
	defer file.Close()

	ip := h.getClientIP(c)
	uploadDir := c.PostForm("dir")
	// 清洗上传目录：去除路径遍历和空字节
	uploadDir = strings.TrimSpace(uploadDir)
	uploadDir = strings.ReplaceAll(uploadDir, "..", "")
	uploadDir = strings.ReplaceAll(uploadDir, "\x00", "")

	deleteOnDownload := c.PostForm("deleteOnDownload") == "true"

	result, err := h.tempService.Upload(file, handler.Filename, ip, uploadDir, handler.Size, deleteOnDownload)
	if err != nil {
		utils.HandleInternalServerError(c, err.Error())
		return
	}

	// 触发哈希计算（后台执行，不阻塞）
	if h.indexSvc != nil {
		h.indexSvc.TriggerHash(context.Background())
	}

	middleware.LogOperation(c, "temp.upload", handler.Filename, nil)
	utils.HandleSuccess(c, http.StatusCreated, "", gin.H{
		"code":        result.Code,
		"filename":    result.Filename,
		"fileSize":    result.FileSize,
		"expiredAt":   result.ExpiredAt,
		"downloadUrl": result.DownloadURL,
	})
}

// InfoHandler 处理获取文件信息
func (h *Handler) InfoHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	code := c.Param("code")
	if !validateCode(code) {
		utils.HandleBadRequest(c, "无效的访问码格式", nil)
		return
	}

	tempFile, err := h.tempService.GetByCode(code)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleNotFound(c, "文件不存在")
		} else {
			utils.HandleInternalServerError(c, "获取文件信息失败")
		}
		return
	}

	if utils.Now().After(tempFile.ExpiredAt) {
		utils.HandleErrorCompat(c, http.StatusGone, "文件已过期", nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", tempFile.ToResponse())
}

// DownloadHandler 处理文件下载
func (h *Handler) DownloadHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	code := c.Param("code")
	if !validateCode(code) {
		utils.HandleBadRequest(c, "无效的访问码格式", nil)
		return
	}

	tempFile, err := h.tempService.DownloadFile(code)
	if err != nil {
		if strings.Contains(err.Error(), "已过期") {
			utils.HandleErrorCompat(c, http.StatusGone, err.Error(), nil)
		} else if strings.Contains(err.Error(), "已被下载") {
			utils.HandleErrorCompat(c, http.StatusGone, err.Error(), nil)
		} else if strings.Contains(err.Error(), "不存在") {
			utils.HandleNotFound(c, err.Error())
		} else {
			utils.HandleInternalServerError(c, err.Error())
		}
		return
	}

	// 打开文件
	file, err := os.Open(tempFile.FilePath)
	if err != nil {
		utils.HandleInternalServerError(c, "无法打开文件")
		return
	}
	defer file.Close()

	// 设置下载响应头 - RFC 5987安全编码
	safeFilename := url.PathEscape(tempFile.Filename)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, tempFile.Filename, safeFilename))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", tempFile.FileSize))

	// 发送文件
	written, err := io.Copy(c.Writer, file)
	if err != nil {
		middleware.LogOperation(c, "temp.download", tempFile.Filename, err)
		utils.Error("文件下载失败", utils.Err(err), utils.Int64("written", written))
	} else {
		middleware.LogOperation(c, "temp.download", tempFile.Filename, nil)
	}

	// 下载后处理
	h.tempService.MarkDownloaded(tempFile, h.config.DeleteOnDownload)
}

// DeleteHandler 处理删除文件
func (h *Handler) DeleteHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	code := c.Param("code")
	if !validateCode(code) {
		utils.HandleBadRequest(c, "无效的访问码格式", nil)
		return
	}

	ip := h.getClientIP(c)
	if err := h.tempService.Delete(code, ip); err != nil {
		utils.HandleNotFound(c, err.Error())
		return
	}

	middleware.LogOperation(c, "temp.delete", code, nil)
	utils.HandleSuccess(c, http.StatusOK, "", nil)
}

// CleanupExpiredFiles 清理过期文件（供定时任务调用）
func (h *Handler) CleanupExpiredFiles() int {
	if !h.config.Enabled {
		return 0
	}

	count, err := h.tempService.CleanupExpired()
	if err != nil {
		utils.Error("清理过期临时文件失败", utils.Err(err))
		return 0
	}

	if count > 0 {
		utils.Info("已清理过期临时文件", utils.Int("count", count))
	}
	return count
}

// CleanupTempSessions 清理过期的临时上传会话
func (h *Handler) CleanupTempSessions() int {
	if !h.config.Enabled {
		return 0
	}

	// 查找过期的会话
	var sessions []TempUploadSession
	if err := h.db.Where("expired_at < ?", utils.Now()).Find(&sessions).Error; err != nil {
		utils.Error("查询过期临时上传会话失败", utils.Err(err))
		return 0
	}

	for _, session := range sessions {
		// 删除临时上传文件
		uploadingPath := filepath.Join(h.config.Path, fmt.Sprintf("%s.%d.uploading", session.UploadID, session.ID))
		os.Remove(uploadingPath)
		// 删除会话记录和分片记录
		if err := h.db.Where("session_id = ?", session.ID).Delete(&ChunkUploadRecord{}).Error; err != nil {
			utils.Warn("删除过期临时上传分片记录失败", utils.Int64("session_id", int64(session.ID)), utils.Err(err))
		}
		if err := h.db.Delete(&session).Error; err != nil {
			utils.Warn("删除过期临时上传会话失败", utils.Int64("session_id", int64(session.ID)), utils.Err(err))
		}
	}

	if len(sessions) > 0 {
		utils.Info("已清理过期临时上传会话", utils.Int("count", len(sessions)))
	}
	return len(sessions)
}

// ========== 分片上传相关方法 ==========

// InitTempSessionTable 初始化临时上传会话表
func (h *Handler) InitTempSessionTable() error {
	return h.db.AutoMigrate(&TempUploadSession{}, &ChunkUploadRecord{})
}

// CreateTempSessionHandler 处理创建临时文件上传会话
func (h *Handler) CreateTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	var req struct {
		Filename         string `json:"filename" binding:"required"`
		FileSize         int64  `json:"fileSize" binding:"required"`
		Dir              string `json:"dir"`
		DeleteOnDownload bool   `json:"deleteOnDownload"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "参数错误", err)
		return
	}

	ip := h.getClientIP(c)

	// 检查磁盘空间
	if err := services.CheckDiskSpace(h.config.Path, req.FileSize); err != nil {
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	// 检查配额
	used, _ := h.tempService.GetQuotaUsage(ip)
	quotaPerIP, _ := configPkg.ParseQuotaString(h.config.Quota.PerIP)
	if quotaPerIP > 0 {
		if used+req.FileSize > quotaPerIP {
			utils.HandleBadRequest(c, fmt.Sprintf("超出IP配额限制（已用：%d 字节，配额：%d 字节）",
				used, quotaPerIP), nil)
			return
		}
	}

	// 生成访问码
	code, err := utils.GenerateAccessCode()
	if err != nil {
		utils.HandleInternalServerError(c, "生成分享码失败")
		return
	}

	// 生成上传ID
	uploadIDBytes := make([]byte, 16)
	if _, err := rand.Read(uploadIDBytes); err != nil {
		utils.Error("生成上传ID失败", utils.Err(err))
		utils.HandleInternalServerError(c, "生成上传ID失败")
		return
	}
	uploadID := hex.EncodeToString(uploadIDBytes)

	// 计算总分片数，使用全局上传配置的分片大小
	chunkSize := configPkg.GlobalConfig.Upload.ChunkSize
	totalChunks := int((req.FileSize + chunkSize - 1) / chunkSize)

	// 验证 Dir 路径安全性
	if req.Dir != "" {
		cleanDir := filepath.Clean(req.Dir)
		target := filepath.Join(h.config.Path, cleanDir)
		if !isPathWithinRoot(h.config.Path, target) {
			utils.HandleBadRequest(c, "不允许的路径", nil)
			return
		}
	}
	// 计算过期时间
	expireDays := h.config.DefaultExpireDays
	expiredAt := utils.Now().Add(time.Duration(expireDays) * 24 * time.Hour)

	// 创建会话记录
	session := &TempUploadSession{
		UploadID:         uploadID,
		Code:             code,
		Filename:         req.Filename,
		FileSize:         req.FileSize,
		ClientIP:         ip,
		TotalChunks:      totalChunks,
		Dir:              req.Dir,
		DeleteOnDownload: req.DeleteOnDownload,
		ExpiredAt:        expiredAt,
	}

	if err := h.db.Create(session).Error; err != nil {
		utils.HandleInternalServerError(c, "创建上传会话失败")
		return
	}

	// 确保目录存在
	if err := os.MkdirAll(h.config.Path, 0755); err != nil {
		h.db.Delete(session)
		utils.HandleInternalServerError(c, "创建存储目录失败")
		return
	}

	utils.HandleSuccess(c, http.StatusCreated, "", gin.H{
		"uploadId":    uploadID,
		"code":        code,
		"totalChunks": totalChunks,
		"chunkSize":   chunkSize,
		"expiredAt":   expiredAt,
	})
}

// GetTempSessionHandler 处理获取临时文件上传会话状态
func (h *Handler) GetTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.Param("uploadId")
	if uploadID == "" {
		utils.HandleBadRequest(c, "缺少上传ID", nil)
		return
	}

	var session TempUploadSession
	if err := h.db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleNotFound(c, "上传会话不存在")
		} else {
			utils.HandleInternalServerError(c, "获取上传会话失败")
		}
		return
	}

	// 获取已上传的分片
	var chunks []ChunkUploadRecord
	h.db.Where("session_id = ?", session.ID).Find(&chunks)

	uploadedIndexes := make([]int, len(chunks))
	for i, chunk := range chunks {
		uploadedIndexes[i] = chunk.ChunkIndex
	}

	// 计算已上传大小
	chunkSize := configPkg.GlobalConfig.Upload.ChunkSize
	uploadedSize := int64(len(chunks)) * chunkSize
	if uploadedSize > session.FileSize {
		uploadedSize = session.FileSize
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"session": gin.H{
			"uploadId":        session.UploadID,
			"code":            session.Code,
			"filename":        session.Filename,
			"fileSize":        session.FileSize,
			"totalChunks":     session.TotalChunks,
			"chunkSize":       chunkSize,
			"uploadedSize":    uploadedSize,
			"uploadedIndexes": uploadedIndexes,
			"expiredAt":       session.ExpiredAt,
		},
	})
}

// UploadTempChunkHandler 处理上传临时文件分片
func (h *Handler) UploadTempChunkHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.PostForm("uploadId")
	chunkIndexStr := c.PostForm("chunkIndex")
	checksum := c.PostForm("checksum")

	if uploadID == "" || chunkIndexStr == "" {
		utils.HandleBadRequest(c, "缺少必要参数", nil)
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		utils.HandleBadRequest(c, "无效的分片索引", nil)
		return
	}

	// 获取会话
	var session TempUploadSession
	if err := h.db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleNotFound(c, "上传会话不存在")
		} else {
			utils.HandleInternalServerError(c, "获取上传会话失败")
		}
		return
	}

	// 检查分片索引
	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		utils.HandleBadRequest(c, "分片索引无效", nil)
		return
	}

	// 检查是否已上传
	var existing ChunkUploadRecord
	if err := h.db.Where("session_id = ? AND chunk_index = ?", session.ID, chunkIndex).First(&existing).Error; err == nil {
		// 已上传，直接返回成功
		utils.HandleSuccess(c, http.StatusOK, "", gin.H{"chunkIndex": chunkIndex, "checksum": checksum})
		return
	}

	// 获取分片文件
	header, err := c.FormFile("chunk")
	if err != nil {
		utils.HandleBadRequest(c, "获取分片文件失败", nil)
		return
	}

	file, err := header.Open()
	if err != nil {
		utils.HandleBadRequest(c, "打开分片文件失败", nil)
		return
	}
	defer file.Close()

	// 上传文件路径（扁平结构，无 chunks 子目录）
	uploadingPath := filepath.Join(h.config.Path, fmt.Sprintf("%s.%d.uploading", uploadID, session.ID))

	// 使用共享函数写入分片数据
	chunkSize, err := uploadutil.WriteChunkToUploading(
		uploadingPath, session.FileSize,
		chunkIndex, configPkg.GlobalConfig.Upload.ChunkSize, session.TotalChunks,
		header.Size, file, checksum,
	)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	// 记录分片
	record := &ChunkUploadRecord{
		SessionID:  session.ID,
		ChunkIndex: chunkIndex,
		Checksum:   checksum,
	}
	if err := h.db.Create(record).Error; err != nil {
		utils.Error("创建分片记录失败", utils.Err(err))
		// 回滚已写入的分片数据
		zeroOffset := int64(chunkIndex) * configPkg.GlobalConfig.Upload.ChunkSize
		if f, reopenErr := os.OpenFile(uploadingPath, os.O_RDWR, 0644); reopenErr == nil {
			services.ZeroOutChunk(f, zeroOffset, header.Size)
			f.Close()
		}
		utils.HandleInternalServerError(c, "保存分片记录失败")
		return
	}

	// 原子递增已上传大小（分片可能并发上传，读改写会丢更新）
	if err := h.db.Model(&TempUploadSession{}).
		Where("id = ?", session.ID).
		UpdateColumn("uploaded_size", gorm.Expr("uploaded_size + ?", chunkSize)).Error; err != nil {
		utils.Warn("更新上传大小失败", utils.Err(err))
	}
	session.UploadedSize += chunkSize

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"chunkIndex":   chunkIndex,
		"chunkSize":    chunkSize,
		"checksum":     checksum,
		"uploadedSize": session.UploadedSize,
	})
}

// ResumeTempSessionHandler 处理恢复临时文件上传会话
func (h *Handler) ResumeTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.Param("uploadId")
	if uploadID == "" {
		utils.HandleBadRequest(c, "缺少上传ID", nil)
		return
	}

	var session TempUploadSession
	if err := h.db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleNotFound(c, "上传会话不存在")
		} else {
			utils.HandleInternalServerError(c, "获取上传会话失败")
		}
		return
	}

	// 重置过期时间
	expireDays := h.config.DefaultExpireDays
	session.ExpiredAt = utils.Now().Add(time.Duration(expireDays) * 24 * time.Hour)
	if err := h.db.Model(&session).Update("expired_at", session.ExpiredAt).Error; err != nil {
		utils.Warn("更新会话过期时间失败", utils.Err(err))
	}

	// 获取已上传的分片索引
	var chunks []ChunkUploadRecord
	h.db.Where("session_id = ?", session.ID).Find(&chunks)
	uploadedIndexes := make([]int, len(chunks))
	for i, chunk := range chunks {
		uploadedIndexes[i] = chunk.ChunkIndex
	}

	chunkSize := configPkg.GlobalConfig.Upload.ChunkSize
	uploadedSize := int64(len(chunks)) * chunkSize
	if uploadedSize > session.FileSize {
		uploadedSize = session.FileSize
	}

	utils.HandleSuccess(c, http.StatusOK, "会话已恢复", gin.H{
		"uploadId":        session.UploadID,
		"uploadedIndexes": uploadedIndexes,
		"uploadedSize":    uploadedSize,
		"totalChunks":     session.TotalChunks,
		"chunkSize":       chunkSize,
		"expiredAt":       session.ExpiredAt,
	})
}

// CancelTempSessionHandler 处理取消临时文件上传会话
func (h *Handler) CancelTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.Param("uploadId")
	if uploadID == "" {
		utils.HandleBadRequest(c, "缺少上传ID", nil)
		return
	}

	var session TempUploadSession
	if err := h.db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleNotFound(c, "上传会话不存在")
		} else {
			utils.HandleInternalServerError(c, "获取上传会话失败")
		}
		return
	}

	// 删除临时上传文件
	uploadingPath := filepath.Join(h.config.Path, fmt.Sprintf("%s.%d.uploading", uploadID, session.ID))
	os.Remove(uploadingPath)

	// 删除分片记录和会话记录
	if err := h.db.Where("session_id = ?", session.ID).Delete(&ChunkUploadRecord{}).Error; err != nil {
		utils.Warn("删除分片记录失败", utils.Int64("session_id", int64(session.ID)), utils.Err(err))
	}
	if err := h.db.Delete(&session).Error; err != nil {
		utils.Warn("删除会话记录失败", utils.Int64("session_id", int64(session.ID)), utils.Err(err))
	}

	middleware.LogOperation(c, "temp.upload.session.cancel", uploadID, nil)
	utils.HandleSuccess(c, http.StatusOK, "会话已取消", nil)
}

// FinalizeTempUploadHandler 处理完成临时文件上传
func (h *Handler) FinalizeTempUploadHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.PostForm("uploadId")
	if uploadID == "" {
		// 前端 UploadManager 发送 JSON body，尝试解析
		var body struct {
			UploadID string `json:"uploadId"`
		}
		if err := c.ShouldBindJSON(&body); err == nil && body.UploadID != "" {
			uploadID = body.UploadID
		}
	}
	if uploadID == "" {
		utils.HandleBadRequest(c, "缺少上传ID", nil)
		return
	}

	// 获取会话
	var session TempUploadSession
	if err := h.db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleNotFound(c, "上传会话不存在")
		} else {
			utils.HandleInternalServerError(c, "获取上传会话失败")
		}
		return
	}

	// 检查所有分片是否已上传
	var chunkCount int64
	if err := h.db.Model(&ChunkUploadRecord{}).Where("session_id = ?", session.ID).Count(&chunkCount).Error; err != nil {
		utils.Error("查询分片数量失败", utils.Err(err))
		utils.HandleInternalServerError(c, "查询分片数量失败")
		return
	}

	if int(chunkCount) < session.TotalChunks {
		utils.HandleBadRequest(c, fmt.Sprintf("分片上传不完整（已上传 %d/%d）", chunkCount, session.TotalChunks), nil)
		return
	}

	// 重命名 .uploading 文件为最终文件
	hash := xxh3.Hash([]byte(session.Code + "fuzhan-secret"))
	safeFilename := fmt.Sprintf("%016x", hash)
	targetDir := h.config.Path
	if session.Dir != "" && session.Dir != "/" {
		targetDir = filepath.Join(targetDir, session.Dir)
		// 验证路径不越权
		if !isPathWithinRoot(h.config.Path, targetDir) {
			utils.HandleBadRequest(c, "不允许的路径", nil)
			return
		}
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			utils.Warn("创建目标目录失败", utils.String("dir", targetDir), utils.Err(err))
		}
	}
	filePath := filepath.Join(targetDir, safeFilename)

	uploadingPath := filepath.Join(h.config.Path, fmt.Sprintf("%s.%d.uploading", uploadID, session.ID))
	if err := os.Rename(uploadingPath, filePath); err != nil {
		var linkErr *os.LinkError
		if errors.As(err, &linkErr) {
			if copyErr := utils.CopyFile(uploadingPath, filePath); copyErr != nil {
				utils.HandleInternalServerError(c, "完成上传失败: "+copyErr.Error())
				return
			}
			if rmErr := os.Remove(uploadingPath); rmErr != nil {
				utils.Warn("跨设备复制后删除源文件失败", utils.String("path", uploadingPath), utils.Err(rmErr))
			}
		} else {
			utils.HandleInternalServerError(c, "完成上传失败: "+err.Error())
			return
		}
	}

	totalSize := session.FileSize

	// 创建临时文件记录
	tempFile := &models.TempFile{
		Code:             session.Code,
		Filename:         session.Filename,
		FileSize:         totalSize,
		FilePath:         filePath,
		ClientIP:         session.ClientIP,
		Dir:              session.Dir,
		DeleteOnDownload: session.DeleteOnDownload,
		ExpiredAt:        session.ExpiredAt,
	}

	if err := h.db.Create(tempFile).Error; err != nil {
		os.Remove(filePath)
		utils.HandleInternalServerError(c, "保存文件记录失败")
		return
	}

	// 同步到临时文件索引表
	now := utils.Now()
	tempRecord := models.FileRecordTemp{
		FileRecordBase: models.FileRecordBase{
			FileName:     session.Filename,
			FilePath:     session.Code,
			RootName:     "temp",
			FullPath:     "/temp/" + session.Code,
			FileSize:     totalSize,
			IsDir:        false,
			ModTime:      now,
			Status:       models.FileStatusActive,
			OwnerID:      session.ClientIP,
			LastSyncedAt: now,
		},
	}
	if err := h.db.Create(&tempRecord).Error; err != nil {
		utils.Warn("同步临时文件索引失败", utils.String("code", session.Code), utils.Err(err))
	}

	// 触发哈希计算（后台执行，不阻塞）
	if h.indexSvc != nil {
		h.indexSvc.TriggerHash(context.Background())
	}

	// 删除会话记录
	h.db.Delete(&session)
	h.db.Delete(&ChunkUploadRecord{}, "session_id = ?", session.ID)

	utils.HandleSuccess(c, http.StatusCreated, "", gin.H{
		"code":        session.Code,
		"filename":    session.Filename,
		"fileSize":    totalSize,
		"expiredAt":   session.ExpiredAt,
		"downloadUrl": fmt.Sprintf("/api/v1/temp/%s/download", session.Code),
	})
}
