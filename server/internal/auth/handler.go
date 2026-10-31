// Package auth 处理认证业务
package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/captcha"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/security"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recordAuthEvent 将登录/注册相关风险写入安全事件库（供暴破分析与强提醒）。
func recordAuthEvent(c *gin.Context, eventType, operation, target, severity, reason string) {
	security.RecordFromGin(c, &models.SecurityEvent{
		EventType: eventType,
		Severity:  severity,
		Operation: operation,
		Target:    target,
		Reason:    reason,
		Guard:     models.GuardAuth,
	})
}

// handleLoginBlocked 输出爆破防护拦截结果
func handleLoginBlocked(c *gin.Context) {
	response.HandleError(c, http.StatusTooManyRequests, response.CodeBadRequest, "尝试过于频繁，请稍后再试", nil)
}

// Handler 认证处理器
type Handler struct {
	authService   *services.AuthService
	oauth2Service *OAuth2Service
	db            *gorm.DB
}

// NewHandler 创建认证处理器实例
func NewHandler(authService *services.AuthService, db *gorm.DB) *Handler {
	return &Handler{authService: authService, db: db}
}

// recordLoginLog 记录登录/注册事件到 login_logs（成功与失败均记录）。
func (h *Handler) recordLoginLog(c *gin.Context, userID, username, method, status, failReason string) {
	RecordLoginLog(h.db, c, userID, username, method, status, failReason)
}

// RecordLoginLog 将登录/注册事件写入 login_logs（成功与失败均记录）。
// 供 web 与 lite 两端登录/注册共用，保持个人登录记录口径一致。
func RecordLoginLog(db *gorm.DB, c *gin.Context, userID, username, method, status, failReason string) {
	if db == nil {
		return
	}
	log := &models.LoginLog{
		UserID:     userID,
		Username:   username,
		Method:     method,
		ClientIP:   utils.GetClientIP(c),
		UserAgent:  c.Request.UserAgent(),
		Status:     status,
		FailReason: failReason,
	}
	if len(log.UserAgent) > 512 {
		log.UserAgent = log.UserAgent[:512]
	}
	if err := db.Create(log).Error; err != nil {
		utils.Warn("写入登录日志失败", utils.Err(err))
	}
}

// SetOAuth2Service 注入 OAuth2 认证服务（无论初始是否启用都注入，便于配置保存后热启用）
func (h *Handler) SetOAuth2Service(svc *OAuth2Service) {
	h.oauth2Service = svc
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

// authSessionResponse 构造登录/注册/刷新成功的响应体。
// 凭证只由 HttpOnly Cookie 下发，响应体永远不回传 JWT。
func authSessionResponse(resp *services.AuthResponse) gin.H {
	return gin.H{
		"uuid":      resp.UUID,
		"username":  resp.Username,
		"expiresIn": resp.ExpiresIn,
	}
}

// verifyCaptcha 验证码门禁：无需验证码或校验通过返回 true；
// 需要但缺失/错误时写入 400 响应并返回 false。
func verifyCaptcha(c *gin.Context, id, answer string) bool {
	if !CaptchaRequired(utils.GetClientIP(c)) {
		return true
	}
	if !captcha.Verify(id, answer) {
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.captcha", utils.GetClientIP(c), models.SeverityMedium, "验证码错误")
		response.HandleBadRequest(c, "验证码错误或已过期，请重试", gin.H{"code": "captcha_invalid"})
		return false
	}
	return true
}

// GetCaptcha 生成图形验证码（匿名可拉取，路由层按 IP 限流）。
// 返回 captchaId 与可直接用于 <img src> 的 PNG data URI。
func (h *Handler) GetCaptcha(c *gin.Context) {
	id, dataURI, err := captcha.Generate()
	if err != nil {
		utils.Warn("生成图形验证码失败", utils.Err(err))
		response.HandleInternalServerError(c, "验证码生成失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"captchaId": id,
		"image":     dataURI,
	})
}

// Register 用户注册
func (h *Handler) Register(c *gin.Context) {
	var req services.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}
	if !verifyCaptcha(c, req.CaptchaID, req.CaptchaAnswer) {
		return
	}
	if isLoginLocked(utils.GetClientIP(c)) {
		middleware.LogOperation(c, "auth.register", req.Username, fmt.Errorf("注册被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.register", req.Username, models.SeverityHigh, "尝试过于频繁，注册被频率限制锁定")
		handleLoginBlocked(c)
		return
	}
	resp, err := h.authService.Register(&req)
	if err != nil {
		registerLoginFailure(utils.GetClientIP(c))
		middleware.LogOperation(c, "auth.register", req.Username, err)
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.register", req.Username, models.SeverityMedium, "注册失败: "+err.Error())
		h.recordLoginLog(c, "", req.Username, "register", "failed", err.Error())
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}
	resetLoginFailures(utils.GetClientIP(c))
	middleware.LogOperation(c, "auth.register", req.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, "register", "success", "")
	// 注册即登录（与轻量版一致）：把 JWT 写入 HttpOnly Cookie，
	// 使 Vue 与 lite 两端登录态统一，均由同源 Cookie 承载。
	middleware.SetAuthCookie(c, resp.Token)
	response.HandleSuccess(c, http.StatusCreated, "注册成功", authSessionResponse(resp))
}

// Login 用户登录
func (h *Handler) Login(c *gin.Context) {
	var req services.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}
	if !verifyCaptcha(c, req.CaptchaID, req.CaptchaAnswer) {
		return
	}
	if isLoginLocked(utils.GetClientIP(c)) {
		middleware.LogOperation(c, "auth.login", req.Username, fmt.Errorf("登录被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.login", req.Username, models.SeverityHigh, "尝试过于频繁，登录被频率限制锁定")
		handleLoginBlocked(c)
		return
	}
	resp, err := h.authService.Login(&req)
	if err != nil {
		registerLoginFailure(utils.GetClientIP(c))
		middleware.LogOperation(c, "auth.login", req.Username, err)
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.login", req.Username, models.SeverityMedium, "登录失败: "+err.Error())
		h.recordLoginLog(c, "", req.Username, "local", "failed", err.Error())
		response.HandleUnauthorized(c, err.Error())
		return
	}
	resetLoginFailures(utils.GetClientIP(c))
	middleware.LogOperation(c, "auth.login", req.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, "local", "success", "")
	// 登录成功即写 HttpOnly Cookie，统一 Vue 与 lite 两端的会话凭证。
	middleware.SetAuthCookie(c, resp.Token)
	response.HandleSuccess(c, http.StatusOK, "登录成功", authSessionResponse(resp))
}

// Logout 登出：清除 HttpOnly Cookie，同时清理前端由服务端维护的会话凭证。
// Cookie 为 HttpOnly，前端脚本无法直接删除，必须由后端清除。
func (h *Handler) Logout(c *gin.Context) {
	middleware.ClearAuthCookie(c)
	response.HandleSuccess(c, http.StatusOK, "已退出登录", nil)
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
		// 令牌被刷新时只同步 HttpOnly Cookie，不回传响应体（凭证不对脚本暴露）。
		middleware.SetAuthCookie(c, newToken)
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

// OAuth2Status 返回外部 OAuth2 认证的可用状态，供登录页决定是否展示入口
func (h *Handler) OAuth2Status(c *gin.Context) {
	cfg := appconfig.GetConfig().Auth.OAuth2
	enabled := h.oauth2Service != nil && h.oauth2Service.IsEnabled()
	name := cfg.ProviderName
	if name == "" {
		name = "OAuth2"
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"enabled":      enabled,
		"providerName": name,
	})
}

// OAuth2Authorize 生成外部认证授权地址，交由前端跳转（便于统一处理错误提示）
func (h *Handler) OAuth2Authorize(c *gin.Context) {
	if h.oauth2Service == nil || !h.oauth2Service.IsEnabled() {
		response.HandleBadRequest(c, "OAuth2 认证未启用", nil)
		return
	}
	authURL, state, err := h.oauth2Service.AuthorizeURL(h.oauth2CallbackURL(c))
	if err != nil {
		utils.Warn("生成 OAuth2 授权地址失败", utils.Err(err))
		response.HandleInternalServerError(c, "生成授权地址失败: "+err.Error())
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"authorizeUrl": authURL,
		"state":        state,
	})
}

