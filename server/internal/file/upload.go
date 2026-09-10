package file

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UploadSessionHandler 上传会话处理器
type UploadSessionHandler struct {
	service     *services.UploadSessionService
	db          *gorm.DB
	urlTaskRepo *repositories.URLDownloadTaskRepository
	recordRepo  repositories.AuditStore
	indexSvc    *index.Service
	taskSvc     *services.TaskService
	chunkSize   int64
	downloadSem chan struct{}
}

const maxConcurrentDownloads = 5

// createSSRFProtectedHTTPClient 创建带 DNS 重绑定防护的 HTTP 客户端
// 在实际建立连接时再次验证 IP 安全性，防止 DNS 重绑定攻击
func createSSRFProtectedHTTPClient(timeout time.Duration, urlCfg utils.URLUploadConfig) *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip != nil {
				return utils.CheckIPSafe(ip, urlCfg)
			}
			// 是域名，解析后再验证所有 IP
			ips, lookupErr := net.LookupIP(host)
			if lookupErr != nil {
				return fmt.Errorf("SSRF 防护: DNS 解析失败 %s: %w", host, lookupErr)
			}
			for _, resolvedIP := range ips {
				if err := utils.CheckIPSafe(resolvedIP, urlCfg); err != nil {
					return fmt.Errorf("SSRF 防护: %s (%s) %w", host, resolvedIP.String(), err)
				}
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}
}

// NewUploadSessionHandler 创建上传会话处理器实例
func NewUploadSessionHandler(svc *services.UploadSessionService, db *gorm.DB, chunkSize int64, recordRepo repositories.AuditStore, indexSvc *index.Service, taskSvc *services.TaskService) *UploadSessionHandler {
	return &UploadSessionHandler{
		service:     svc,
		db:          db,
		urlTaskRepo: repositories.NewURLDownloadTaskRepository(db),
		recordRepo:  recordRepo,
		indexSvc:    indexSvc,
		taskSvc:     taskSvc,
		chunkSize:   chunkSize,
		downloadSem: make(chan struct{}, maxConcurrentDownloads),
	}
}

// CreateSessionRequest 创建会话请求
type CreateSessionRequest struct {
	Filename   string            `json:"filename" binding:"required,max=255"`
	FileSize   int64             `json:"fileSize" binding:"required,min=1"`
	Dir        string            `json:"dir" binding:"required,max=1024"`
	RootName   string            `json:"rootName" binding:"required,max=255"`
	TargetType models.TargetType `json:"targetType"`
}

