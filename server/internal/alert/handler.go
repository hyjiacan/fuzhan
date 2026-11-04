// Package alert 提供管理端存储告警消息接口（仅管理员可访问）。
package alert

import (
	"net/http"
	"strconv"

	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 存储告警消息处理器。
type Handler struct {
	svc *services.StorageAlertService
}

// NewHandler 创建存储告警消息处理器。
func NewHandler(svc *services.StorageAlertService) *Handler {
	return &Handler{svc: svc}
}

// List GET /api/web/admin/alert-messages
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	unreadOnly := c.Query("unreadOnly") == "true" || c.Query("unreadOnly") == "1"

	items, total, err := h.svc.ListMessages(page, pageSize, unreadOnly)
	if err != nil {
		utils.Error("查询存储告警消息失败", utils.Err(err))
		response.HandleInternalServerError(c, "查询存储告警消息失败")
		return
	}
	unread, _ := h.svc.UnreadCount()
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"list":     items,
		"total":    total,
		"unread":   unread,
		"page":     page,
		"pageSize": pageSize,
	})
}

// UnreadCount GET /api/web/admin/alert-messages/unread-count
func (h *Handler) UnreadCount(c *gin.Context) {
	count, err := h.svc.UnreadCount()
	if err != nil {
		utils.Error("查询未读存储告警数量失败", utils.Err(err))
		response.HandleInternalServerError(c, "查询未读存储告警数量失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{"count": count})
}

// MarkRead POST /api/web/admin/alert-messages/read
// 支持批量标记：ids 为空且 all=true 时标记全部未读。
func (h *Handler) MarkRead(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids"`
		All bool   `json:"all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", nil)
		return
	}
	if req.All {
		if err := h.svc.MarkAllRead(); err != nil {
			utils.Error("标记全部存储告警已读失败", utils.Err(err))
			response.HandleInternalServerError(c, "标记已读失败")
			return
		}
		response.HandleSuccess(c, http.StatusOK, "", gin.H{"ok": true})
		return
	}
	for _, id := range req.IDs {
		if err := h.svc.MarkRead(id); err != nil {
			utils.Error("标记存储告警已读失败", utils.Int("id", int(id)), utils.Err(err))
		}
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{"ok": true})
}
