// Package auth 处理认证业务
package auth

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "fuzhan/internal/middleware"
    "fuzhan/pkg/response"
    "fuzhan/internal/services"
    "fuzhan/internal/utils"
)

// Handler 认证处理器
type Handler struct {
    authService *services.AuthService
}

// NewHandler 创建认证处理器实例
func NewHandler(authService *services.AuthService) *Handler {
    return &Handler{authService: authService}
}

// RegisterRequest 注册请求结构体
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=32"`
    Password string `json:"password" binding:"required,min=6,max=128"`
}

// LoginRequest 登录请求结构体
type LoginRequest struct {
    Username string `json:"username" binding:"required,max=64"`
    Password string `json:"password" binding:"required,max=128"`
}

// AuthResponse 认证响应结构体
type AuthResponse struct {
    Token     string `json:"token"`
    UUID      string `json:"uuid"`
    Username  string `json:"username"`
    ExpiresIn int    `json:"expiresIn"`
}

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
    var req RegisterRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求数据格式错误", err.Error())
        return
    }
    resp, err := h.authService.Register(&services.RegisterRequest{
        Username: req.Username,
        Password: req.Password,
    })
    if err != nil {
        middleware.LogOperation(c, "auth.register", req.Username, err)
        response.HandleBadRequest(c, err.Error(), nil)
        return
    }
    middleware.LogOperation(c, "auth.register", req.Username, nil)
    response.HandleSuccess(c, http.StatusCreated, "注册成功", AuthResponse{
        Token: resp.Token, UUID: resp.UUID, Username: resp.Username, ExpiresIn: 86400,
    })
}

// Login 用户登录
func (h *Handler) Login(c *gin.Context) {
    var req LoginRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求数据格式错误", err.Error())
        return
    }
    resp, err := h.authService.Login(&services.LoginRequest{
        Username: req.Username,
        Password: req.Password,
    })
    if err != nil {
        middleware.LogOperation(c, "auth.login", req.Username, err)
        response.HandleUnauthorized(c, err.Error())
        return
    }
    middleware.LogOperation(c, "auth.login", req.Username, nil)
    response.HandleSuccess(c, http.StatusOK, "登录成功", AuthResponse{
        Token: resp.Token, UUID: resp.UUID, Username: resp.Username, ExpiresIn: 86400,
    })
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
