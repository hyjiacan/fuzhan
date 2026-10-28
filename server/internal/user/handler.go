// Package user 处理个人中心相关接口（登录记录、操作记录等）
package user

import (
	"net/http"
	"strconv"
	"time"

	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler 个人中心处理器
type Handler struct {
	recordRepo repositories.AuditStore
	db         *gorm.DB
}

// NewHandler 创建个人中心处理器实例
func NewHandler(recordRepo repositories.AuditStore, db *gorm.DB) *Handler {
	return &Handler{recordRepo: recordRepo, db: db}
}

// currentUser 从上下文中取当前登录用户 UUID
func currentUser(c *gin.Context) string {
	uuid, _ := c.Get(string(constants.ContextKeyUserUUID))
	s, _ := uuid.(string)
	return s
}

// pageParams 解析分页参数，返回规范化后的 page/pageSize
func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

// GetLoginLogs GET /user/login-logs 获取个人登录记录
// 当前用户（local/oauth2/register 成功）按 user_id 查询；
// 另以「当前请求 IP」兜底命中自己发出的失败尝试（失败记录无 UUID，仅带 IP）。
func (h *Handler) GetLoginLogs(c *gin.Context) {
	if h.db == nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "数据库未初始化", nil)
		return
	}
	page, pageSize := pageParams(c)
	userID := currentUser(c)
	clientIP := utils.GetClientIP(c)

	var logs []models.LoginLog
	var total int64
	q := h.db.Model(&models.LoginLog{})
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	} else {
		q = q.Where("client_ip = ?", clientIP)
	}
	q.Count(&total)
	err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询登录记录失败: "+err.Error(), nil)
		return
	}
	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"records":  logs,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// GetRecords GET /user/records 获取个人操作记录
// 私有上传记录 user_id = 用户UUID 可命中；公开上传/下载记录仅带 client_ip，
// 故取「user_id = UUID OR client_ip = 当前IP」的并集。
func (h *Handler) GetRecords(c *gin.Context) {
	page, pageSize := pageParams(c)
	userID := currentUser(c)
	clientIP := utils.GetClientIP(c)

	records, total, err := h.recordRepo.ListByUserIDOrIP(userID, clientIP, page, pageSize)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询操作记录失败: "+err.Error(), nil)
		return
	}

	// 统一回填 fullPath（向后兼容历史记录，根目录名拼接）
	for i := range records {
		if records[i].FullPath == "" && records[i].RootName != "" {
			records[i].FullPath = "/" + records[i].RootName + "/" + trimPrefixSlash(records[i].FilePath)
		}
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"records":  records,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func trimPrefixSlash(s string) string {
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	return s
}

// GetStats GET /user/stats 获取个人存储统计
// 三类文件分别取自各自数据源，保证口径与对应存储页一致：
//   - 公共文件：operation_records 中当前用户上传的 action=upload 记录
//     （公开上传 user_id 存的是客户端 IP，故按 user_id = UUID 或 client_ip = 当前IP 匹配）；
//   - 临时文件：temp_files 按当前 IP 且未过期的文件（与临时页配额同口径）；
//   - 私有文件：file_records_private 中 owner_id = 当前用户 UUID 且未删除的非目录文件。
func (h *Handler) GetStats(c *gin.Context) {
	userID := currentUser(c)
	clientIP := utils.GetClientIP(c)

	var publicFiles, publicBytes int64
	var tempFiles, tempBytes int64
	var privateFiles, privateBytes int64

	if h.db != nil {
		// 公共文件：上传动作记录，source_type 可能为 'public'（DB 默认）
		// 或历史空值，兼容两种口径
		h.db.Model(&models.OperationRecord{}).
			Where("action = ? AND (source_type = ? OR source_type = '') AND (user_id = ? OR client_ip = ?)",
				"upload", models.SourceTypePublic, userID, clientIP).
			Count(&publicFiles)
		h.db.Model(&models.OperationRecord{}).
			Where("action = ? AND (source_type = ? OR source_type = '') AND (user_id = ? OR client_ip = ?)",
				"upload", models.SourceTypePublic, userID, clientIP).
			Select("COALESCE(SUM(file_size),0)").Scan(&publicBytes)

		// 临时文件：按当前 IP 且未过期（与 TempFileService.GetQuotaUsage 同口径）
		h.db.Model(&models.TempFile{}).
			Where("client_ip = ? AND expired_at > ?", clientIP, utils.Now()).
			Count(&tempFiles)
		h.db.Model(&models.TempFile{}).
			Where("client_ip = ? AND expired_at > ?", clientIP, utils.Now()).
			Select("COALESCE(SUM(file_size),0)").Scan(&tempBytes)

		// 私有文件：owner_id = 当前用户 UUID，活跃且非目录
		if userID != "" {
			h.db.Table(models.FileRecordPrivate{}.TableName()).
				Where("owner_id = ? AND status = ? AND is_dir = ?",
					userID, models.FileStatusActive, false).
				Count(&privateFiles)
			h.db.Table(models.FileRecordPrivate{}.TableName()).
				Where("owner_id = ? AND status = ? AND is_dir = ?",
					userID, models.FileStatusActive, false).
				Select("COALESCE(SUM(file_size),0)").Scan(&privateBytes)
		}
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"publicFiles":  publicFiles,
		"publicBytes":  publicBytes,
		"tempFiles":    tempFiles,
		"tempBytes":    tempBytes,
		"privateFiles": privateFiles,
		"privateBytes": privateBytes,
		"updatedAt":    time.Now(),
	})
}
