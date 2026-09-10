package file

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/accessguard"
	configPkg "fuzhan/internal/appconfig"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PrivateStorageHandlers 私有存储相关处理器
type PrivateStorageHandlers struct{}

// NewPrivateStorageHandlers 创建私有存储处理器实例
func NewPrivateStorageHandlers() *PrivateStorageHandlers {
	return &PrivateStorageHandlers{}
}

// getUserUUID 从 gin context 获取用户 UUID
func (h *PrivateStorageHandlers) getUserUUID(c *gin.Context) (string, error) {
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

// isValidShareCode 检查是否为有效的32位十六进制分享码
func isValidShareCode(code string) bool {
	if code == "" || len(code) != 32 {
		return false
	}
	for _, cc := range code {
		if !((cc >= '0' && cc <= '9') || (cc >= 'A' && cc <= 'F') || (cc >= 'a' && cc <= 'f')) {
			return false
		}
	}
	return true
}

// Upload 私有文件上传（简单上传，落真实文件名）
func (h *PrivateStorageHandlers) Upload(c *gin.Context) {
	userID, err := h.getUserUUID(c)
	if err != nil {
		utils.HandleUnauthorized(c, "用户未认证")
		return
	}

	fh, header, err := c.Request.FormFile("file")
	if err != nil {
		middleware.LogOperation(c, "private.upload", "", fmt.Errorf("获取文件失败"))
		utils.HandleBadRequest(c, "获取文件失败", nil)
		return
	}
	defer fh.Close()

	basePath := configPkg.GlobalConfig.Storage.Private.Path
	userDir := GetPrivateUserPath(basePath, userID)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		utils.HandleInternalServerError(c, "创建目录失败")
		return
	}

	// 简单上传固定落到用户根目录；名称经净化后作为真实文件名。
	cleanFilename := filepath.Base(filepath.Clean(header.Filename))
	if cleanFilename == "." || strings.Contains(cleanFilename, "..") {
		utils.HandleBadRequest(c, "无效的文件名", nil)
		return
	}
	targetPath := filepath.Join(userDir, cleanFilename)
	if _, err := os.Stat(targetPath); err == nil {
		utils.HandleErrorCompat(c, http.StatusConflict, "同名文件已存在，请使用其他名称", nil)
		return
	}

	out, err := os.Create(targetPath)
	if err != nil {
		utils.HandleInternalServerError(c, "创建文件失败")
		return
	}
	fileSize, copyErr := io.Copy(out, fh)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(targetPath)
		utils.HandleInternalServerError(c, "保存文件失败")
		return
	}
	if closeErr != nil {
		utils.HandleInternalServerError(c, "保存文件失败")
		return
	}

	code, err := GenerateShareCode()
	if err != nil {
		os.Remove(targetPath)
		utils.HandleInternalServerError(c, "生成分享码失败")
		return
	}

	now := utils.Now()
	rec := &models.FileRecordPrivate{ShareCode: code}
	rec.FileName = cleanFilename
	rec.FilePath = "/" + cleanFilename
	rec.RootName = privateRootName
	rec.FullPath = "/" + privateRootName + "/" + cleanFilename
	rec.FileSize = fileSize
	rec.IsDir = false
	rec.Xxh3Hash = ""
	rec.HashStatus = "pending"
	rec.ModTime = now
	rec.LastSyncedAt = now
	rec.Status = models.FileStatusActive
	rec.OwnerID = userID
	rec.CreatedAt = now
	rec.UpdatedAt = now
	if db := configPkg.GetDB(); db != nil {
		if err := db.Create(rec).Error; err != nil {
			os.Remove(targetPath)
			utils.HandleInternalServerError(c, "保存记录失败")
			return
		}
	}

	middleware.LogOperation(c, "private.upload", cleanFilename, nil)
	utils.HandleSuccess(c, http.StatusCreated, "", gin.H{"code": code})
}

// List 私有文件列表
func (h *PrivateStorageHandlers) List(c *gin.Context) {
	userID, err := h.getUserUUID(c)
	if err != nil {
		utils.HandleUnauthorized(c, "用户未认证")
		return
	}

	path := strings.Trim(c.Query("path"), "/")

	files, subdirs, err := ListPrivateDirectory(configPkg.GlobalConfig.Storage.Private.Path, userID, path)
	if err != nil {
		utils.Error("获取文件列表失败", utils.Err(err))
		utils.HandleInternalServerError(c, "获取文件列表失败")
		return
	}

	items := make([]gin.H, 0, len(files))
	for _, f := range files {
		items = append(items, gin.H{
			"filename":   f.FileName,
			"fileSize":   f.FileSize,
			"uploadTime": f.CreatedAt,
			"owner":      f.OwnerID,
			"code":       f.ShareCode,
		})
	}

	userUsed := GetUserUsedQuota(configPkg.GlobalConfig.Storage.Private.Path, userID)

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"files":   items,
		"subdirs": subdirs,
		"quota":   map[string]interface{}{"user": configPkg.GlobalConfig.Storage.Private.PerUserQuota},
		"used":    userUsed,
	})
}