// OAuth2Callback 处理外部认证回调：校验 state → 换取用户信息 → 映射本地用户 → 写会话 Cookie 并跳回前端
func (h *Handler) OAuth2Callback(c *gin.Context) {
	if h.oauth2Service == nil || !h.oauth2Service.IsEnabled() {
		h.redirectOAuth2Error(c, "OAuth2 认证未启用")
		return
	}
	if errMsg := strings.TrimSpace(c.Query("error")); errMsg != "" {
		desc := c.Query("error_description")
		h.redirectOAuth2Error(c, strings.TrimSpace(errMsg+" "+desc))
		return
	}
	if err := h.oauth2Service.verifyState(c.Query("state")); err != nil {
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.oauth2", "", models.SeverityMedium, "OAuth2 回调 state 校验失败: "+err.Error())
		h.redirectOAuth2Error(c, err.Error())
		return
	}

	user, err := h.oauth2Service.Authenticate(c.Query("code"), h.oauth2CallbackURL(c))
	if err != nil {
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.oauth2", "", models.SeverityMedium, "OAuth2 认证失败: "+err.Error())
		h.redirectOAuth2Error(c, err.Error())
		return
	}

	resp, err := h.authService.LoginByUser(user)
	if err != nil {
		h.redirectOAuth2Error(c, err.Error())
		return
	}
	middleware.LogOperation(c, "auth.oauth2.login", user.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, "oauth2", "success", "")

	// OAuth2 登录成功写 HttpOnly Cookie，凭证只经 Cookie 传递；
	// 跳回前端时不再把令牌放进 URL（避免残留在浏览器历史/脚本可读的 location）。
	middleware.SetAuthCookie(c, resp.Token)
	// 相对地址跳转，避免用请求 Host 拼绝对 URL（Host 头可被伪造）。
	c.Redirect(http.StatusFound, "/#/oauth2/callback")
}

// oauth2CallbackURL 计算注册到外部系统的回调地址（未显式配置时按当前请求推断）
func (h *Handler) oauth2CallbackURL(c *gin.Context) string {
	if cfg := appconfig.GetConfig().Auth.OAuth2; cfg.RedirectURL != "" {
		return cfg.RedirectURL
	}
	return h.frontendBase(c) + "/api/v1/web/auth/oauth2/callback"
}

// frontendBase 返回当前请求对应的前端访问基址（scheme://host）
func (h *Handler) frontendBase(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + c.Request.Host
}

// redirectOAuth2Error 认证失败时跳回前端并携带错误信息
func (h *Handler) redirectOAuth2Error(c *gin.Context, msg string) {
	q := url.Values{}
	q.Set("error", msg)
	// 相对地址跳转，避免用请求 Host 拼绝对 URL（Host 头可被伪造）。
	c.Redirect(http.StatusFound, "/#/oauth2/callback?"+q.Encode())
}
