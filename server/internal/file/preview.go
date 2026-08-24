package file

import (
    "io"
    "mime"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strconv"
    "strings"

    "github.com/gin-gonic/gin"
    "fuzhan/internal/appconfig"
    "fuzhan/pkg/response"
    "fuzhan/internal/utils"
)

// ReloadPreviewConfig 重新加载预览配置（配置更新后调用）
func ReloadPreviewConfig() {
    cfg := appconfig.GetConfig()
    utils.InitPreviewConfig(cfg.Preview.AllowMimes, cfg.Preview.AllowExts)
}

// PreviewHandler 文件预览处理器
type PreviewHandler struct {
    BaseHandler *BaseHandler
}

// NewPreviewHandler 创建预览处理器实例
func NewPreviewHandler(baseHandler *BaseHandler) *PreviewHandler {
    cfg := appconfig.GetConfig()
    utils.InitPreviewConfig(cfg.Preview.AllowMimes, cfg.Preview.AllowExts)
    return &PreviewHandler{
        BaseHandler: baseHandler,
    }
}

// PreviewFile 预览文件
func (h *PreviewHandler) PreviewFile(c *gin.Context) {
    filePath := c.Param("path")
    filePath, _ = url.QueryUnescape(filePath)
    filePath = strings.TrimPrefix(filePath, "/")

    parts := strings.SplitN(filePath, "/", 2)
    rootName := parts[0]
    if len(parts) < 2 {
        response.HandleBadRequest(c, "无效的文件路径", nil)
        return
    }

    validator := utils.NewPathValidatorWithRoots(rootName, appconfig.RootNames)
    if validator == nil {
        response.HandleBadRequest(c, "无效的根目录", nil)
        return
    }

    fullPath := validator.Join(parts[1])

    if err := validator.Validate(fullPath); err != nil {
        utils.HandleForbidden(c, "路径越权访问")
        return
    }

    info, err := os.Stat(fullPath)
    if err != nil || info.IsDir() {
        response.HandleBadRequest(c, "文件不存在: "+fullPath, nil)
        return
    }

    ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fullPath), "."))

    mimeType := mime.TypeByExtension(fullPath)
    if mimeType == "" || mimeType == "application/octet-stream" {
        mimeType = utils.GetMimeType(ext)
    }

    isPreviewable := utils.IsPreviewable(fullPath, mimeType)

    result := gin.H{
        "name":        filepath.Base(fullPath),
        "path":        filePath,
        "size":        info.Size(),
        "mimeType":    mimeType,
        "previewable": isPreviewable,
    }

    if isPreviewable {
        isImageExt := utils.IsImageExtension(ext)
        isTextExt := utils.IsTextMime(mimeType) || (utils.IsTextExtension(ext) && !isImageExt)

        if isTextExt {
            cfg := appconfig.GetConfig()
            maxInlineSize, _ := appconfig.ParseQuotaString(cfg.Preview.MaxInlineSize)
            // 安全上限：防止超大文本文件导致 OOM
            const maxInlineSizeSafetyLimit int64 = 50 * 1024 * 1024 // 50MB
            if maxInlineSize <= 0 || maxInlineSize > maxInlineSizeSafetyLimit {
                maxInlineSize = maxInlineSizeSafetyLimit
            }
            textChunkSize, _ := appconfig.ParseQuotaString(cfg.Preview.TextChunkSize)
            if textChunkSize <= 0 {
                textChunkSize = 100 * 1024
            }
            if info.Size() <= maxInlineSize {
                content, err := os.ReadFile(fullPath)
                if err != nil {
                    utils.Error("读取文件失败", utils.String("path", fullPath), utils.Err(err))
                    response.HandleBadRequest(c, "读取文件失败", nil)
                    return
                }
                result["content"] = string(content)
                result["encoding"] = "utf-8"
            }
            result["chunked"] = info.Size() > maxInlineSize
            result["chunkSize"] = textChunkSize
            result["totalChunks"] = (info.Size() + textChunkSize - 1) / textChunkSize
        } else {
            result["fileUrl"] = "/download/" + filePath
        }
    }

    response.HandleSuccess(c, http.StatusOK, "", result)
}

// GetChunk 读取文件指定分块
func (h *PreviewHandler) GetChunk(c *gin.Context) {
    filePath := c.Param("path")
    filePath, _ = url.QueryUnescape(filePath)
    filePath = strings.TrimPrefix(filePath, "/")

    // 解析分块索引
    chunkStr := c.Query("chunk")
    chunkIndex, err := strconv.Atoi(chunkStr)
    if err != nil || chunkIndex < 0 {
        response.HandleBadRequest(c, "无效的分块索引", nil)
        return
    }

    parts := strings.SplitN(filePath, "/", 2)
    rootName := parts[0]
    if len(parts) < 2 {
        response.HandleBadRequest(c, "无效的文件路径", nil)
        return
    }

    validator := utils.NewPathValidatorWithRoots(rootName, appconfig.RootNames)
    if validator == nil {
        response.HandleBadRequest(c, "无效的根目录", nil)
        return
    }

    fullPath := validator.Join(parts[1])

    if err := validator.Validate(fullPath); err != nil {
        utils.HandleForbidden(c, "路径越权访问")
        return
    }

    info, err := os.Stat(fullPath)
    if err != nil || info.IsDir() {
        response.HandleBadRequest(c, "文件不存在: "+fullPath, nil)
        return
    }

    cfg := appconfig.GetConfig()
    textChunkSize, _ := appconfig.ParseQuotaString(cfg.Preview.TextChunkSize)
    if textChunkSize <= 0 {
        textChunkSize = 100 * 1024
    }

    totalChunks := int((info.Size() + textChunkSize - 1) / textChunkSize)
    if chunkIndex >= totalChunks {
        response.HandleBadRequest(c, "分块索引超出范围", nil)
        return
    }

    // 打开文件
    f, err := os.Open(fullPath)
    if err != nil {
        utils.Error("打开文件失败", utils.String("path", fullPath), utils.Err(err))
        response.HandleBadRequest(c, "读取文件失败", nil)
        return
    }
    defer f.Close()

    // 计算偏移量和读取大小
    offset := int64(chunkIndex) * textChunkSize
    readLen := int(textChunkSize)
    if offset+textChunkSize > info.Size() {
        readLen = int(info.Size() - offset)
    }

    // 跳转到指定位置
    if _, err := f.Seek(offset, io.SeekStart); err != nil {
        utils.Error("Seek文件失败", utils.String("path", fullPath), utils.Err(err))
        response.HandleBadRequest(c, "读取文件失败", nil)
        return
    }

    // 读取分块内容
    buf := make([]byte, readLen)
    n, err := f.Read(buf)
    if err != nil && err != io.EOF {
        utils.Error("读取文件失败", utils.String("path", fullPath), utils.Err(err))
        response.HandleBadRequest(c, "读取文件失败", nil)
        return
    }

    result := gin.H{
        "content":     string(buf[:n]),
        "chunkIndex":  chunkIndex,
        "chunkSize":   textChunkSize,
        "totalChunks": totalChunks,
        "startOffset": offset,
        "endOffset":   offset + int64(n),
    }

    response.HandleSuccess(c, http.StatusOK, "", result)
}

