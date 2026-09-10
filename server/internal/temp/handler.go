package temp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"fuzhan/internal/accessguard"
	configPkg "fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
)

// Handler 临时文件处理器
type Handler struct {
	db          *gorm.DB
	config      configPkg.TempConfig
	tempService *services.TempFileService
	indexSvc    *index.Service
	// uploadSvc 携带临时落盘与收尾策略的统一分片上传服务
	uploadSvc *services.UploadSessionService
}

// NewHandler 创建临时文件处理器
func NewHandler(db *gorm.DB, config configPkg.TempConfig, tempService *services.TempFileService, indexSvc *index.Service) *Handler {
	return &Handler{
		db: db, config: config, tempService: tempService, indexSvc: indexSvc,
		uploadSvc: services.NewUploadSessionServiceWithPolicies(
			db, configPkg.GlobalConfig.Upload.ChunkSize, indexSvc,
			NewStorage(config.Path),
			NewFinalizer(db, indexSvc, config.Path, config.DefaultExpireDays),
			models.TargetTypeTemp,
		),
	}
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

// hexCodePattern 32位十六进制访问码正则（128bit 熵，防在线枚举）
var hexCodePattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// isValidHexCode 检查是否为有效的32位十六进制访问码
func isValidHexCode(code string) bool {
	return hexCodePattern.MatchString(code)
}

// validateCode 校验分享码格式
func validateCode(code string) bool {
	return code != "" && len(code) == 32 && isValidHexCode(code)
}

// checkSessionIP 校验上传会话绑定的创建者 IP 与当前请求 IP 一致，防止凭 uploadId 窃取会话
func (h *Handler) checkSessionIP(c *gin.Context, sessionIP string) bool {
	if sessionIP == "" {
		return true
	}
	return sessionIP == h.getClientIP(c)
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

	// 下载访问限流：per-IP 频率限制 + 失败计数锁定，防在线枚举爆破
	ip := utils.GetClientIP(c)
	if !accessguard.Acquire(accessguard.SCOPE_TEMP, ip) {
		utils.HandleErrorCompat(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试", nil)
		return
	}

	code := c.Param("code")
	if !validateCode(code) {
		accessguard.Fail(accessguard.SCOPE_TEMP, ip)
		utils.HandleBadRequest(c, "无效的访问码格式", nil)
		return
	}

	tempFile, err := h.tempService.DownloadFile(code)
	if err != nil {
		accessguard.Fail(accessguard.SCOPE_TEMP, ip)
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
