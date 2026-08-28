package file

import (
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    configPkg "fuzhan/internal/appconfig"
    constantsPkg "fuzhan/internal/constants"
    "fuzhan/internal/middleware"
    "fuzhan/internal/response"
    "fuzhan/internal/utils"
)

// PrivateStorageHandlers 私有存储相关处理器
type PrivateStorageHandlers struct{}

// NewPrivateStorageHandlers 创建私有存储处理器实例
func NewPrivateStorageHandlers() *PrivateStorageHandlers {
    return &PrivateStorageHandlers{}
}

// getUserUUID 从 gin context 获取用户 UUID
func (h *PrivateStorageHandlers) getUserUUID(c *gin.Context) (string, error) {
    uuidInterface, exists := c.Get(string(constantsPkg.ContextKeyUserUUID))
    if !exists {
        return "", fmt.Errorf("用户未认证")
    }
    uuid, ok := uuidInterface.(string)
    if !ok || uuid == "" {
        return "", fmt.Errorf("无效的用户UUID")
    }
    return uuid, nil
}

// isValidShareCode 检查是否为有效的8位十六进制分享码
func isValidShareCode(code string) bool {
    if code == "" || len(code) != 8 {
        return false
    }
    for _, c := range code {
        if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
            return false
        }
    }
    return true
}

// Upload 私有文件上传
func (h *PrivateStorageHandlers) Upload(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, "用户未认证")
        return
    }

    file, handler, err := c.Request.FormFile("file")
    if err != nil {
        middleware.LogOperation(c, "private.upload", "", fmt.Errorf("获取文件失败"))
        utils.HandleBadRequest(c, "获取文件失败", nil)
        return
    }
    defer file.Close()

    code, err := GenerateShareCode()
    if err != nil {
        middleware.LogOperation(c, "private.upload", "", fmt.Errorf("生成分享码失败"))
        utils.HandleInternalServerError(c, "生成分享码失败")
        return
    }

    basePath := configPkg.GlobalConfig.Storage.Private.Path
    userDir := GetPrivateUserPath(basePath, userID)
    if err := os.MkdirAll(userDir, 0755); err != nil {
        utils.HandleInternalServerError(c, "创建目录失败")
        return
    }

    fileDir := GetPrivateFileDir(basePath, userID, code)
    if err := os.MkdirAll(fileDir, 0755); err != nil {
        utils.HandleInternalServerError(c, "创建目录失败")
        return
    }

    filePath := filepath.Join(fileDir, "data")
    out, err := os.Create(filePath)
    if err != nil {
        utils.HandleInternalServerError(c, "创建文件失败")
        return
    }
    defer out.Close()

    fileSize, err := io.Copy(out, file)
    if err != nil {
        utils.HandleInternalServerError(c, "保存文件失败")
        return
    }

    meta := &PrivateFileMeta{
        Filename:   handler.Filename,
        UploadTime: time.Now(),
        FileSize:   fileSize,
        Owner:      userID,
        Code:       code,
    }

    if err := SavePrivateFileMetadata(fileDir, meta); err != nil {
        utils.HandleInternalServerError(c, "保存元数据失败")
        return
    }

    ipAddress := utils.GetRealIP(c.Request)
    response.AddUploadRecord(ipAddress, handler.Filename, filePath)
    middleware.LogOperation(c, "private.upload", handler.Filename, nil)
    utils.HandleSuccess(c, http.StatusCreated, "", gin.H{"code": code})
}

// List 私有文件列表
func (h *PrivateStorageHandlers) List(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, "用户未认证")
        return
    }

    path := c.Query("path")
    if path == "/" {
        path = ""
    }

    files, subdirs, err := ListPrivateDirectory(configPkg.GlobalConfig.Storage.Private.Path, userID, path)
    if err != nil {
        utils.Error("获取文件列表失败", utils.Err(err))
        utils.HandleInternalServerError(c, "获取文件列表失败")
        return
    }

    userUsed := GetUserUsedQuota(configPkg.GlobalConfig.Storage.Private.Path, userID)

    utils.HandleSuccess(c, http.StatusOK, "", gin.H{
        "files":    files,
        "subdirs":  subdirs,
        "quota": map[string]interface{}{"user": configPkg.GlobalConfig.Storage.Private.PerUserQuota},
        "used":  userUsed,
    })
}

