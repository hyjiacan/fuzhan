package file

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
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/pathutils"
	"github.com/zeebo/xxh3"
)

// buildTargetPath 构建目标文件路径和上传中文件路径（供 URL 下载使用）
func (h *UploadSessionHandler) buildTargetPath(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
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
	if !strings.HasPrefix(absTargetDir, absRootPath) {
		err = fmt.Errorf("路径越界")
		return
	}

	if err = os.MkdirAll(targetDir, 0755); err != nil {
		err = fmt.Errorf("创建目标目录失败: %w", err)
		return
	}

	decodedFilename, _ := url.QueryUnescape(session.FileName)
	cleanFilename := filepath.Clean(decodedFilename)

	if filepath.IsAbs(cleanFilename) ||
		cleanFilename == ".." ||
		strings.Contains(cleanFilename, "/") ||
		strings.Contains(cleanFilename, "\\") {
		err = fmt.Errorf("无效的文件名")
		return
	}

	targetPath = filepath.Join(targetDir, cleanFilename)
	// 上传中临时文件名加前导 .，避免与用户文件冲突，且通过前导 . 过滤隐藏
	uploadingPath = filepath.Join(filepath.Dir(targetPath), fmt.Sprintf(".%s.%d.uploading", filepath.Base(targetPath), session.ID))

	return
}

// generateTempCode 生成8位临时文件访问码
func (h *UploadSessionHandler) generateTempCode() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), nil
}

// buildTempTargetPath 构建临时文件的目标路径
func (h *UploadSessionHandler) buildTempTargetPath(filename string, clientIP string, dir string) (targetPath string, uploadingPath string, err error) {
	tempPath := appconfig.GlobalConfig.Storage.Temp.Path
	if tempPath == "" {
		err = fmt.Errorf("临时文件存储未配置")
		return
	}

	safeIP := strings.ReplaceAll(clientIP, ":", "_")
	baseDir := filepath.Join(tempPath, "files", safeIP)
	if dir != "" && dir != "/" {
		cleanDir := filepath.Clean(dir)
		if strings.Contains(cleanDir, "..") || filepath.IsAbs(cleanDir) {
			err = fmt.Errorf("无效的路径")
			return
		}
		baseDir = filepath.Join(baseDir, cleanDir)
	}

	if err = os.MkdirAll(baseDir, 0755); err != nil {
		err = fmt.Errorf("创建目录失败: %w", err)
		return
	}

	absBase, _ := filepath.Abs(baseDir)
	absRoot, _ := filepath.Abs(tempPath)
	if !strings.HasPrefix(absBase, absRoot) {
		err = fmt.Errorf("路径越界")
		return
	}

	cleanFilename := filepath.Clean(filename)
	if cleanFilename == "." || cleanFilename == ".." || strings.Contains(cleanFilename, "/") || strings.Contains(cleanFilename, "\\") {
		err = fmt.Errorf("无效的文件名")
		return
	}

	targetPath = filepath.Join(baseDir, cleanFilename)
	uploadingPath = filepath.Join(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".url-download.uploading")

	return
}

// EnsureUploadingFile 确保上传文件存在，必要时预分配空间（供 URL 下载使用）
func EnsureUploadingFile(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	if err := services.PreAllocate(file, size); err != nil {
		os.Remove(path)
		return err
	}

	return nil
}

// maxURLRetries 读取当前配置的 URL 上传自动重试次数上限（<=0 时回退默认值）。
func maxURLRetries() int {
	n := appconfig.GlobalConfig.Upload.URLUpload.MaxRetries
	if n <= 0 {
		n = appconfig.DefaultURLUploadMaxRetries
	}
	return n
}

// isRetryableHTTPStatus 判断 HTTP 状态码是否属于可重试错误：
// 5xx 服务端错误、408 请求超时、429 限流可重试；其余 4xx 客户端错误不重试。
func isRetryableHTTPStatus(code int) bool {
	if code >= 500 {
		return true
	}
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
}