// CreateSession 创建上传会话
func (h *UploadSessionHandler) CreateSession(c *gin.Context) {
	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}

	// 检查公共目录上传开关（非临时/私有文件）
	if (req.TargetType != models.TargetTypeTemp && req.TargetType != models.TargetTypePrivate) && !appconfig.GlobalConfig.Upload.Enabled {
		utils.HandleErrorCompat(c, http.StatusForbidden, "公共目录上传已关闭", nil)
		return
	}

	// 检查目标文件是否已存在（仅针对公开共享目录）
	targetExists := false
	if req.TargetType != models.TargetTypeTemp && req.TargetType != models.TargetTypePrivate {
		// 路径安全校验：防止 .. 穿越
		if strings.Contains(req.Dir, "..") {
			utils.HandleBadRequest(c, "路径不能包含 ..", nil)
			return
		}
		// 文件名清洗：去除路径分隔符和空字节
		req.Filename = strings.ReplaceAll(req.Filename, "/", "")
		req.Filename = strings.ReplaceAll(req.Filename, "\\", "")
		req.Filename = strings.ReplaceAll(req.Filename, "\x00", "")
		if rootDir, ok := appconfig.RootNames[req.RootName]; ok {
			cleanDir := filepath.Clean(req.Dir)
			if cleanDir == "." || cleanDir == "/" {
				cleanDir = ""
			}
			// 检查文件系统
			targetPath := filepath.Join(rootDir, cleanDir, req.Filename)
			if _, statErr := os.Stat(targetPath); statErr == nil {
				targetExists = true
			}
			// 同时检查索引表（扫描可能未及时完成，或索引表记录了文件系统尚未反映的文件）
			if !targetExists && h.indexSvc != nil {
				// 构建索引表中的 filePath（格式：/目录/文件名）
				indexFilePath := "/" + req.Filename
				if cleanDir != "" {
					indexFilePath = "/" + cleanDir + "/" + req.Filename
				}
				record, lookupErr := h.indexSvc.FindRecordByPath(req.Filename, req.RootName, indexFilePath)
				if lookupErr == nil && record != nil {
					targetExists = true
				}
			}
		}
	}

	if targetExists {
		role, _ := c.Get(string(constants.ContextKeyRole))
		if role != "admin" {
			// 非管理员：仅当上传者IP与现有文件的上传者IP一致时允许覆盖
			clientIP := utils.GetClientIP(c)
			indexFilePath := "/" + req.Filename
			if cleanDir := strings.Trim(strings.TrimSuffix(req.Dir, "/"), "/"); cleanDir != "" {
				indexFilePath = "/" + cleanDir + "/" + req.Filename
			}
			if clientIP == "" || h.indexSvc == nil {
				utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
				return
			}
			rec, lookupErr := h.indexSvc.FindRecordByPath(req.Filename, req.RootName, indexFilePath)
			if lookupErr != nil || rec == nil || rec.UploaderIP == "" || rec.UploaderIP != clientIP {
				utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
				return
			}
			// 上传者IP一致，允许覆盖
		}
		// 覆盖，通过 overwriteRequired 标记告知前端
	}

	session, err := h.service.CreateSession(&services.CreateSessionReq{
		Filename:   req.Filename,
		FileSize:   req.FileSize,
		Dir:        req.Dir,
		RootName:   req.RootName,
		TargetType: req.TargetType,
		UserID:     utils.GetClientIP(c),
		ClientIP:   utils.GetClientIP(c),
	})
	if err != nil {
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	middleware.LogOperation(c, "upload.session.create", req.Filename, nil)
	utils.HandleSuccess(c, http.StatusCreated, "会话创建成功", gin.H{
		"uploadId":          session.ID,
		"chunkSize":         h.chunkSize,
		"totalChunks":       session.TotalChunks,
		"expiredAt":         session.ExpiredAt,
		"overwriteRequired": targetExists,
	})
}

// GetSession 获取会话状态
func (h *UploadSessionHandler) GetSession(c *gin.Context) {
	uploadIDStr := c.Param("id")
	uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
	if err != nil {
		utils.HandleBadRequest(c, "无效的会话ID", nil)
		return
	}

	status, err := h.service.GetStatus(uint(uploadID))
	if err != nil {
		middleware.LogOperation(c, "upload.session.cancel", fmt.Sprintf("session %d", uploadID), fmt.Errorf("会话不存在"))
		utils.HandleNotFound(c, "会话不存在")
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"session": gin.H{
			"id":              status.ID,
			"fileName":        status.FileName,
			"fileSize":        status.FileSize,
			"chunkSize":       status.ChunkSize,
			"status":          status.Status,
			"totalChunks":     status.TotalChunks,
			"uploadedIndexes": status.UploadedIndexes,
			"expired":         status.Expired,
			"expiredAt":       status.ExpiredAt,
		},
	})
}

// ResumeSession 续传会话
func (h *UploadSessionHandler) ResumeSession(c *gin.Context) {
	uploadIDStr := c.Param("id")
	uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
	if err != nil {
		utils.HandleBadRequest(c, "无效的会话ID", nil)
		return
	}

	status, err := h.service.Resume(uint(uploadID))
	if err != nil {
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "续传成功", gin.H{
		"uploadId":        status.ID,
		"uploadedIndexes": status.UploadedIndexes,
		"expiredAt":       status.ExpiredAt,
	})
}

// CancelSession 取消会话
func (h *UploadSessionHandler) CancelSession(c *gin.Context) {
	uploadIDStr := c.Param("id")
	uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
	if err != nil {
		utils.HandleBadRequest(c, "无效的会话ID", nil)
		return
	}

	if err := h.service.Cancel(uint(uploadID)); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	middleware.LogOperation(c, "upload.session.cancel", fmt.Sprintf("session %d", uploadID), nil)
	utils.HandleSuccess(c, http.StatusOK, "会话已取消", nil)
}

