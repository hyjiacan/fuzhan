package file

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PrivateUploadHandler 私有存储上传处理器。
// 只负责 HTTP 层与授权/配额校验，分片上传的编排统一委托
// 携带私有落盘与收尾策略的 UploadSessionService。
type PrivateUploadHandler struct {
	service     *services.UploadSessionService
	db          *gorm.DB
	privatePath string
}

// NewPrivateUploadHandler 创建私有存储上传处理器。
func NewPrivateUploadHandler(db *gorm.DB, chunkSize int64, privateStoragePath string, indexSvc *index.Service) *PrivateUploadHandler {
	return &PrivateUploadHandler{
		service: services.NewUploadSessionServiceWithPolicies(
			db, chunkSize, indexSvc,
			NewPrivateStorage(privateStoragePath),
			NewPrivateFinalizer(db, indexSvc),
		),
		db:          db,
		privatePath: privateStoragePath,
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

func (h *PrivateUploadHandler) getSessionRepo() *repositories.SessionRepository {
	return repositories.NewSessionRepository(h.db)
}

// getAndVerifySession 解析会话ID并校验归属（只能操作本人会话）
func (h *PrivateUploadHandler) getAndVerifySession(c *gin.Context, userID string) (*models.UploadSession, error) {
	var uploadIDStr string
	switch {
	case c.Param("id") != "":
		uploadIDStr = c.Param("id")
	case c.PostForm("uploadId") != "":
		uploadIDStr = c.PostForm("uploadId")
	default:
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

	if err := services.CheckDiskSpace(h.privatePath, req.FileSize); err != nil {
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	quota := appconfig.GlobalConfig.Storage.Private.PerUserQuota
	if quota > 0 {
		used, err := h.getUserUsedSpace(userID)
		if err == nil && used+req.FileSize > quota {
			utils.HandleBadRequest(c, fmt.Sprintf("超出用户配额限制（已用：%d 字节，配额：%d 字节）", used, quota), nil)
			return
		}
	}

	if req.Dir != "" {
		cleanDir := filepath.Clean(filepath.FromSlash(req.Dir))
		if strings.HasPrefix(cleanDir, "..") || strings.HasPrefix(cleanDir, "/") || strings.HasPrefix(cleanDir, "\\") || filepath.IsAbs(cleanDir) {
			utils.HandleBadRequest(c, "不允许的路径", nil)
			return
		}
	}

	cs, err := h.service.CreateSession(&services.CreateSessionReq{
		Filename:   req.Filename,
		FileSize:   req.FileSize,
		Dir:        req.Dir,
		RootName:   userID,
		TargetType: models.TargetTypePrivate,
		UserID:     userID,
		ClientIP:   utils.GetRealIP(c.Request),
	})
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusCreated, "会话创建成功", gin.H{
		"uploadId":    cs.ID,
		"chunkSize":   cs.ChunkSize,
		"totalChunks": cs.TotalChunks,
		"expiredAt":   cs.ExpiredAt,
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

	status, err := h.service.GetStatus(session.ID)
	if err != nil {
		utils.HandleNotFound(c, err.Error())
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{"session": status})
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

	status, err := h.service.Resume(session.ID)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "会话已恢复", gin.H{
		"uploadId":        status.ID,
		"uploadedIndexes": status.UploadedIndexes,
		"expiredAt":       status.ExpiredAt,
	})
}

// CancelSession 取消私有会话
func (h *PrivateUploadHandler) CancelSession(c *gin.Context) {
	userID, err := h.getUserUUID(c)
	if err != nil {
		utils.HandleUnauthorized(c, err.Error())
		return
	}
	if _, err := h.getAndVerifySession(c, userID); err != nil {
		utils.HandleNotFound(c, err.Error())
		return
	}

	uploadID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.service.Cancel(uint(uploadID)); err != nil {
		utils.HandleNotFound(c, err.Error())
		return
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
	// 校验会话归属（只能上传本人会话）
	if _, err := h.getAndVerifySession(c, userID); err != nil {
		utils.HandleNotFound(c, "会话不存在")
		return
	}

	file, err := c.FormFile("chunk")
	if err != nil {
		utils.HandleBadRequest(c, "未找到分片文件", nil)
		return
	}
	srcFile, err := file.Open()
	if err != nil {
		utils.HandleInternalServerError(c, "打开分片文件失败")
		return
	}

	if err := h.service.UploadChunk(&services.UploadChunkReq{
		UploadID:   uint(uploadID),
		ChunkIndex: int(mustAtoi(chunkIndexStr)),
		ChunkData:  srcFile,
		ChunkSize:  file.Size,
		Checksum:   checksum,
	}); err != nil {
		utils.HandleErrorCompat(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "分片上传成功", gin.H{"chunkIndex": chunkIndexStr})
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

	result, err := h.service.Finalize(session.ID, false)
	if err != nil {
		if errors.Is(err, services.ErrFileExists) {
			utils.HandleErrorCompat(c, http.StatusConflict, "同名文件已存在，请使用其他名称", nil)
			return
		}
		utils.HandleErrorCompat(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "上传完成", gin.H{
		"code":     result.ShareCode,
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

	used, err := h.getUserUsedSpace(userID)
	if err != nil {
		utils.HandleInternalServerError(c, "获取空间使用情况失败")
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"used":  used,
		"quota": appconfig.GlobalConfig.Storage.Private.PerUserQuota,
	})
}

// getUserUsedSpace 获取用户已使用的空间（从 file_records_private 查询）
func (h *PrivateUploadHandler) getUserUsedSpace(userID string) (int64, error) {
	var total int64
	err := h.db.Table("file_records_private").
		Where("owner_id = ? AND status = ? AND is_dir = ?",
			userID, models.FileStatusActive, false).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&total).Error
	return total, err
}

// mustAtoi 宽容解析整数（失败返回 -1，由 service 侧再次校验分片索引范围）
func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}