// resolveShareCodeRecord 按分享码反查私有文件活跃记录
func resolveShareCodeRecord(db *gorm.DB, code string, upper bool) (*models.FileRecordPrivate, error) {
	if upper {
		code = strings.ToUpper(code)
	}
	var rec models.FileRecordPrivate
	if err := db.Where("share_code = ? AND status = ?", code, models.FileStatusActive).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// privateFileRealPath 由索引记录计算物理文件路径（带越界校验）
func privateFileRealPath(basePath, userID string, rec *models.FileRecordPrivate) (string, error) {
	userDir := GetPrivateUserPath(basePath, userID)
	rel := strings.TrimPrefix(rec.FilePath, "/")
	p := filepath.Join(userDir, rel)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("路径解析失败")
	}
	absRoot, err := filepath.Abs(userDir)
	if err != nil {
		return "", fmt.Errorf("根路径解析失败")
	}
	if !strings.HasPrefix(abs, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越权")
	}
	return p, nil
}

// Delete 删除私有文件（按分享码反查）
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

	db := configPkg.GetDB()
	if db == nil {
		utils.HandleInternalServerError(c, "数据库未就绪")
		return
	}
	rec, err := resolveShareCodeRecord(db, code, true)
	if err != nil {
		middleware.LogOperation(c, "private.delete", code, fmt.Errorf("文件不存在"))
		utils.HandleNotFound(c, "文件不存在")
		return
	}
	if rec.OwnerID != userID {
		utils.HandleForbidden(c, "无权限删除此文件")
		return
	}

	basePath := configPkg.GlobalConfig.Storage.Private.Path
	p, err := privateFileRealPath(basePath, rec.OwnerID, rec)
	if err != nil {
		utils.HandleForbidden(c, "无权操作此文件")
		return
	}

	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		utils.HandleInternalServerError(c, "删除文件失败")
		return
	}
	// 若父目录为空则清理，保留用户根目录
	if dir := filepath.Dir(p); dir != GetPrivateUserPath(basePath, rec.OwnerID) {
		_ = os.Remove(dir)
	}

	if err := db.Delete(rec).Error; err != nil {
		utils.Error("删除私有文件记录失败", utils.Err(err))
	}

	middleware.LogOperation(c, "private.delete", code, nil)
	utils.HandleSuccess(c, http.StatusOK, "", nil)
}

// Download 分享下载（按分享码反查，免认证）
func (h *PrivateStorageHandlers) Download(c *gin.Context) {
	// 下载访问限流：per-IP 频率限制 + 失败计数锁定，防在线枚举爆破
	ip := utils.GetClientIP(c)
	if !accessguard.Acquire(accessguard.SCOPE_SHARE, ip) {
		utils.HandleErrorCompat(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试", nil)
		return
	}

	code := c.Param("code")
	if !isValidShareCode(code) {
		accessguard.Fail(accessguard.SCOPE_SHARE, ip)
		utils.HandleBadRequest(c, "无效的分享码格式", nil)
		return
	}

	db := configPkg.GetDB()
	if db == nil {
		utils.HandleInternalServerError(c, "服务暂不可用")
		return
	}
	rec, err := resolveShareCodeRecord(db, code, true)
	if err != nil {
		accessguard.Fail(accessguard.SCOPE_SHARE, ip)
		utils.HandleNotFound(c, "文件不存在")
		return
	}

	basePath := configPkg.GlobalConfig.Storage.Private.Path
	p, err := privateFileRealPath(basePath, rec.OwnerID, rec)
	if err != nil {
		accessguard.Fail(accessguard.SCOPE_SHARE, ip)
		utils.HandleNotFound(c, "文件不存在")
		return
	}

	file, err := os.Open(p)
	if err != nil {
		utils.HandleInternalServerError(c, "无法打开文件")
		return
	}
	defer file.Close()

	// RFC 5987/RFC 6266 安全编码文件名
	safeFilename := url.PathEscape(rec.FileName)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, rec.FileName, safeFilename))
	c.Header("Content-Type", "application/octet-stream")
	middleware.LogOperation(c, "private.download", rec.FileName, nil)
	if _, err := io.Copy(c.Writer, file); err != nil {
		utils.Error("文件下载失败", utils.Err(err))
	}
}