// Delete 删除私有文件
func (h *PrivateStorageHandlers) Delete(c *gin.Context) {
    userID, err := h.getUserUUID(c)
    if err != nil {
        utils.HandleUnauthorized(c, "用户未认证")
        return
    }

    code := c.Param("code")
    if !isValidShareCode(code) {
        utils.HandleBadRequest(c, "无效的分享码格式", nil)
        return
    }

    fileDir, meta, err := FindPrivateFileByCode(configPkg.GlobalConfig.Storage.Private.Path, userID, code)
    if err != nil {
        middleware.LogOperation(c, "private.delete", code, fmt.Errorf("文件不存在"))
        utils.HandleNotFound(c, "文件不存在")
        return
    }

    if meta.Owner != userID {
        utils.HandleForbidden(c, "无权限删除此文件")
        return
    }

    // 安全验证：确保文件目录在用户目录范围内（防御元数据腐败/篡改）
    userBaseDir := GetPrivateUserPath(configPkg.GlobalConfig.Storage.Private.Path, userID)
    absFileDir, err := filepath.Abs(fileDir)
    if err != nil {
        utils.Error("私有文件删除: 路径解析失败", utils.Err(err), utils.String("path", fileDir))
        utils.HandleInternalServerError(c, "文件路径错误")
        return
    }
    absUserBaseDir, err := filepath.Abs(userBaseDir)
    if err != nil {
        utils.Error("私有文件删除: 用户目录解析失败", utils.Err(err))
        utils.HandleInternalServerError(c, "内部错误")
        return
    }
    if !strings.HasPrefix(absFileDir, absUserBaseDir+string(filepath.Separator)) && absFileDir != absUserBaseDir {
        utils.Error("私有文件删除: 路径越权", utils.String("fileDir", absFileDir), utils.String("userDir", absUserBaseDir))
        utils.HandleForbidden(c, "无权操作此文件")
        return
    }

    if err := os.RemoveAll(fileDir); err != nil {
        utils.HandleInternalServerError(c, "删除文件失败")
        return
    }

    middleware.LogOperation(c, "private.delete", code, nil)
    utils.HandleSuccess(c, http.StatusOK, "", nil)
    }

// Download 分享下载
func (h *PrivateStorageHandlers) Download(c *gin.Context) {
    code := c.Param("code")
    if !isValidShareCode(code) {
        utils.HandleBadRequest(c, "无效的分享码格式", nil)
        return
    }

    usersDir := filepath.Join(configPkg.GlobalConfig.Storage.Private.Path, "users")
    entries, err := os.ReadDir(usersDir)
    if err != nil {
        utils.HandleInternalServerError(c, "无法访问存储目录")
        return
    }

    var filePath string
    var meta *PrivateFileMeta

    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }

        fileDir, entryMeta, findErr := FindPrivateFileByCode(configPkg.GlobalConfig.Storage.Private.Path, entry.Name(), code)
        if findErr != nil {
            continue
        }

        meta = entryMeta
        filePath = filepath.Join(fileDir, "data")
        break
    }

    if filePath == "" || meta == nil {
        utils.HandleNotFound(c, "文件不存在")
        return
    }

    file, err := os.Open(filePath)
    if err != nil {
        utils.HandleInternalServerError(c, "无法打开文件")
        return
    }
    defer file.Close()

    // RFC 5987/RFC 6266 安全编码文件名
    safeFilename := url.PathEscape(meta.Filename)
    c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, meta.Filename, safeFilename))
    c.Header("Content-Type", "application/octet-stream")
    middleware.LogOperation(c, "private.download", meta.Filename, nil)
    if _, err := io.Copy(c.Writer, file); err != nil {
        utils.Error("文件下载失败", utils.Err(err))
    }
}