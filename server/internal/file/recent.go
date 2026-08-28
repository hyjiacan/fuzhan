package file

import (
    "net/http"
    "strconv"
    "strings"

    "github.com/gin-gonic/gin"
    "fuzhan/internal/repositories"
    "fuzhan/internal/utils"
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
}

// NewRecentHandler 创建最近上传处理器实例
func NewRecentHandler(recordRepo repositories.AuditStore) *RecentHandler {
    return &RecentHandler{recordRepo: recordRepo}
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

    utils.HandleSuccess(c, http.StatusOK, "", gin.H{
        "records": records,
        "total":   total,
        "page":    page,
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