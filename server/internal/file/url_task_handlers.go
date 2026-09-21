package file

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UploadFromURLRequest URL上传请求
type UploadFromURLRequest struct {
	URL         string `json:"url" binding:"required"`
	Filename    string `json:"filename"`
	Dir         string `json:"dir"`
	RootName    string `json:"rootName"`
	StorageType string `json:"storageType"`
}

const (
	storageTypeRegular = "regular"
	storageTypeTemp    = "temp"
	storageTypePrivate = "private"
)

// UploadFromURL 从URL上传文件（异步，返回 taskId 供前端轮询进度）
func (h *UploadSessionHandler) UploadFromURL(c *gin.Context) {
	var req UploadFromURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}

	// 检查公共目录上传开关（regular 和空 TargetType 视为公共上传）
	if (req.StorageType == "" || req.StorageType == storageTypeRegular) && !appconfig.GlobalConfig.Upload.Enabled {
		utils.HandleErrorCompat(c, http.StatusForbidden, "公共目录上传已关闭", nil)
		return
	}

	urlCfg := utils.URLUploadConfig{
		Enabled:         appconfig.GlobalConfig.Upload.URLUpload.Enabled,
		AllowedIPRanges: appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges,
	}

	isFTP := strings.HasPrefix(req.URL, "ftp://") || strings.HasPrefix(req.URL, "ftps://")
	if isFTP {
		// FTP/FTPS URL 也需要进行 SSRF 安全检查（提取主机部分验证 IP）
		if parsedURL, parseErr := url.Parse(req.URL); parseErr == nil && parsedURL.Host != "" {
			ftpHost := parsedURL.Host
			if hostOnly, _, portErr := net.SplitHostPort(ftpHost); portErr == nil {
				ftpHost = hostOnly
			}
			if err := utils.IsURLSafe("http://"+ftpHost+"/", urlCfg); err != nil {
				utils.HandleErrorCompat(c, http.StatusBadRequest, "FTP URL安全验证失败: "+err.Error(), nil)
				return
			}
		}
	} else {
		if err := utils.IsURLSafe(req.URL, urlCfg); err != nil {
			utils.HandleErrorCompat(c, http.StatusBadRequest, "URL安全验证失败: "+err.Error(), nil)
			return
		}
	}

	var filename string
	var fileSize int64
	var etag string
	var resumeSupported bool
	var resumeURL string
	var err error

	if isFTP {
		// FTP/FTPS 协议：使用 FTP 客户端获取文件信息
		info, ftpErr := GetFTPFileInfo(req.URL)
		if ftpErr != nil {
			utils.HandleErrorCompat(c, http.StatusBadGateway, "获取 FTP 文件信息失败: "+ftpErr.Error(), nil)
			return
		}
		filename = info.FileName
		fileSize = info.FileSize
		etag = ""
		resumeSupported = false
		resumeURL = req.URL
	} else {
		// HTTP/HTTPS 协议：使用 HEAD 请求获取文件信息（含 DNS 重绑定防护）
		client := createSSRFProtectedHTTPClient(30*time.Second, urlCfg)
		headReq, headErr := http.NewRequest("HEAD", req.URL, nil)
		if headErr != nil {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建请求失败: "+headErr.Error(), nil)
			return
		}

		headResp, headDoErr := client.Do(headReq)
		if headDoErr != nil {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, "获取文件信息失败: "+headDoErr.Error(), nil)
			return
		}
		headResp.Body.Close()

		if headResp.StatusCode != http.StatusOK {
			utils.HandleErrorCompat(c, http.StatusBadGateway, "无法访问远程文件，状态码: "+strconv.Itoa(headResp.StatusCode), nil)
			return
		}

		filename = req.Filename
		if filename == "" {
			cd := headResp.Header.Get("Content-Disposition")
			if cd != "" {
				re := regexp.MustCompile(`filename[^;=\n]*=(['"]?)([^;"\n]+)\1`)
				if matches := re.FindStringSubmatch(cd); len(matches) > 2 {
					filename = matches[2]
				}
			}
			if filename == "" {
				filename = filepath.Base(req.URL)
			}
		}
		if filename == "" || filename == "/" || filename == "." {
			filename = "downloaded_file"
		}

		fileSize = headResp.ContentLength
		if fileSize <= 0 {
			utils.HandleErrorCompat(c, http.StatusBadGateway, "无法获取远程文件大小", nil)
			return
		}

		contentType := headResp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			utils.HandleErrorCompat(c, http.StatusBadGateway, "远程 URL 返回 HTML 页面，不是有效文件", nil)
			return
		}

		etag = headResp.Header.Get("ETag")
		resumeSupported = strings.Contains(headResp.Header.Get("Accept-Ranges"), "bytes")
		resumeURL = headResp.Request.URL.String()
	}

	targetType := models.TargetTypeRegular
	switch req.StorageType {
	case storageTypeTemp:
		targetType = models.TargetTypeTemp
	case storageTypePrivate:
		targetType = models.TargetTypePrivate
	}

	clientIP := utils.GetClientIP(c)
	var targetPath, uploadingPath string
	var sessionTargetPath, sessionTargetRoot string
	var sessionTargetType models.TargetType

	if targetType == models.TargetTypeTemp {
		sessionTargetPath = req.Dir
		sessionTargetRoot = ""
		sessionTargetType = targetType
		targetPath, uploadingPath, err = h.buildTempTargetPath(filename, clientIP, req.Dir)
		if err != nil {
			utils.HandleBadRequest(c, err.Error(), nil)
			return
		}
	} else {
		if req.RootName == "" {
			utils.HandleBadRequest(c, "缺少根目录名称", nil)
			return
		}
		// 按目录检查：与普通上传一致，禁止向不允许上传的共享目录发 URL 上传
		if !appconfig.PublicDirUploadAllowed(req.RootName) {
			utils.HandleErrorCompat(c, http.StatusForbidden, "该共享目录不允许上传文件", nil)
			return
		}
		session := &models.UploadSession{
			FileName:  filename,
			FileSize:  fileSize,
			ChunkSize: h.chunkSize,
			// 溢出安全: fileSize>0 已校验, 用 (fileSize-1)/chunkSize+1 求向上取整, 避免 fileSize+chunkSize-1 整数溢出为负数
			TotalChunks: int((fileSize-1)/h.chunkSize + 1),
			Status:      models.UploadStatusInProgress,
			TargetType:  targetType,
			TargetPath:  req.Dir,
			TargetRoot:  req.RootName,
			UserID:      clientIP,
			ExpiredAt:   utils.Now().Add(24 * time.Hour),
		}

		targetPath, uploadingPath, err = h.buildTargetPath(session)
		if err != nil {
			utils.HandleBadRequest(c, err.Error(), nil)
			return
		}
		sessionTargetPath = session.TargetPath
		sessionTargetRoot = session.TargetRoot
		sessionTargetType = session.TargetType
	}

	if _, err := os.Stat(targetPath); err == nil {
		utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
		return
	}
	if err := EnsureUploadingFile(uploadingPath, fileSize); err != nil {
		os.Remove(uploadingPath)
		utils.Error("URL下载创建上传文件失败", utils.String("path", uploadingPath), utils.Err(err))
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建上传文件失败", nil)
		return
	}

	taskID := uuid.New().String()
	anonID := c.GetHeader("X-Anonymous-ID")
	var userIDPtr *string
	if uid, exists := c.Get("userUUID"); exists {
		if s, ok := uid.(string); ok && s != "" {
			userIDPtr = &s
		}
	}
	urlTask := &models.URLDownloadTask{
		ID:              taskID,
		URL:             req.URL,
		FileName:        filename,
		FileSize:        fileSize,
		StorageType:     models.URLDownloadStorageType(targetType),
		RootName:        req.RootName,
		Status:          models.URLDownloadStatusDownloading,
		TargetPath:      targetPath,
		TempFilePath:    uploadingPath, // 记录下载中的临时路径，供中断对账/取消时清理孤儿文件
		Notified:        false,
		UserID:          userIDPtr,
		ETag:            etag,
		ResumeSupported: resumeSupported,
		ResumeURL:       resumeURL,
	}
	if anonID != "" {
		urlTask.AnonymousID = &anonID
	}
	if err := h.urlTaskRepo.Create(urlTask); err != nil {
		utils.Error("创建URL下载任务记录失败", utils.Err(err))
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建下载任务失败", nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"taskId":   taskID,
		"taskType": "url",
		"fileName": filename,
		"fileSize": fileSize,
	})

	go h.downloadFromURL(taskID, req.URL, fileSize, filename, clientIP, uploadingPath, targetPath, sessionTargetPath, sessionTargetRoot, sessionTargetType)
}

