package file

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
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

	var err error

	filename, fileSize, etag, resumeSupported, resumeURL, probeErr := probeRemoteFileInfo(req.URL, urlCfg, req.Filename)
	if probeErr != nil {
		utils.HandleErrorCompat(c, http.StatusBadGateway, probeErr.Error(), nil)
		return
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
	var sessionTargetPath string

	if targetType == models.TargetTypeTemp {
		sessionTargetPath = req.Dir
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
	}

	if _, err := os.Stat(targetPath); err == nil {
		utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
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
		Status:          models.URLDownloadStatusPending, // 入队等待，由后台队列按全局并行上限调度
		TargetPath:      targetPath,
		TempFilePath:    uploadingPath, // 记录上传中的临时路径，供队列执行/中断对账/取消时清理
		ClientIP:        clientIP,
		SessionDir:      sessionTargetPath,
		QueuedAt:        utils.Now(), // 入队时间：调度按此排序，重试时刷新以排到队尾
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
		utils.Error("创建URL上传任务记录失败", utils.Err(err))
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建上传任务失败", nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"taskId":   taskID,
		"taskType": "url",
		"fileName": filename,
		"fileSize": fileSize,
		"status":   models.URLDownloadStatusPending,
	})

	// 任务已持久化入队，与当前请求/浏览器解耦；唤醒后台调度器按全局并行上限执行
	if h.dispatcher != nil {
		h.dispatcher.Notify()
	}
}

// probeRemoteFileInfo 对远端 URL 执行探测，返回文件名/大小/ETag/是否支持续传/最终 URL。
// 支持 http/https（HEAD，含 DNS 重绑定防护）与 ftp/ftps。preferredName 非空时直接采用该文件名。
func probeRemoteFileInfo(downloadURL string, urlCfg utils.URLUploadConfig, preferredName string) (filename string, fileSize int64, etag string, resumeSupported bool, resumeURL string, err error) {
	isFTP := strings.HasPrefix(downloadURL, "ftp://") || strings.HasPrefix(downloadURL, "ftps://")
	if isFTP {
		info, ftpErr := GetFTPFileInfo(downloadURL)
		if ftpErr != nil {
			return "", 0, "", false, "", fmt.Errorf("获取 FTP 文件信息失败: %w", ftpErr)
		}
		return info.FileName, info.FileSize, "", false, downloadURL, nil
	}

	client := createSSRFProtectedHTTPClient(30*time.Second, urlCfg)
	headReq, headErr := http.NewRequest("HEAD", downloadURL, nil)
	if headErr != nil {
		return "", 0, "", false, "", fmt.Errorf("创建请求失败: %w", headErr)
	}
	headResp, headDoErr := client.Do(headReq)
	if headDoErr != nil {
		return "", 0, "", false, "", fmt.Errorf("获取文件信息失败: %w", headDoErr)
	}
	defer headResp.Body.Close()

	if headResp.StatusCode != http.StatusOK {
		return "", 0, "", false, "", fmt.Errorf("无法访问远程文件，状态码: %d", headResp.StatusCode)
	}

	filename = preferredName
	if filename == "" {
		if cd := headResp.Header.Get("Content-Disposition"); cd != "" {
			re := regexp.MustCompile(`filename[^;=\n]*=(['"]?)([^;"\n]+)\1`)
			if matches := re.FindStringSubmatch(cd); len(matches) > 2 {
				filename = matches[2]
			}
		}
		if filename == "" {
			filename = filepath.Base(downloadURL)
		}
	}
	if filename == "" || filename == "/" || filename == "." {
		filename = "downloaded_file"
	}

	fileSize = headResp.ContentLength
	if fileSize <= 0 {
		return "", 0, "", false, "", fmt.Errorf("无法获取远程文件大小")
	}
	if strings.Contains(headResp.Header.Get("Content-Type"), "text/html") {
		return "", 0, "", false, "", fmt.Errorf("远程 URL 返回 HTML 页面，不是有效文件")
	}

	etag = headResp.Header.Get("ETag")
	resumeSupported = strings.Contains(headResp.Header.Get("Accept-Ranges"), "bytes")
	resumeURL = headResp.Request.URL.String()
	return filename, fileSize, etag, resumeSupported, resumeURL, nil
}

// URLProbeRequest 批量探测请求
type URLProbeRequest struct {
	URLs        []string `json:"urls" binding:"required"`
	Dir         string   `json:"dir"`
	RootName    string   `json:"rootName"`
	StorageType string   `json:"storageType"`
}

