// Package monitor 处理系统监控
package monitor

import (
	"net/http"
	"strconv"

	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 系统监测处理器
type Handler struct {
	monitorService *services.MonitorService
}

// NewHandler 创建监测处理器
func NewHandler(monitorService *services.MonitorService) *Handler {
	return &Handler{
		monitorService: monitorService,
	}
}

// HotDownloads 获取热门下载文件
func (h *Handler) HotDownloads(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	result, err := h.monitorService.GetHotDownloads(page, pageSize)
	if err != nil {
		utils.Warn("获取热门下载统计失败", utils.Err(err))
		response.HandleSuccess(c, http.StatusOK, "", gin.H{
			"records":  []interface{}{},
			"total":    0,
			"page":     page,
			"pageSize": pageSize,
		})
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", result)
}

// Storage 获取存储统计
func (h *Handler) Storage(c *gin.Context) {
	stats, err := h.monitorService.GetStorageStats()
	if err != nil {
		response.HandleInternalServerError(c, "获取存储统计失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", stats)
}

// Access 获取访问统计
func (h *Handler) Access(c *gin.Context) {
	stats, err := h.monitorService.GetAccessStats()
	if err != nil {
		response.HandleInternalServerError(c, "获取访问统计失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", stats)
}

// Keywords 获取常用关键词
func (h *Handler) Keywords(c *gin.Context) {
	limit := 10
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	result, err := h.monitorService.GetKeywords(limit)
	if err != nil {
		response.HandleInternalServerError(c, "获取关键词统计失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", result)
}

// RecentKeywords 获取最近关键词
func (h *Handler) RecentKeywords(c *gin.Context) {
	result, err := h.monitorService.GetRecentKeywords()
	if err != nil {
		response.HandleInternalServerError(c, "获取最近关键词失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", result)
}

// Rankings 获取排行榜
func (h *Handler) Rankings(c *gin.Context) {
	limit := 10
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	result, err := h.monitorService.GetRankings(limit)
	if err != nil {
		response.HandleInternalServerError(c, "获取排行榜失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", result)
}
