package auth

import (
	"net/http"
	"strconv"

	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// ApiKeyHandler API Key HTTP 处理器
type ApiKeyHandler struct {
	svc *ApiKeyService
}

// NewApiKeyHandler 创建 API Key 处理器
func NewApiKeyHandler(svc *ApiKeyService) *ApiKeyHandler {
	return &ApiKeyHandler{svc: svc}
}

// CreateApiKey 创建 API Key
// POST /api/v1/admin/api-keys
func (h *ApiKeyHandler) CreateApiKey(c *gin.Context) {
	var req CreateApiKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	resp, err := h.svc.CreateApiKey(req)
	if err != nil {
		response.HandleInternalServerError(c, "创建失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusCreated, "API Key 已创建", resp)
}

// ListApiKeys 分页查询 API Key 列表
// GET /api/v1/admin/api-keys
func (h *ApiKeyHandler) ListApiKeys(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	keys, total, err := h.svc.ListApiKeys(page, pageSize)
	if err != nil {
		response.HandleInternalServerError(c, "查询失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"keys":     keys,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// GetApiKey 获取单个 API Key
// GET /api/v1/admin/api-keys/:id
func (h *ApiKeyHandler) GetApiKey(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleBadRequest(c, "无效的 ID", nil)
		return
	}

	key, err := h.svc.GetApiKey(uint(id))
	if err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", key)
}

// UpdateApiKeyStatus 更新 API Key 状态
// PUT /api/v1/admin/api-keys/:id/status
func (h *ApiKeyHandler) UpdateApiKeyStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleBadRequest(c, "无效的 ID", nil)
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	if err := h.svc.UpdateApiKeyStatus(uint(id), req.Status); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "状态已更新", nil)
}

// DeleteApiKey 删除 API Key
// DELETE /api/v1/admin/api-keys/:id
func (h *ApiKeyHandler) DeleteApiKey(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleBadRequest(c, "无效的 ID", nil)
		return
	}

	if err := h.svc.DeleteApiKey(uint(id)); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "API Key 已删除", nil)
}
