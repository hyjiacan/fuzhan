// Package notification 处理通知业务
package notification

import (
	"net/http"

	"fuzhan/internal/repositories"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler 通知处理器
type Handler struct {
	db   *gorm.DB
	repo *repositories.URLDownloadTaskRepository
}

// NewHandler 创建通知处理器
func NewHandler(db *gorm.DB) *Handler {
	return &Handler{
		db:   db,
		repo: repositories.NewURLDownloadTaskRepository(db),
	}
}

// GetNotifications 获取未通知的任务列表
func (h *Handler) GetNotifications(c *gin.Context) {
	userID, hasUser := c.Get("userUUID")
	anonymousID := c.GetHeader("X-Anonymous-ID")

	if hasUser {
		uid, ok := userID.(string)
		if ok && uid != "" {
			tasks, err := h.repo.ListNotNotified(&uid, nil)
			if err != nil {
				response.HandleErrorCompat(c, http.StatusInternalServerError, "查询通知失败: "+err.Error(), nil)
				return
			}
			response.HandleSuccess(c, http.StatusOK, "", tasks)
			return
		}
	}

	if anonymousID != "" {
		tasks, err := h.repo.ListNotNotified(nil, &anonymousID)
		if err != nil {
			response.HandleErrorCompat(c, http.StatusInternalServerError, "查询通知失败: "+err.Error(), nil)
			return
		}
		response.HandleSuccess(c, http.StatusOK, "", tasks)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", []interface{}{})
}

// MarkReadRequest 批量标记通知为已读请求
type MarkReadRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

// MarkRead 批量标记通知为已读
func (h *Handler) MarkRead(c *gin.Context) {
	var req MarkReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求参数错误", nil)
		return
	}

	if len(req.IDs) == 0 {
		response.HandleBadRequest(c, "ids 不能为空", nil)
		return
	}

	var userIDPtr *string
	var anonymousIDPtr *string
	if uid, exists := c.Get("userUUID"); exists {
		if s, ok := uid.(string); ok && s != "" {
			userIDPtr = &s
		}
	} else if aid := c.GetHeader("X-Anonymous-ID"); aid != "" {
		anonymousIDPtr = &aid
	}

	if err := h.repo.MarkNotified(req.IDs, userIDPtr, anonymousIDPtr); err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, "标记已读失败: "+err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", nil)
}

// MergeAnonymous 将匿名通知合并到当前用户
func (h *Handler) MergeAnonymous(c *gin.Context) {
	anonymousID := c.GetHeader("X-Anonymous-ID")
	if anonymousID == "" {
		response.HandleSuccess(c, http.StatusOK, "", nil)
		return
	}

	userID, exists := c.Get("userUUID")
	if !exists {
		response.HandleSuccess(c, http.StatusOK, "", nil)
		return
	}

	uid, ok := userID.(string)
	if !ok || uid == "" {
		response.HandleSuccess(c, http.StatusOK, "", nil)
		return
	}

	if err := h.repo.MergeAnonymousToUser(anonymousID, uid); err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, "合并通知失败: "+err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", nil)
}