// UploadChunk 上传分片
func (h *UploadSessionHandler) UploadChunk(c *gin.Context) {
	uploadIDStr := c.PostForm("uploadId")
	chunkIndexStr := c.PostForm("chunkIndex")

	uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
	if err != nil {
		utils.HandleBadRequest(c, "无效的会话ID", nil)
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil || chunkIndex < 0 {
		utils.HandleBadRequest(c, "无效的分片索引", nil)
		return
	}

	file, err := c.FormFile("chunk")
	if err != nil {
		utils.HandleBadRequest(c, "未找到分片文件: "+err.Error(), nil)
		return
	}

	srcFile, err := file.Open()
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "打开分片文件失败: "+err.Error(), nil)
		return
	}
	defer srcFile.Close()

	checksum := c.PostForm("checksum")

	if err := h.service.UploadChunk(&services.UploadChunkReq{
		UploadID:   uint(uploadID),
		ChunkIndex: chunkIndex,
		ChunkData:  srcFile,
		ChunkSize:  file.Size,
		Checksum:   checksum,
	}); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "分片上传成功", gin.H{"chunkIndex": chunkIndex})
}

// FinalizeSession 完成上传
func (h *UploadSessionHandler) FinalizeSession(c *gin.Context) {
	var req struct {
		UploadID uint64 `json:"uploadId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "无效的会话ID: "+err.Error(), nil)
		return
	}

	// 获取用户角色：匿名用户无角色，admin 可覆盖已有文件
	role, _ := c.Get(string(constants.ContextKeyRole))
	allowOverwrite := role == "admin"

	result, err := h.service.Finalize(uint(req.UploadID), allowOverwrite)
	if err != nil {
		if errors.Is(err, services.ErrFileExists) {
			utils.HandleErrorCompat(c, http.StatusConflict, err.Error(), nil)
		} else {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		}
		return
	}

	middleware.LogOperation(c, "upload.session.finalize", result.FileName, nil)
	utils.HandleSuccess(c, http.StatusOK, "文件上传完成", gin.H{})
}

// CleanupExpiredSessions 清理过期会话
func (h *UploadSessionHandler) CleanupExpiredSessions() (int, error) {
	return h.service.CleanupExpired()
}

// ListUploadSessions 列出当前用户的上传会话
func (h *UploadSessionHandler) ListUploadSessions(c *gin.Context) {
	sessionType := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	clientIP := utils.GetClientIP(c)

	switch sessionType {
	case "public":
		sessions, total, err := h.service.ListByUser(clientIP, models.TargetTypeRegular, page, pageSize)
		if err != nil {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
			return
		}
		result := make([]gin.H, 0, len(sessions))
		for _, s := range sessions {
			result = append(result, gin.H{
				"id":        s.ID,
				"fileName":  s.FileName,
				"fileSize":  s.FileSize,
				"status":    s.Status,
				"createdAt": s.CreatedAt,
				"expiredAt": s.ExpiredAt,
			})
		}
		utils.HandleSuccess(c, http.StatusOK, "", gin.H{"sessions": result, "total": total})

	case "temp":
		sessions, total, err := h.service.ListByIPAndType(clientIP, models.TargetTypeTemp, page, pageSize)
		if err != nil {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
			return
		}
		result := make([]gin.H, 0, len(sessions))
		for _, s := range sessions {
			result = append(result, gin.H{
				"id":        s.ID,
				"fileName":  s.FileName,
				"fileSize":  s.FileSize,
				"status":    s.Status,
				"createdAt": s.CreatedAt,
				"expiredAt": s.ExpiredAt,
			})
		}
		utils.HandleSuccess(c, http.StatusOK, "", gin.H{"sessions": result, "total": total})

	case "private":
		sessions, total, err := h.service.ListByUser(clientIP, models.TargetTypePrivate, page, pageSize)
		if err != nil {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
			return
		}
		result := make([]gin.H, 0, len(sessions))
		for _, s := range sessions {
			result = append(result, gin.H{
				"id":        s.ID,
				"fileName":  s.FileName,
				"fileSize":  s.FileSize,
				"status":    s.Status,
				"createdAt": s.CreatedAt,
				"expiredAt": s.ExpiredAt,
			})
		}
		utils.HandleSuccess(c, http.StatusOK, "", gin.H{"sessions": result, "total": total})

	default:
		utils.HandleBadRequest(c, "无效的会话类型", nil)
	}
}
