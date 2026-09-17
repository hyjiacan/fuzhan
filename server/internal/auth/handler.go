// Package auth 处理认证业务
package auth

import (
	"net/http"

	"fuzhan/internal/middleware"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// handleLoginBlocked 输出爆破防护拦截结果
func handleLoginBlocked(c *gin.Context) {
	response.HandleError(c, http.StatusTooManyRequests, response.CodeBadRequest, "尝试过于频繁，请稍后再试", nil)
}

// Handler 认证处理器
type Handler struct {
	authService *services.AuthService
}

// NewHandler 创建认证处理器实例
func NewHandler(authService *services.AuthService) *Handler {
	return &Handler{authService: authService}
}

// refreshTokenIfNeeded 需要时刷新前端持有的 JWT 令牌，返回新令牌（无变化时为空串）
func (h *Handler) refreshTokenIfNeeded(c *gin.Context) string {
	newToken, err := h.authService.RefreshTokenIfNeeded(c)
	if err != nil {
		utils.Warn("刷新token失败", utils.Err(err))
		return ""
	}
	return newToken
}

// Register 用户注册
func (h *Handler) Register(c *gin.Context) {
	var req services.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}
	if isLoginLocked(utils.GetClientIP(c)) {
		middleware.LogOperation(c, "auth.register", req.Username, nil)
		handleLoginBlocked(c)
		return
	}
	resp, err := h.authService.Register(&req)
	if err != nil {
		registerLoginFailure(utils.GetClientIP(c))
		middleware.LogOperation(c, "auth.register", req.Username, err)
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}
	resetLoginFailures(utils.GetClientIP(c))
	middleware.LogOperation(c, "auth.register", req.Username, nil)
	response.HandleSuccess(c, http.StatusCreated, "注册成功", resp)
}

// Login 用户登录
func (h *Handler) Login(c *gin.Context) {
	var req services.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}
	if isLoginLocked(utils.GetClientIP(c)) {
		middleware.LogOperation(c, "auth.login", req.Username, nil)
		handleLoginBlocked(c)
		return
	}
	resp, err := h.authService.Login(&req)
	if err != nil {
		registerLoginFailure(utils.GetClientIP(c))
		middleware.LogOperation(c, "auth.login", req.Username, err)
		response.HandleUnauthorized(c, err.Error())
		return
	}
	resetLoginFailures(utils.GetClientIP(c))
	middleware.LogOperation(c, "auth.login", req.Username, nil)
	response.HandleSuccess(c, http.StatusOK, "登录成功", resp)
}

// GetCurrentUser 获取当前用户信息
func (h *Handler) GetCurrentUser(c *gin.Context) {
	user, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, err.Error())
		return
	}
	newToken := h.refreshTokenIfNeeded(c)
	data := gin.H{
		"uuid": user.UUID, "username": user.Username,
		"role": user.Role, "createdAt": user.CreatedAt,
	}
	if newToken != "" {
		data["refreshedToken"] = newToken
	}
	response.HandleSuccess(c, http.StatusOK, "", data)
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required,min=6"`
}

// ChangePassword 修改用户密码
func (h *Handler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	user, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, err.Error())
		return
	}

	if err := h.authService.ChangePassword(user.UUID, req.OldPassword, req.NewPassword); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	middleware.LogOperation(c, "auth.change_password", user.Username, nil)
	response.HandleSuccess(c, http.StatusOK, "密码修改成功", nil)
}