// GetURLTask 获取 URL 下载任务进度
func (h *UploadSessionHandler) GetURLTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	task, err := h.urlTaskRepo.GetByID(taskID)
	if err != nil {
		utils.HandleNotFound(c, "任务不存在")
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"task": gin.H{
			"id":              task.ID,
			"fileName":        task.FileName,
			"fileSize":        task.FileSize,
			"downloadedBytes": task.DownloadedBytes,
			"status":          task.Status,
			"errorMessage":    task.ErrorMessage,
			"createdAt":       task.CreatedAt,
			"completedAt":     task.CompletedAt,
		},
	})
}

// CancelURLTask 取消 URL 下载任务
func (h *UploadSessionHandler) CancelURLTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	task, err := h.urlTaskRepo.GetByID(taskID)
	if err != nil {
		utils.HandleNotFound(c, "任务不存在")
		return
	}

	if !h.checkTaskOwnership(c, task) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
		return
	}

	if task.Status != models.URLDownloadStatusPending && task.Status != models.URLDownloadStatusDownloading {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "只能取消进行中的任务", nil)
		return
	}

	if err := h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusCancelled, "用户取消"); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "取消失败", nil)
		return
	}

	if task.TargetPath != "" {
		os.Remove(task.TargetPath)
	}

	middleware.LogOperation(c, "upload.url.cancel", taskID, nil)
	utils.HandleSuccess(c, http.StatusOK, "任务已取消", nil)
}

