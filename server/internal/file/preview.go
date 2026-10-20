package file

import (
	"encoding/hex"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// ReloadPreviewConfig 重新加载预览配置（配置更新后调用）
func ReloadPreviewConfig() {
	cfg := appconfig.GetConfig()
	utils.InitPreviewConfig(cfg.Preview.InitConfig())
	utils.InitMimeTable(cfg.MimeTable)
}

// PreviewHandler 文件预览处理器
type PreviewHandler struct {
	BaseHandler *BaseHandler
}

// NewPreviewHandler 创建预览处理器实例
func NewPreviewHandler(baseHandler *BaseHandler) *PreviewHandler {
	cfg := appconfig.GetConfig()
	utils.InitPreviewConfig(cfg.Preview.InitConfig())
	utils.InitMimeTable(cfg.MimeTable)
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

	// 预览 MIME 识别优先级：用户自定义表(设置页「MIME 类型」可配) > Go 内置表 > 内置兜底表，
	// 使配置的「扩展名 -> MIME」能生效于预览识别与元信息展示
	mimeType := utils.GetPreviewMimeType(ext)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(fullPath)
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = utils.GetMimeType(ext)
		}
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
		// 按内置分组分发预览方式：文本组走内容/分块返回，其余组（图片/音频/视频/文档）走内联 URL；
		// 未归入已启用内置分组的（仅自定义组命中）按 MIME 前缀兜底判断是否为文本
		group := utils.ClassifyPreviewGroup(ext, mimeType)
		isTextExt := group == "text" || (group == "" && strings.HasPrefix(strings.ToLower(mimeType), "text/"))

		if isTextExt {
			cfg := appconfig.GetConfig()
			maxInlineSize, _ := appconfig.ParseQuotaString(cfg.Preview.TextMaxInlineSize())
			// 安全上限：防止超大文本文件导致 OOM
			const maxInlineSizeSafetyLimit int64 = 50 * 1024 * 1024 // 50MB
			if maxInlineSize <= 0 || maxInlineSize > maxInlineSizeSafetyLimit {
				maxInlineSize = maxInlineSizeSafetyLimit
			}
			textChunkSize, _ := appconfig.ParseQuotaString(cfg.Preview.TextChunkSize())
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
			// 图片/PDF/音频/视频内联预览走下载接口但标记 preview=true：既排除内置浏览器预览器的
			// 预取/缩略图请求，也不会累加下载计数或写入下载历史（见 download_service.go）；
			// 音视频播放依赖下载接口的 Range/206 支持（http.ServeFile 原生提供）
			result["fileUrl"] = "/download/" + filePath + "?preview=true"
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
	textChunkSize, _ := appconfig.ParseQuotaString(cfg.Preview.TextChunkSize())
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

// FileChecksumRequest 计算文件校验值请求：
// - public：需 rootName + path（相对根目录的路径）
// - private / temp：需 code（私有分享码 / 临时访问码）
// algorithms 为 md5/sha1/sha256 的任意子集
type FileChecksumRequest struct {
	StorageType string   `json:"storageType"`
	RootName    string   `json:"rootName"`
	Path        string   `json:"path"`
	Code        string   `json:"code"`
	Algorithms  []string `json:"algorithms" binding:"required"`
}

// ComputeFileChecksum 计算已上传文件在服务器上的校验值（md5/sha1/sha256，可多选）。
// 与 URL 上传任务的校验值能力一致：文件已落盘在服务器，按所选算法单次流式读取计算，
// 结果仅返回给调用方，不落库。支持 public/private/temp 三种存储。
func (h *PreviewHandler) ComputeFileChecksum(c *gin.Context) {
	var req FileChecksumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}
	if len(req.Algorithms) == 0 {
		response.HandleBadRequest(c, "请至少选择一种校验算法", nil)
		return
	}

	fullPath, ok := resolveStoredFilePath(c, &req)
	if !ok {
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		response.HandleBadRequest(c, "文件不存在", nil)
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
			response.HandleBadRequest(c, "不支持的校验算法: "+algo, nil)
			return
		}
		hs := newHash()
		hashers[name] = hs
		writers = append(writers, hs)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		response.HandleBadRequest(c, "读取文件失败", nil)
		return
	}
	defer file.Close()

	if _, err := io.Copy(io.MultiWriter(writers...), file); err != nil {
		response.HandleError(c, http.StatusInternalServerError, 500, "计算校验值失败: "+err.Error(), nil)
		return
	}

	checksums := make(map[string]string, len(hashers))
	for name, hs := range hashers {
		checksums[name] = hex.EncodeToString(hs.Sum(nil))
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{"checksums": checksums})
}

// resolveStoredFilePath 依据存储类型把请求解析为服务器上的物理文件路径。
// 解析失败时已写入响应，返回 ok=false。
func resolveStoredFilePath(c *gin.Context, req *FileChecksumRequest) (string, bool) {
	switch req.StorageType {
	case "private":
		if !isValidShareCode(req.Code) {
			response.HandleBadRequest(c, "无效的分享码格式", nil)
			return "", false
		}
		db := appconfig.GetDB()
		if db == nil {
			response.HandleError(c, http.StatusInternalServerError, 500, "服务暂不可用", nil)
			return "", false
		}
		rec, err := resolveShareCodeRecord(db, req.Code, true)
		if err != nil {
			response.HandleNotFound(c, "文件不存在")
			return "", false
		}
		basePath := appconfig.GlobalConfig.Storage.Private.Path
		p, err := privateFileRealPath(basePath, rec.OwnerID, rec)
		if err != nil {
			utils.HandleForbidden(c, "无权访问此文件")
			return "", false
		}
		return p, true

	case "temp":
		db := appconfig.GetDB()
		if db == nil {
			response.HandleError(c, http.StatusInternalServerError, 500, "服务暂不可用", nil)
			return "", false
		}
		var tf models.TempFile
		if err := db.Where("code = ?", strings.ToUpper(strings.TrimSpace(req.Code))).First(&tf).Error; err != nil {
			response.HandleNotFound(c, "文件不存在")
			return "", false
		}
		if utils.Now().After(tf.ExpiredAt) {
			response.HandleBadRequest(c, "文件已过期", nil)
			return "", false
		}
		basePath := appconfig.GlobalConfig.Storage.Temp.Path
		if basePath == "" || !isPathUnderRoot(basePath, tf.FilePath) {
			utils.HandleForbidden(c, "无权访问此文件")
			return "", false
		}
		return tf.FilePath, true

	default: // public
		if req.RootName == "" {
			response.HandleBadRequest(c, "缺少根目录", nil)
			return "", false
		}
		validator := utils.NewPathValidatorWithRoots(req.RootName, appconfig.RootNames)
		if validator == nil {
			response.HandleBadRequest(c, "无效的根目录", nil)
			return "", false
		}
		fullPath := validator.Join(strings.TrimPrefix(req.Path, "/"))
		if err := validator.Validate(fullPath); err != nil {
			utils.HandleForbidden(c, "路径越权访问")
			return "", false
		}
		return fullPath, true
	}
}

// isPathUnderRoot 判断物理路径是否位于指定根目录内（防越界）。
func isPathUnderRoot(root, p string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	if absPath == absRoot {
		return true
	}
	return strings.HasPrefix(absPath, absRoot+string(os.PathSeparator))
}