// ProbeURLs 批量探测 URL：串行 HEAD 取文件名/大小，并检查目标文件是否已存在、
// 该 URL 是否已成功上传过（全局范围）。供前端添加条目时即时提示与文件名编辑。
func (h *UploadSessionHandler) ProbeURLs(c *gin.Context) {
	var req URLProbeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}
	if len(req.URLs) == 0 {
		utils.HandleBadRequest(c, "请至少提供一个 URL", nil)
		return
	}
	if len(req.URLs) > 100 {
		utils.HandleBadRequest(c, "单次最多探测 100 个 URL", nil)
		return
	}

	urlCfg := utils.URLUploadConfig{
		Enabled:         appconfig.GlobalConfig.Upload.URLUpload.Enabled,
		AllowedIPRanges: appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges,
	}
	clientIP := utils.GetClientIP(c)

	targetType := models.TargetTypeRegular
	switch req.StorageType {
	case storageTypeTemp:
		targetType = models.TargetTypeTemp
	case storageTypePrivate:
		targetType = models.TargetTypePrivate
	}

	items := make([]gin.H, 0, len(req.URLs))
	// 串行探测：逐个 HEAD，避免对远端造成瞬时压力
	for _, raw := range req.URLs {
		item := gin.H{"url": raw}
		downloadURL := strings.TrimSpace(raw)
		if downloadURL == "" {
			item["error"] = "空 URL"
			items = append(items, item)
			continue
		}

		isFTP := strings.HasPrefix(downloadURL, "ftp://") || strings.HasPrefix(downloadURL, "ftps://")
		isHTTP := strings.HasPrefix(downloadURL, "http://") || strings.HasPrefix(downloadURL, "https://")
		if !isHTTP && !isFTP {
			item["error"] = "不支持的 URL 协议，仅支持 http/https/ftp/ftps"
			items = append(items, item)
			continue
		}

		if isFTP {
			if parsedURL, parseErr := url.Parse(downloadURL); parseErr == nil && parsedURL.Host != "" {
				ftpHost := parsedURL.Host
				if hostOnly, _, portErr := net.SplitHostPort(ftpHost); portErr == nil {
					ftpHost = hostOnly
				}
				if err := utils.IsURLSafe("http://"+ftpHost+"/", urlCfg); err != nil {
					item["error"] = "FTP URL安全验证失败: " + err.Error()
					items = append(items, item)
					continue
				}
			}
		} else if err := utils.IsURLSafe(downloadURL, urlCfg); err != nil {
			item["error"] = "URL安全验证失败: " + err.Error()
			items = append(items, item)
			continue
		}

		filename, fileSize, _, _, _, probeErr := probeRemoteFileInfo(downloadURL, urlCfg, "")
		if probeErr != nil {
			item["error"] = probeErr.Error()
			items = append(items, item)
			continue
		}
		item["fileName"] = filename
		item["fileSize"] = fileSize

		if targetPath := h.probeTargetPath(targetType, filename, clientIP, req.Dir, req.RootName); targetPath != "" {
			_, statErr := os.Stat(targetPath)
			item["targetExists"] = statErr == nil
		}

		if exists, histErr := h.urlTaskRepo.ExistsCompletedByURL(downloadURL); histErr == nil {
			item["alreadyUploaded"] = exists
		} else {
			item["alreadyUploaded"] = false
		}
		items = append(items, item)
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{"items": items})
}

// probeTargetPath 计算探测时的目标文件路径（仅用于存在性检查，不落库）。
func (h *UploadSessionHandler) probeTargetPath(targetType models.TargetType, filename, clientIP, dir, rootName string) string {
	if targetType == models.TargetTypeTemp {
		targetPath, _, err := h.buildTempTargetPath(filename, clientIP, dir)
		if err != nil {
			return ""
		}
		return targetPath
	}
	if rootName == "" {
		return ""
	}
	session := &models.UploadSession{
		FileName:   filename,
		TargetType: targetType,
		TargetPath: dir,
		TargetRoot: rootName,
	}
	targetPath, _, err := h.buildTargetPath(session)
	if err != nil {
		return ""
	}
	return targetPath
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

	if !h.checkTaskOwnership(c, task) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权查看此任务", nil)
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
			"retryCount":      task.RetryCount,
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

	// 先中断进行中的下载（若在跑），再置为已取消；状态带守卫，避免被 worker 的终态写入覆盖
	running := false
	if h.dispatcher != nil {
		running = h.dispatcher.CancelTask(taskID)
	}
	if _, err := h.urlTaskRepo.UpdateStatusFrom(taskID,
		[]models.URLDownloadStatus{models.URLDownloadStatusPending, models.URLDownloadStatusDownloading},
		models.URLDownloadStatusCancelled, "用户取消"); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "取消失败", nil)
		return
	}

	// 仅当任务未被 worker 执行时才由请求方清理临时文件，避免与 worker 写文件竞争；
	// 正在执行的任务由 worker 感知取消后自行清理。
	if !running && task.TempFilePath != "" {
		os.Remove(task.TempFilePath)
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
			"retryCount":      t.RetryCount,
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

	// 安全清理可能残留的目标文件：仅当目标确由本任务上传者创建时才删除，
	// 避免误删共享目录中他人的同名文件（否则重试会因"目标已存在"失败，但不会造成破坏）
	h.removeTargetIfOwned(task)

	// 重新入队，由后台队列调度执行；断点续传由 worker 依据 TempFilePath 自动处理
	if h.dispatcher != nil {
		h.dispatcher.Notify()
	}

	middleware.LogOperation(c, "upload.url.retry", taskID, nil)
	utils.HandleSuccess(c, http.StatusOK, "任务已重试", nil)
}