// checkTaskOwnership 检查当前用户是否拥有该任务
func (h *UploadSessionHandler) checkTaskOwnership(c *gin.Context, task *models.URLDownloadTask) bool {
	if uid, exists := c.Get("userUUID"); exists {
		if s, ok := uid.(string); ok && s != "" && task.UserID != nil && *task.UserID == s {
			return true
		}
	}
	anonID := c.GetHeader("X-Anonymous-ID")
	if anonID != "" && task.AnonymousID != nil && *task.AnonymousID == anonID {
		return true
	}
	return false
}

// ListURLTasks 列出当前用户的 URL 下载任务
func (h *UploadSessionHandler) ListURLTasks(c *gin.Context) {
	storageType := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	var userID, anonymousID *string
	if uid, exists := c.Get("userUUID"); exists {
		if s, ok := uid.(string); ok && s != "" {
			userID = &s
		}
	}
	anonID := c.GetHeader("X-Anonymous-ID")
	if anonID != "" {
		anonymousID = &anonID
	}

	mappedType := ""
	switch storageType {
	case "public":
		mappedType = string(models.URLDownloadStorageRegular)
	case "temp":
		mappedType = string(models.URLDownloadStorageTemp)
	case "private":
		mappedType = string(models.URLDownloadStoragePrivate)
	}

	tasks, total, err := h.urlTaskRepo.ListByUser(userID, anonymousID, mappedType, page, pageSize)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
		return
	}

	result := make([]gin.H, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, gin.H{
			"id":              t.ID,
			"fileName":        t.FileName,
			"fileSize":        t.FileSize,
			"downloadedBytes": t.DownloadedBytes,
			"status":          t.Status,
			"errorMessage":    t.ErrorMessage,
			"createdAt":       t.CreatedAt,
			"completedAt":     t.CompletedAt,
			"storageType":     t.StorageType,
		})
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"tasks": result,
		"total": total,
	})
}

// RetryURLTask 重试失败或已取消的 URL 下载任务
func (h *UploadSessionHandler) RetryURLTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	task, err := h.urlTaskRepo.GetByID(taskID)
	if err != nil {
		utils.HandleNotFound(c, "任务不存在")
		return
	}

	if !h.checkTaskOwnership(c, task) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
		return
	}

	if task.Status != models.URLDownloadStatusFailed && task.Status != models.URLDownloadStatusCancelled {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "只能重试失败或已取消的任务", nil)
		return
	}

	if err := h.urlTaskRepo.ResetToPending(taskID); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "重试失败", nil)
		return
	}

	uploadingPath, targetPath, sessionTargetPath, sessionTargetRoot, sessionTargetType := h.buildURLRetryPaths(task)

	// 优先断点续传：复用已存在的部分文件，避免整份重下
	resumed := false
	if partial, ok := h.resumableUploadingPath(task, targetPath); ok {
		uploadingPath = partial
		resumed = true
	}

	if !resumed {
		// 全新下载：清理旧的部分文件与失败残留目标，避免直接写目标或 finalize 误判"目标已存在"
		if task.TempFilePath != "" && task.TempFilePath != uploadingPath {
			if rmErr := os.Remove(task.TempFilePath); rmErr != nil && !os.IsNotExist(rmErr) {
				utils.Warn("清理URL下载旧部分文件失败",
					utils.String("path", task.TempFilePath), utils.Err(rmErr))
			}
		}
		if targetPath != "" {
			if rmErr := os.Remove(targetPath); rmErr != nil && !os.IsNotExist(rmErr) {
				utils.Warn("清理URL下载失败残留目标文件失败",
					utils.String("path", targetPath), utils.Err(rmErr))
			}
		}
	}
	// 记录本次实际使用的临时路径，供取消/删除/启动对账清理
	if uploadingPath != "" {
		_ = h.urlTaskRepo.UpdateTempFilePath(taskID, uploadingPath)
	}

	go h.downloadFromURL(taskID, task.URL, task.FileSize, task.FileName, "", uploadingPath, targetPath, sessionTargetPath, sessionTargetRoot, sessionTargetType)

	middleware.LogOperation(c, "upload.url.retry", taskID, nil)
	utils.HandleSuccess(c, http.StatusOK, "任务已重试", nil)
}

