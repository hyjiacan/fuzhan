package openapi

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "fuzhan/pkg/response"
)

// StatsHandler API 调用统计处理器
type StatsHandler struct {
    stat *APICallStat
}

// NewStatsHandler 创建统计处理器
func NewStatsHandler(stat *APICallStat) *StatsHandler {
    return &StatsHandler{stat: stat}
}

// GetStats GET /api/v1/admin/open-api/stats
func (h *StatsHandler) GetStats(c *gin.Context) {
    summary := h.stat.GetSummary()
    response.HandleSuccess(c, http.StatusOK, "", summary)
}
