package file

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "github.com/zeebo/xxh3"
    "gorm.io/gorm"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/pkg/pathutils"
	"fuzhan/internal/utils"
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
            utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
            return
        }
        // admin 允许覆盖，通过 overwriteRequired 标记告知前端
    }

    session, err := h.service.CreateSession(&services.CreateSessionReq{
        Filename:   req.Filename,
        FileSize:   req.FileSize,
        Dir:        req.Dir,
        RootName:   req.RootName,
        TargetType: req.TargetType,
        UserID:     utils.GetClientIP(c),
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
func (h *UploadSessionHandler) CleanupExpiredSessions() error {
    return h.service.CleanupExpired()
}

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
        session := &models.UploadSession{
            FileName:    filename,
            FileSize:    fileSize,
            ChunkSize:   h.chunkSize,
            // 溢出安全: fileSize>0 已校验, 用 (fileSize-1)/chunkSize+1 求向上取整, 避免 fileSize+chunkSize-1 整数溢出为负数
            TotalChunks: int((fileSize - 1) / h.chunkSize + 1),
            Status:      models.UploadStatusInProgress,
            TargetType:  targetType,
            TargetPath:  req.Dir,
            TargetRoot:  req.RootName,
            UserID:      clientIP,
            ExpiredAt:   time.Now().Add(24 * time.Hour),
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
        Status:          models.URLDownloadStatusDownloading,
        TargetPath:      targetPath,
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

    go h.downloadFromURL(taskID, task.URL, task.FileSize, task.FileName, "", task.TargetPath, task.TargetPath, "", "", "")

    middleware.LogOperation(c, "upload.url.retry", taskID, nil)
    utils.HandleSuccess(c, http.StatusOK, "任务已重试", nil)
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
        sessions, total, err := h.service.ListTempByUser(clientIP, page, pageSize)
        if err != nil {
            utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
            return
        }
        result := make([]gin.H, 0, len(sessions))
        for _, s := range sessions {
            result = append(result, gin.H{
                "id":        s["id"],
                "fileName":  s["fileName"],
                "fileSize":  s["fileSize"],
                "status":    s["status"],
                "createdAt": s["createdAt"],
                "expiredAt": s["expiredAt"],
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

// downloadFromURL 后台下载文件并记录进度
func (h *UploadSessionHandler) downloadFromURL(taskID string, downloadURL string, fileSize int64, filename string, clientIP string, uploadingPath string, targetPath string, sessionTargetPath string, sessionTargetRoot string, sessionTargetType models.TargetType) {
    h.downloadSem <- struct{}{}
    defer func() { <-h.downloadSem }()

    // 创建任务记录（供任务管理页面展示）
    var taskRecordID uint
    if h.taskSvc != nil {
        if task, err := h.taskSvc.CreateTask("url_download", "URL下载: "+filename); err == nil {
            taskRecordID = task.ID
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
            h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "FTP 下载失败: "+err.Error())
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
            h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "FTP 下载文件大小不匹配")
            return
        }
    } else {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
        defer cancel()

        getReq, reqErr := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
        if reqErr != nil {
            outFile.Close()
            os.Remove(uploadingPath)
            urlTaskUpdated = true
            h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "创建下载请求失败")
            utils.Error("URL下载创建请求失败", utils.Err(reqErr))
            return
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
            os.Remove(uploadingPath)
            urlTaskUpdated = true
            h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "下载请求失败: "+getErr.Error())
            utils.Error("URL下载请求失败", utils.String("url", downloadURL), utils.Err(getErr))
            return
        }

        if getResp.StatusCode != http.StatusOK {
            getResp.Body.Close()
            outFile.Close()
            os.Remove(uploadingPath)
            urlTaskUpdated = true
            h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "服务器返回异常状态码: "+strconv.Itoa(getResp.StatusCode))
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
                    os.Remove(uploadingPath)
                    urlTaskUpdated = true
                    h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "写入文件失败")
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
                os.Remove(uploadingPath)
                urlTaskUpdated = true
                h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "读取响应数据失败")
                utils.Error("URL下载读取响应失败", utils.Int64("downloaded", downloaded), utils.Err(readErr))
                return
            }
        }

        getResp.Body.Close()
        outFile.Close()

        if fileSize > 0 && downloaded != fileSize {
            utils.Warn("URL下载大小不一致", utils.Int64("expected", fileSize), utils.Int64("actual", downloaded))
            os.Remove(uploadingPath)
            urlTaskUpdated = true
            h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusFailed, "下载文件大小不匹配")
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
            ExpiredAt: time.Now().Add(time.Duration(expireDays) * 24 * time.Hour),
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
            now := time.Now()
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
        UploadTime: time.Now(),
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
		}
		// 触发哈希计算（后台执行，不阻塞）
		h.indexSvc.TriggerHash(context.Background())
	}

    *urlTaskUpdated = true
    h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusCompleted, "")

    utils.Info("URL文件下载完成", utils.String("filename", filename), utils.Int64("size", downloaded))
}