// resumableUploadingPath 判断是否可用已有部分文件断点续传：
// 服务器支持 Range、存在有效（非目录、0<大小<目标）且仍位于目标同目录的部分文件。
func (h *UploadSessionHandler) resumableUploadingPath(task *models.URLDownloadTask, targetPath string) (string, bool) {
	if !task.ResumeSupported || task.TempFilePath == "" || targetPath == "" {
		return "", false
	}
	fi, err := os.Stat(task.TempFilePath)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 || fi.Size() >= task.FileSize {
		return "", false
	}
	if filepath.Dir(task.TempFilePath) != filepath.Dir(targetPath) {
		return "", false
	}
	return task.TempFilePath, true
}

// buildURLRetryPaths 根据已存的下载任务还原重试所需的路径与会话上下文。
// 重试写入目标所在目录下的专用 `.retry.uploading` 临时文件，避免直接写目标，
// 下载完成后由 downloadFromURL/finalizeURLDownload 原子重命名到最终路径。
func (h *UploadSessionHandler) buildURLRetryPaths(task *models.URLDownloadTask) (uploadingPath, targetPath, sessionTargetPath, sessionTargetRoot string, sessionTargetType models.TargetType) {
	if task.TargetPath == "" {
		return
	}
	targetPath = task.TargetPath
	base := filepath.Base(targetPath)
	dir := filepath.Dir(targetPath)
	// 用任务 ID 保证临时文件唯一且可追溯
	uploadingPath = filepath.Join(dir, "."+base+"."+task.ID+".retry.uploading")

	switch task.StorageType {
	case models.URLDownloadStorageRegular:
		sessionTargetType = models.TargetTypeRegular
		sessionTargetRoot = task.RootName
	case models.URLDownloadStoragePrivate:
		sessionTargetType = models.TargetTypePrivate
		sessionTargetRoot = task.RootName
	case models.URLDownloadStorageTemp:
		sessionTargetType = models.TargetTypeTemp
	}

	// 还原目标相对根目录的目录（用于记录/索引的完整路径）
	if sessionTargetRoot != "" {
		if rootPath := appconfig.RootNames[sessionTargetRoot]; rootPath != "" {
			if rootAbs, err1 := filepath.Abs(rootPath); err1 == nil {
				if tgtAbs, err2 := filepath.Abs(targetPath); err2 == nil {
					sep := string(filepath.Separator)
					if strings.HasPrefix(tgtAbs, rootAbs+sep) {
						rel := strings.TrimPrefix(tgtAbs, rootAbs+sep)
						relDir := filepath.ToSlash(filepath.Dir(rel))
						if relDir != "." && relDir != "/" {
							sessionTargetPath = relDir
						}
					}
				}
			}
		}
	}
	return
}

// DeleteURLTask 删除 URL 下载任务
func (h *UploadSessionHandler) DeleteURLTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	task, err := h.urlTaskRepo.GetByID(taskID)
	if err != nil {
		utils.HandleNotFound(c, "任务不存在")
		return
	}

	if !h.checkTaskOwnership(c, task) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
		return
	}

	if err := h.urlTaskRepo.Delete(taskID); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "删除失败", nil)
		return
	}

	middleware.LogOperation(c, "upload.url.delete", taskID, nil)
	utils.HandleSuccess(c, http.StatusOK, "任务已删除", nil)
}