// failURLTask 处理 URL 上传任务的一次失败：
// 当 retryable 为 true 且未达自动重试上限时，把任务重新入队到队尾并唤醒调度器，返回 true；
// 否则标记为失败并返回 false。
func (h *UploadSessionHandler) failURLTask(taskID, msg string, retryable bool) bool {
	if retryable {
		maxRetries := maxURLRetries()
		if task, err := h.urlTaskRepo.GetByID(taskID); err == nil && task != nil && task.RetryCount < maxRetries {
			rerr := h.urlTaskRepo.RequeueForRetry(taskID)
			if rerr == nil {
				utils.Warn("URL上传任务失败，已重新入队等待自动重试",
					utils.String("taskId", taskID),
					utils.Int("retry", task.RetryCount+1),
					utils.Int("maxRetries", maxRetries),
					utils.String("reason", msg))
				if h.dispatcher != nil {
					h.dispatcher.Notify()
				}
				return true
			}
			utils.Warn("URL上传任务重新入队失败，将直接标记为失败",
				utils.String("taskId", taskID), utils.Err(rerr))
		}
	}
	_ = h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, msg)
	return false
}

// executeURLDownloadTask 由后台队列调度器在领取到任务后调用：
// 从任务记录还原执行上下文并启动实际下载。并发上限由调度器 worker 数控制。
func (h *UploadSessionHandler) executeURLDownloadTask(task *models.URLDownloadTask) {
	if task.TargetPath == "" || task.TempFilePath == "" {
		_ = h.urlTaskRepo.UpdateStatus(task.ID, models.URLDownloadStatusFailed, "任务缺少目标路径")
		return
	}

	sessionTargetType := models.TargetTypeRegular
	sessionTargetRoot := task.RootName
	switch task.StorageType {
	case models.URLDownloadStorageTemp:
		sessionTargetType = models.TargetTypeTemp
		sessionTargetRoot = ""
	case models.URLDownloadStoragePrivate:
		sessionTargetType = models.TargetTypePrivate
	}

	// 目标文件已存在则直接失败（可能被其他任务/用户抢先创建）
	if _, statErr := os.Stat(task.TargetPath); statErr == nil {
		_ = h.urlTaskRepo.UpdateStatus(task.ID, models.URLDownloadStatusFailed, "目标文件已存在")
		return
	}

	// 预分配上传临时文件；已存在（断点续传的部分文件）时保留
	if err := EnsureUploadingFile(task.TempFilePath, task.FileSize); err != nil {
		os.Remove(task.TempFilePath)
		utils.Error("URL上传创建临时文件失败", utils.String("path", task.TempFilePath), utils.Err(err))
		_ = h.urlTaskRepo.UpdateStatus(task.ID, models.URLDownloadStatusFailed, "创建上传文件失败")
		return
	}

	h.downloadFromURL(task.ID, task.URL, task.FileSize, task.FileName, task.ClientIP,
		task.TempFilePath, task.TargetPath, task.SessionDir, sessionTargetRoot, sessionTargetType)
}

