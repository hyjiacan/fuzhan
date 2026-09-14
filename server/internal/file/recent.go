package file

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 合法的 action 类型
var validActions = map[string]bool{
	"upload":   true,
	"download": true,
	"search":   true,
}

func isValidAction(action string) bool {
	if action == "" {
		return true
	}
	return validActions[action]
}

// RecentHandler 最近上传处理器
type RecentHandler struct {
	recordRepo repositories.AuditStore
	db         *gorm.DB // 用于按索引表回填下载记录的文件上传时间
}

// NewRecentHandler 创建最近上传处理器实例
func NewRecentHandler(recordRepo repositories.AuditStore, db *gorm.DB) *RecentHandler {
	return &RecentHandler{recordRepo: recordRepo, db: db}
}

// GetRecent 获取最近上传记录（不限IP，用于最近上传页面）
// 支持分页参数 page 和 pageSize
func (h *RecentHandler) GetRecent(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("pageSize", "20")
	action := c.Query("action")

	// action 参数必填
	if action == "" {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "action参数不能为空", nil)
		return
	}

	if !isValidAction(action) {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "非法的action参数，有效值: upload, download, search", nil)
		return
	}

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(pageSizeStr)
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	records, total, err := h.recordRepo.ListRecentAllPaginated(page, pageSize, action)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败: "+err.Error(), nil)
		return
	}

	// 统一回填 fullPath（向后兼容历史记录，根目录名拼接）
	for i := range records {
		if records[i].FullPath == "" && records[i].RootName != "" {
			records[i].FullPath = "/" + records[i].RootName + "/" + strings.TrimPrefix(records[i].FilePath, "/")
		}
	}

	// 最近下载：回填文件真实上传时间。下载操作记录本身不含上传时间，
	// 以索引表（file_records_public）为权威来源，按 full_path 关联。
	if action == "download" && h.db != nil {
		backfillDownloadUploadTime(h.db, records)
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"records":  records,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// GetRecentCarousel 获取首页轮播的最近记录（固定5条，不限IP）
func (h *RecentHandler) GetRecentCarousel(c *gin.Context) {
	// 首页轮播默认显示上传记录
	records, err := h.recordRepo.ListRecentAll(5, "upload")
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败: "+err.Error(), nil)
		return
	}

	// 统一回填 fullPath
	for i := range records {
		if records[i].FullPath == "" && records[i].RootName != "" {
			records[i].FullPath = "/" + records[i].RootName + "/" + strings.TrimPrefix(records[i].FilePath, "/")
		}
	}

	utils.HandleSuccess(c, http.StatusOK, "", records)
}

// backfillDownloadUploadTime 按完整路径回填下载记录的文件上传时间。
// full_path 相同的多条记录共享一个哈希路径查询，结果写入各记录的真实上传时间。
func backfillDownloadUploadTime(db *gorm.DB, records []models.OperationRecord) {
	paths := make([]string, 0, len(records))
	seen := make(map[string]bool, len(records))
	for i := range records {
		p := records[i].FullPath
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return
	}
	var fres []models.FileRecordPublic
	if err := db.Where("full_path IN ? AND status = ? AND deleted_at IS NULL",
		paths, models.FileStatusActive).Find(&fres).Error; err != nil {
		return
	}
	byPath := make(map[string]time.Time, len(fres))
	for _, f := range fres {
		byPath[f.FullPath] = f.CreatedAt
	}
	for i := range records {
		if t, ok := byPath[records[i].FullPath]; ok {
			records[i].UploadTime = t
		}
	}
}
