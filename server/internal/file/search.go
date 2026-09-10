package file

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
)

// recordSearch 记录搜索操作。优先走全局有界记录器（有界 async），
// 未初始化时回退为同步写入，避免漏记。
func recordSearch(recordRepo interface {
	Create(record *models.OperationRecord) error
}, record *models.OperationRecord) {
	if !services.SubmitRecord(record) {
		if err := recordRepo.Create(record); err != nil {
			utils.Error("记录搜索操作失败", utils.Err(err))
		}
	}
}

// SearchHandlers 搜索相关处理器
type SearchHandlers struct {
	SearchService *services.SearchService
	recordRepo    interface {
		Create(record *models.OperationRecord) error
	}
}

// NewSearchHandlers 创建搜索处理器实例
func NewSearchHandlers(searchService *services.SearchService, recordRepo interface {
	Create(record *models.OperationRecord) error
}) *SearchHandlers {
	return &SearchHandlers{
		SearchService: searchService,
		recordRepo:    recordRepo,
	}
}

// SearchFiles 搜索文件（一次性返回所有结果）
func (sh *SearchHandlers) SearchFiles(c *gin.Context) {
	sh.searchFiles(c, true)
}

// AdminSearchFiles 管理页面搜索文件（不记录历史）
func (sh *SearchHandlers) AdminSearchFiles(c *gin.Context) {
	sh.searchFiles(c, false)
}

func (sh *SearchHandlers) searchFiles(c *gin.Context, record bool) {
	query := c.Param("query")
	// 去掉前导斜杠（路由 /*query 会捕获前导 /）
	query = strings.TrimPrefix(query, "/")
	// URL 解码
	query, _ = url.QueryUnescape(query)
	if len(query) > 200 {
		query = strings.ToValidUTF8(query[:200], "")
	}
	utils.Info("搜索请求", utils.String("query", query))

	// 记录搜索操作
	if record && sh.recordRepo != nil && query != "" {
		recordSearch(sh.recordRepo, &models.OperationRecord{
			Action:      "search",
			SearchQuery: query,
			ClientIP:    utils.GetRealIP(c.Request),
			CreatedAt:   utils.Now(),
		})
	}

	results, err := sh.SearchService.SearchFiles(query, appconfig.GlobalConfig.Storage.Public.RootDirs, 30*time.Second)
	if err != nil {
		middleware.LogOperation(c, "search", query, err)
		utils.HandleError(c, http.StatusInternalServerError, 500, "搜索失败", err.Error())
		return
	}

	middleware.LogOperation(c, "search", query, nil)
	utils.HandleSuccess(c, http.StatusOK, "", results)
}