// downloadFromURL 后台下载文件并记录进度
func (h *UploadSessionHandler) downloadFromURL(taskID string, downloadURL string, fileSize int64, filename string, clientIP string, uploadingPath string, targetPath string, sessionTargetPath string, sessionTargetRoot string, sessionTargetType models.TargetType) {
	// 创建任务记录（供任务管理页面展示）
	var taskRecordID uint
	if h.taskSvc != nil {
		if task, err := h.taskSvc.CreateTask("url_download", "URL下载: "+filename, "user"); err == nil {
			taskRecordID = task.ID
			h.taskSvc.UpdateTaskDetails(task.ID, "文件名: "+filename)
			h.taskSvc.StartTask(task.ID)
		}
	}
	completeTask := func(success bool, errMsg string) {
		if taskRecordID > 0 && h.taskSvc != nil {
			if success {
				h.taskSvc.CompleteTask(taskRecordID)
			} else {
				h.taskSvc.FailTask(taskRecordID, errMsg)
			}
		}
	}

	var urlTaskUpdated bool
	var outFile *os.File
	var taskSucceeded bool
	var taskErrMsg string
	defer func() {
		if !urlTaskUpdated {
			h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "意外退出：状态未更新")
		}
		completeTask(taskSucceeded, taskErrMsg)
	}()

	defer func() {
		if r := recover(); r != nil {
			utils.Error("URL下载panic", utils.Any("recover", r))
			if outFile != nil {
				outFile.Close()
			}
			os.Remove(uploadingPath)
			h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "下载过程发生内部错误")
		}
	}()

	var downloaded int64

	outFile, err := os.OpenFile(uploadingPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		os.Remove(uploadingPath)
		urlTaskUpdated = true
		h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "打开上传文件失败")
		utils.Error("URL下载打开上传文件失败", utils.Err(err))
		return
	}

	isFTP := strings.HasPrefix(downloadURL, "ftp://") || strings.HasPrefix(downloadURL, "ftps://")

	if isFTP {
		outFile.Close()

		progressFn := func(d int64) {
			_ = h.urlTaskRepo.UpdateProgress(taskID, d)
		}

		if err := DownloadFromFTP(downloadURL, uploadingPath, progressFn); err != nil {
			os.Remove(uploadingPath)
			urlTaskUpdated = true
			h.failURLTask(taskID, "FTP 下载失败: "+err.Error(), true)
			utils.Error("FTP下载失败", utils.String("url", downloadURL), utils.Err(err))
			return
		}

		fi, fiErr := os.Stat(uploadingPath)
		if fiErr != nil {
			os.Remove(uploadingPath)
			urlTaskUpdated = true
			h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "获取 FTP 下载文件信息失败")
			utils.Error("FTP下载获取文件信息失败", utils.Err(fiErr))
			return
		}
		downloaded = fi.Size()

		if fileSize > 0 && downloaded != fileSize {
			utils.Warn("FTP下载大小不一致", utils.Int64("expected", fileSize), utils.Int64("actual", downloaded))
			os.Remove(uploadingPath)
			urlTaskUpdated = true
			h.failURLTask(taskID, "FTP 下载文件大小不匹配", true)
			return
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// ---- 断点续传判定：服务器支持 Range 且存在有效部分文件且未下满 ----
		dlTask, _ := h.urlTaskRepo.GetByID(taskID)
		resumable := false
		var resumeOffset int64
		if dlTask != nil && dlTask.ResumeSupported {
			if fi, statErr := os.Stat(uploadingPath); statErr == nil && fi.Mode().IsRegular() && fi.Size() > 0 && fi.Size() < fileSize {
				resumeOffset = fi.Size()
				resumable = true
			}
		}
		if resumable {
			// 从已有部分末尾续写，已下字节计入总量
			downloaded = resumeOffset
			outFile.Seek(resumeOffset, io.SeekStart)
			_ = h.urlTaskRepo.UpdateProgress(taskID, downloaded)
		} else {
			// 从头下载：清空可能存在的部分文件
			outFile.Truncate(0)
			outFile.Seek(0, io.SeekStart)
		}

		// 失败处理：可重试且未达上限时重新入队到队尾（可续传则保留部分文件与进度以便断点续传），
		// 否则标记失败并清理临时文件
		failTask := func(msg string, retryable bool) {
			urlTaskUpdated = true
			if h.failURLTask(taskID, msg, retryable) {
				if resumable && downloaded > 0 {
					_ = h.urlTaskRepo.UpdateProgress(taskID, downloaded)
				} else {
					os.Remove(uploadingPath)
				}
				return
			}
			os.Remove(uploadingPath)
		}

		getReq, reqErr := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
		if reqErr != nil {
			outFile.Close()
			failTask("创建下载请求失败", false)
			utils.Error("URL下载创建请求失败", utils.Err(reqErr))
			return
		}
		if resumable {
			getReq.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
			if dlTask.ETag != "" {
				// If-Range：资源未变返回206续传，已变返回200全量重下
				getReq.Header.Set("If-Range", dlTask.ETag)
			}
		}

		// 使用带 DNS 重绑定防护的 HTTP 客户端
		downloadCfg := utils.URLUploadConfig{
			Enabled:         appconfig.GlobalConfig.Upload.URLUpload.Enabled,
			AllowedIPRanges: appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges,
		}
		client := createSSRFProtectedHTTPClient(30*time.Minute, downloadCfg)
		getResp, getErr := client.Do(getReq)
		if getErr != nil {
			outFile.Close()
			failTask("下载请求失败: "+getErr.Error(), true)
			utils.Error("URL下载请求失败", utils.String("url", downloadURL), utils.Err(getErr))
			return
		}

		// 服务器忽略 Range（资源已变化）：从头全量重下
		if getResp.StatusCode == http.StatusOK && resumable {
			outFile.Truncate(0)
			outFile.Seek(0, io.SeekStart)
			downloaded = 0
			resumable = false
			_ = h.urlTaskRepo.UpdateProgress(taskID, 0)
		}
		if getResp.StatusCode != http.StatusOK && getResp.StatusCode != http.StatusPartialContent {
			getResp.Body.Close()
			outFile.Close()
			failTask("服务器返回异常状态码: "+strconv.Itoa(getResp.StatusCode), isRetryableHTTPStatus(getResp.StatusCode))
			utils.Error("URL下载响应状态异常", utils.Int("status", getResp.StatusCode))
			return
		}

		buf := make([]byte, 32*1024)
		var lastProgressUpdate int64
		for {
			n, readErr := getResp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := outFile.Write(buf[:n]); writeErr != nil {
					getResp.Body.Close()
					outFile.Close()
					failTask("写入文件失败", true)
					utils.Error("URL下载写入文件失败", utils.Int64("downloaded", downloaded), utils.Err(writeErr))
					return
				}
				downloaded += int64(n)
				if downloaded-lastProgressUpdate >= 1*1024*1024 {
					lastProgressUpdate = downloaded
					_ = h.urlTaskRepo.UpdateProgress(taskID, downloaded)
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				getResp.Body.Close()
				outFile.Close()
				failTask("读取响应数据失败", true)
				utils.Error("URL下载读取响应失败", utils.Int64("downloaded", downloaded), utils.Err(readErr))
				return
			}
		}
		getResp.Body.Close()
		outFile.Close()

		if fileSize > 0 && downloaded != fileSize {
			utils.Warn("URL下载大小不一致", utils.Int64("expected", fileSize), utils.Int64("actual", downloaded))
			failTask("下载文件大小不匹配", true)
			return
		}
	}

	// 以下为 HTTP 和 FTP 协议统一的文件处理逻辑
	taskErrMsg = "下载失败"
	h.finalizeURLDownload(taskID, filename, fileSize, downloaded, clientIP, uploadingPath, targetPath, sessionTargetPath, sessionTargetRoot, sessionTargetType, &urlTaskUpdated)
	// 检查 URL 任务的最终状态判断是否成功
	if finalTask, e := h.urlTaskRepo.GetByID(taskID); e == nil && finalTask.Status == models.URLDownloadStatusCompleted {
		taskSucceeded = true
		taskErrMsg = ""
	}
}

