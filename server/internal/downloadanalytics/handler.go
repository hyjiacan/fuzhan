package downloadanalytics

import (
	"net/http"
	"strconv"
	"time"

	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 下载行为分析处理器
type Handler struct {
	service *Service
}

// NewHandler 创建下载行为分析处理器
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// parseRange 解析 from/to 查询参数（RFC3339），缺省为零值交由服务层归一化
func parseRange(c *gin.Context) (time.Time, time.Time) {
	var from, to time.Time
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	return from, to
}

func intQuery(c *gin.Context, key string, def, max int) int {
	v := def
	if s := c.Query(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			v = n
		}
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}

// SummaryHandle GET /admin/download-analytics/summary
func (h *Handler) SummaryHandle(c *gin.Context) {
	from, to := parseRange(c)
	res, err := h.service.Summary(from, to)
	if err != nil {
		utils.Warn("下载行为分析-summary查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取下载总览失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// TrendHandle GET /admin/download-analytics/trend?granularity=&id=
func (h *Handler) TrendHandle(c *gin.Context) {
	from, to := parseRange(c)
	granularity := "day"
	if v := c.Query("granularity"); v == "week" || v == "month" {
		granularity = v
	}
	id, _ := strconv.ParseUint(c.Query("id"), 10, 64)
	res, err := h.service.TrendPoints(from, to, granularity, uint(id))
	if err != nil {
		utils.Warn("下载行为分析-trend查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取下载趋势失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// TopFilesHandle GET /admin/download-analytics/top-files?sort=count|spread
func (h *Handler) TopFilesHandle(c *gin.Context) {
	from, to := parseRange(c)
	limit := intQuery(c, "limit", 10, 100)
	res, err := h.service.TopFiles(from, to, c.Query("sort"), limit)
	if err != nil {
		utils.Warn("下载行为分析-top-files查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取热门下载文件失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// SourcesHandle GET /admin/download-analytics/sources
func (h *Handler) SourcesHandle(c *gin.Context) {
	from, to := parseRange(c)
	by := c.Query("by")
	limit := intQuery(c, "limit", 10, 100)
	res, err := h.service.Sources(from, to, by, limit)
	if err != nil {
		utils.Warn("下载行为分析-sources查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取来源分布失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// FailuresHandle GET /admin/download-analytics/failures
func (h *Handler) FailuresHandle(c *gin.Context) {
	from, to := parseRange(c)
	limit := intQuery(c, "limit", 10, 100)
	res, err := h.service.FailureReasons(from, to, limit)
	if err != nil {
		utils.Warn("下载行为分析-failures查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取失败情况失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// FileDetailHandle GET /admin/download-analytics/file?id=&from=&to=
func (h *Handler) FileDetailHandle(c *gin.Context) {
	id, err := strconv.ParseUint(c.Query("id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleBadRequest(c, "无效的文件ID", nil)
		return
	}
	from, to := parseRange(c)
	res, err := h.service.FileDetail(uint(id), from, to)
	if err != nil {
		utils.Warn("下载行为分析-file明细查询失败", utils.Int64("id", int64(id)), utils.Err(err))
		response.HandleInternalServerError(c, "获取文件明细失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// HeatmapHandle GET /admin/download-analytics/heatmap
func (h *Handler) HeatmapHandle(c *gin.Context) {
	from, to := parseRange(c)
	res, err := h.service.Heatmap(from, to)
	if err != nil {
		utils.Warn("下载行为分析-heatmap查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取时段热度失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// AggregateHandle GET /admin/download-analytics/aggregate?dimension=dir|type
func (h *Handler) AggregateHandle(c *gin.Context) {
	from, to := parseRange(c)
	limit := intQuery(c, "limit", 10, 100)
	res, err := h.service.Aggregate(from, to, c.Query("dimension"), limit)
	if err != nil {
		utils.Warn("下载行为分析-aggregate查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取目录/类型聚合失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}

// LifecycleHandle GET /admin/download-analytics/lifecycle
func (h *Handler) LifecycleHandle(c *gin.Context) {
	from, to := parseRange(c)
	capDays := intQuery(c, "capDays", 30, 90)
	res, err := h.service.Lifecycle(from, to, capDays)
	if err != nil {
		utils.Warn("下载行为分析-lifecycle查询失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取生命周期/衰减曲线失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", res)
}