// removeTargetIfOwned 在重试前安全清理目标文件：
// 临时目录按客户端 IP 隔离，可安全删除；公共目录则仅当索引记录的上传者 IP 与本任务一致时才删除，
// 无法确认归属时一律跳过（宁可让重试以"目标已存在"失败，也不误删他人文件）。
func (h *UploadSessionHandler) removeTargetIfOwned(task *models.URLDownloadTask) {
	if task.TargetPath == "" {
		return
	}
	if task.StorageType == models.URLDownloadStorageTemp {
		if err := os.Remove(task.TargetPath); err != nil && !os.IsNotExist(err) {
			utils.Warn("清理URL上传残留目标文件失败",
				utils.String("path", task.TargetPath), utils.Err(err))
		}
		return
	}
	if task.RootName == "" || task.ClientIP == "" {
		return
	}
	rootPath := appconfig.RootNames[task.RootName]
	if rootPath == "" {
		return
	}
	rel, err := filepath.Rel(rootPath, task.TargetPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	relPath := "/" + filepath.ToSlash(rel)
	var owner string
	if err := h.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND file_path = ?", task.RootName, relPath).
		Pluck("uploader_ip", &owner).Error; err != nil {
		utils.Warn("查询目标文件归属失败，跳过清理",
			utils.String("path", task.TargetPath), utils.Err(err))
		return
	}
	if owner == "" || owner != task.ClientIP {
		utils.Warn("重试跳过删除目标文件（非本任务上传者创建，避免误删）",
			utils.String("path", task.TargetPath),
			utils.String("taskId", task.ID))
		return
	}
	if err := os.Remove(task.TargetPath); err != nil && !os.IsNotExist(err) {
		utils.Warn("清理URL上传残留目标文件失败",
			utils.String("path", task.TargetPath), utils.Err(err))
	}
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

	// 先中断进行中的下载，避免删除记录后 worker 仍继续写入产生孤儿文件
	if h.dispatcher != nil {
		h.dispatcher.CancelTask(taskID)
	}
	if err := h.urlTaskRepo.Delete(taskID); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "删除失败", nil)
		return
	}

	middleware.LogOperation(c, "upload.url.delete", taskID, nil)
	utils.HandleSuccess(c, http.StatusOK, "任务已删除", nil)
}

// URLTaskChecksumRequest 计算校验值请求：algorithms 为 md5/sha1/sha256 的任意子集
type URLTaskChecksumRequest struct {
	Algorithms []string `json:"algorithms" binding:"required"`
}

// supportedChecksumAlgorithms 支持的校验算法白名单
var supportedChecksumAlgorithms = map[string]func() hash.Hash{
	"md5":    md5.New,
	"sha1":   sha1.New,
	"sha256": sha256.New,
}

// ComputeURLTaskChecksum 计算已完成 URL 上传任务的校验值（md5/sha1/sha256，可多选）。
// 文件已落盘在服务器，按所选算法单次流式读取计算；结果仅返回给调用方，不落库。
func (h *UploadSessionHandler) ComputeURLTaskChecksum(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	var req URLTaskChecksumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}
	if len(req.Algorithms) == 0 {
		utils.HandleBadRequest(c, "请至少选择一种校验算法", nil)
		return
	}

	// 先校验任务归属，再做算法白名单校验，避免未授权调用方通过错误信息区分算法是否受支持
	task, err := h.urlTaskRepo.GetByID(taskID)
	if err != nil {
		utils.HandleNotFound(c, "任务不存在")
		return
	}
	if !h.checkTaskOwnership(c, task) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
		return
	}

	// 校验算法白名单并按去重后的顺序构造哈希器
	hashers := make(map[string]hash.Hash, len(req.Algorithms))
	writers := make([]io.Writer, 0, len(req.Algorithms))
	for _, algo := range req.Algorithms {
		name := strings.ToLower(strings.TrimSpace(algo))
		if _, exists := hashers[name]; exists {
			continue
		}
		newHash, ok := supportedChecksumAlgorithms[name]
		if !ok {
			utils.HandleBadRequest(c, "不支持的校验算法: "+algo, nil)
			return
		}
		h := newHash()
		hashers[name] = h
		writers = append(writers, h)
	}

	if task.Status != models.URLDownloadStatusCompleted {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "仅已完成的任务可计算校验值", nil)
		return
	}
	if task.TargetPath == "" {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "任务缺少目标文件路径", nil)
		return
	}

	file, err := os.Open(task.TargetPath)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusNotFound, "目标文件不存在或不可读", nil)
		return
	}
	defer file.Close()

	if _, err := io.Copy(io.MultiWriter(writers...), file); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "计算校验值失败: "+err.Error(), nil)
		return
	}

	checksums := make(map[string]string, len(hashers))
	for name, h := range hashers {
		checksums[name] = hex.EncodeToString(h.Sum(nil))
	}

	middleware.LogOperation(c, "upload.url.checksum", taskID, nil)
	utils.HandleSuccess(c, http.StatusOK, "", gin.H{"checksums": checksums})
}