// finalizeURLDownload 完成下载后的统一处理（HTTP 和 FTP 共用）
func (h *UploadSessionHandler) finalizeURLDownload(taskID string, filename string, fileSize int64, downloaded int64, clientIP string, uploadingPath string, targetPath string, sessionTargetPath string, sessionTargetRoot string, sessionTargetType models.TargetType, urlTaskUpdated *bool) {
	if _, err := os.Stat(targetPath); err == nil {
		os.Remove(uploadingPath)
		*urlTaskUpdated = true
		h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "目标文件已存在")
		utils.Error("URL下载目标文件已存在", utils.String("target", targetPath))
		return
	}

	if err := os.Rename(uploadingPath, targetPath); err != nil {
		var linkErr *os.LinkError
		if errors.As(err, &linkErr) {
			if copyErr := utils.CopyFile(uploadingPath, targetPath); copyErr != nil {
				os.Remove(uploadingPath)
				*urlTaskUpdated = true
				h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "跨设备复制文件失败")
				utils.Error("跨设备复制文件失败", utils.Err(copyErr))
				return
			}
			os.Remove(uploadingPath)
		} else {
			os.Remove(uploadingPath)
			*urlTaskUpdated = true
			h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "重命名文件失败")
			utils.Error("URL下载重命名文件失败", utils.Err(err))
			return
		}
	}

	if sessionTargetType == models.TargetTypeTemp {
		tempCode, codeErr := h.generateTempCode()
		if codeErr != nil {
			os.Remove(targetPath)
			*urlTaskUpdated = true
			h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "生成访问码失败")
			utils.Error("URL下载生成临时访问码失败", utils.Err(codeErr))
			return
		}

		hash := xxh3.Hash([]byte(tempCode + "fuzhan-secret"))
		safeFilename := fmt.Sprintf("%016x", hash)
		tempFilePath := filepath.Join(appconfig.GlobalConfig.Storage.Temp.Path, safeFilename)

		if err := os.Rename(targetPath, tempFilePath); err != nil {
			var linkErr *os.LinkError
			if errors.As(err, &linkErr) {
				if copyErr := utils.CopyFile(targetPath, tempFilePath); copyErr != nil {
					os.Remove(targetPath)
					*urlTaskUpdated = true
					h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "移动文件失败")
					return
				}
				os.Remove(targetPath)
			} else {
				os.Remove(targetPath)
				*urlTaskUpdated = true
				h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "移动文件失败")
				return
			}
		}

		expireDays := appconfig.GlobalConfig.Storage.Temp.DefaultExpireDays
		if expireDays <= 0 {
			expireDays = 7
		}

		tempFile := &models.TempFile{
			Code:      tempCode,
			Filename:  filename,
			FileSize:  downloaded,
			FilePath:  tempFilePath,
			ClientIP:  clientIP,
			Dir:       sessionTargetPath,
			ExpiredAt: utils.Now().Add(time.Duration(expireDays) * 24 * time.Hour),
		}

		if err := h.db.Create(tempFile).Error; err != nil {
			os.Remove(tempFilePath)
			*urlTaskUpdated = true
			h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "创建临时文件记录失败")
			utils.Error("URL下载创建临时文件记录失败", utils.Err(err))
			return
		}

		// 同步到临时文件索引表
		if h.indexSvc != nil {
			now := utils.Now()
			tempRecord := models.FileRecordTemp{
				FileRecordBase: models.FileRecordBase{
					FileName:     filename,
					FilePath:     tempCode,
					RootName:     "temp",
					FullPath:     "/temp/" + tempCode,
					FileSize:     downloaded,
					IsDir:        false,
					ModTime:      now,
					Status:       models.FileStatusActive,
					OwnerID:      clientIP,
					LastSyncedAt: now,
				},
			}
			if err := h.db.Create(&tempRecord).Error; err != nil {
				utils.Warn("URL下载后同步临时文件索引失败", utils.String("code", tempCode), utils.Err(err))
			}
			// 触发哈希计算（后台执行，不阻塞）
			h.indexSvc.TriggerHash(context.Background())
		}

		targetPath = tempFilePath
	}

	relativePath := sessionTargetPath
	if relativePath == "/" || relativePath == "" {
		relativePath = filename
	} else {
		relativePath = strings.TrimSuffix(relativePath, "/")
		if !strings.HasPrefix(relativePath, "/") {
			relativePath = "/" + relativePath
		}
		relativePath = relativePath + "/" + filename
	}
	safeFilename := filepath.Base(filename)
	record := &models.OperationRecord{
		FileName:   safeFilename,
		FileSize:   downloaded,
		FilePath:   relativePath,
		FullPath:   "/" + sessionTargetRoot + "/" + strings.TrimPrefix(relativePath, "/"),
		RootName:   sessionTargetRoot,
		FileType:   pathutils.GetFileType(safeFilename),
		ClientIP:   clientIP,
		UserID:     clientIP,
		UploadType: sessionTargetType,
		UploadTime: utils.Now(),
	}
	if err := h.recordRepo.Create(record); err != nil {
		utils.Error("创建上传记录失败", utils.Err(err))
	}

	// 同步到文件索引表（公共目录）
	if h.indexSvc != nil && sessionTargetType == models.TargetTypeRegular {
		if err := h.indexSvc.SyncFile(sessionTargetRoot, relativePath); err != nil {
			utils.Warn("URL下载后同步索引失败", utils.String("root", sessionTargetRoot), utils.String("path", relativePath), utils.Err(err))
		} else {
			h.indexSvc.MarkRecentlySynced(sessionTargetRoot, relativePath)
			// 关联公共文件索引记录 ID（身份标识），移动/重命名后依然有效
			if record.ID > 0 {
				if fid, rerr := h.recordRepo.ResolvePublicFileID(sessionTargetRoot, record.FullPath); rerr == nil && fid > 0 {
					if uerr := h.recordRepo.UpdateFileRecordID(record.ID, fid); uerr != nil {
						utils.Warn("上传记录关联索引ID失败", utils.Int64("record", int64(record.ID)), utils.Err(uerr))
					}
				}
			}
		}
		// 记录上传者IP（用于"IP一致允许覆盖/重命名/删除"）
		if clientIP != "" {
			if uerr := h.db.Model(&models.FileRecordPublic{}).
				Where("root_name = ? AND file_path = ?", sessionTargetRoot, "/"+strings.TrimPrefix(relativePath, "/")).
				UpdateColumn("uploader_ip", clientIP).Error; uerr != nil {
				utils.Warn("URL下载写入上传者IP失败",
					utils.String("root", sessionTargetRoot),
					utils.String("path", relativePath),
					utils.Err(uerr))
			}
		}
		// 触发哈希计算（后台执行，不阻塞）
		h.indexSvc.TriggerHash(context.Background())
	}

	*urlTaskUpdated = true
	h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusCompleted, "")

	utils.Info("URL文件下载完成", utils.String("filename", filename), utils.Int64("size", downloaded))
}
