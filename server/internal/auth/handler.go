// Package auth 处理认证业务
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// recordAuthFailure 记录一次认证失败：单次失败只进登录日志，
// 仅当命中频率/跨源/高价值身份等场景时才升级为安全事件。
func (h *Handler) recordAuthFailure(c *gin.Context, operation, username string, highValue bool) {
	decision := security.NoteLoginFailure(utils.GetClientIP(c), username, c.Request.UserAgent(), highValue)
	if decision == nil {
		return
	}
	recordAuthEvent(c, decision.EventType, operation, username, decision.Severity, decision.Reason)
}

// recordAuthSuccess 登录成功后检查「多次失败后从不同来源成功」的可疑登录场景。
func recordAuthSuccess(c *gin.Context, username string) {
	decision := security.NoteLoginSuccess(utils.GetClientIP(c), username, c.Request.UserAgent())
	if decision == nil {
		return
	}
	recordAuthEvent(c, decision.EventType, "auth.login", username, decision.Severity, decision.Reason)
}

// handleLoginBlocked 输出爆破防护拦截结果
func handleLoginBlocked(c *gin.Context) {
	response.HandleError(c, http.StatusTooManyRequests, response.CodeBadRequest, "尝试过于频繁，请稍后再试", nil)
}

// Handler 认证处理器
type Handler struct {
	authService     *services.AuthService
	oauth2Service   *OAuth2Service
	ldapService     ldapCredentialVerifier
	identityConfirm *services.IdentityConfirmService
	db              *gorm.DB
}

// NewHandler 创建认证处理器实例
func NewHandler(authService *services.AuthService, db *gorm.DB) *Handler {
	return &Handler{
		authService:     authService,
		db:              db,
		identityConfirm: services.NewIdentityConfirmService(),
	}
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

// SetLDAPService 注入 LDAP 认证服务（用于个人设置自助绑定目录账号）
func (h *Handler) SetLDAPService(svc ldapCredentialVerifier) {
	h.ldapService = svc
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
		"uuid":                       resp.UUID,
		"username":                   resp.Username,
		"display_name":               resp.DisplayName,
		"nickname":                   resp.Nickname,
		"expires_in":                 resp.ExpiresIn,
		"identity_notice_pending":    resp.IdentityNoticePending,
		"identity_notice_provider":   resp.IdentityNoticeProvider,
		"identity_conflict_pending":  resp.IdentityConflictPending,
		"identity_conflict_provider": resp.IdentityConflictProvider,
	}
}

// ldapDirectoryLookup 目录同名查询能力（凭据校验之外的扩展能力，按需断言）。
type ldapDirectoryLookup interface {
	LookupDirectoryUser(username string) (dn, displayName string, err error)
}

// directoryConflictProbeTimeout 本地账号登录后探测目录同名账号的最长等待时间：
// 目录不可达或响应慢时直接跳过提示，避免拖慢本地登录。
const directoryConflictProbeTimeout = 3 * time.Second

// detectDirectoryUsernameConflict 本地账号登录成功后，尽力探测目录中是否存在同名账号。
// 仅用于提示：探测失败、超时一律视为无冲突，不阻断登录、不写冲突记录。
// 管理员已裁决过（bound/ignored）的冲突不再提示，避免每次登录重复打扰。
func (h *Handler) detectDirectoryUsernameConflict(resp *services.AuthResponse, method string) {
	if resp == nil || method != services.LoginMethodLocal || h.ldapService == nil || !h.ldapService.IsEnabled() {
		return
	}
	lookup, ok := h.ldapService.(ldapDirectoryLookup)
	if !ok {
		return
	}
	if h.conflictAlreadyResolved(resp.Username) {
		return
	}
	hit := make(chan struct{}, 1)
	go func() {
		if _, _, err := lookup.LookupDirectoryUser(resp.Username); err != nil {
			utils.Debug("目录同名账号探测未命中，跳过冲突提示",
				utils.String("username", resp.Username), utils.Err(err))
			return
		}
		hit <- struct{}{}
	}()
	select {
	case <-hit:
		resp.IdentityConflictPending = true
		resp.IdentityConflictProvider = services.LDAPProviderLabel()
	case <-time.After(directoryConflictProbeTimeout):
		utils.Info("目录同名账号探测超时，跳过冲突提示", utils.String("username", resp.Username))
	}
}

// conflictAlreadyResolved 该用户名的 LDAP 同名冲突是否已被管理员裁决（绑定或忽略）。
func (h *Handler) conflictAlreadyResolved(username string) bool {
	if h.db == nil {
		return false
	}
	var count int64
	if err := h.db.Model(&models.IdentityConflict{}).
		Where("username = ? AND auth_type = ? AND resolution <> ''", username, models.AuthSourceLDAP).
		Count(&count).Error; err != nil {
		utils.Warn("查询身份冲突裁决结果失败", utils.String("username", username), utils.Err(err))
		return false
	}
	return count > 0
}

// verifyCaptcha 验证码门禁：无需验证码或校验通过返回 true；
// 需要但缺失/错误时写入 400 响应并返回 false。
func verifyCaptcha(c *gin.Context, id, answer string) bool {
	if !CaptchaRequired(utils.GetClientIP(c)) {
		return true
	}
	if !captcha.Verify(id, answer) {
		// 单次验证码错误不构成安全事件；仅作为认证失败计入 IP 频率，达阈值时才入库。
		if decision := security.NoteLoginFailure(utils.GetClientIP(c), "", c.Request.UserAgent(), false); decision != nil {
			recordAuthEvent(c, decision.EventType, "auth.captcha", utils.GetClientIP(c), decision.Severity, decision.Reason)
		}
		response.HandleBadRequest(c, "验证码错误或已过期，请重试", gin.H{"code": "captcha_invalid"})
		return false
	}
	return true
}

// GetCaptcha 生成图形验证码（匿名可拉取，路由层按 IP 限流）。
// 返回 captcha_id 与可直接用于 <img src> 的 PNG data URI。
func (h *Handler) GetCaptcha(c *gin.Context) {
	id, dataURI, err := captcha.Generate()
	if err != nil {
		utils.Warn("生成图形验证码失败", utils.Err(err))
		response.HandleInternalServerError(c, "验证码生成失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"captcha_id": id,
		"image":      dataURI,
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
	if isLoginLockedByUsername(utils.GetClientIP(c), req.Username) {
		middleware.LogOperation(c, "auth.register", req.Username, fmt.Errorf("注册被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.register", req.Username, models.SeverityHigh, "尝试过于频繁，注册被频率限制锁定")
		handleLoginBlocked(c)
		return
	}
	resp, err := h.authService.Register(&req)
	if err != nil {
		registerLoginFailure(utils.GetClientIP(c))
		registerLoginFailureByUsername(utils.GetClientIP(c), req.Username)
		middleware.LogOperation(c, "auth.register", req.Username, err)
		h.recordAuthFailure(c, "auth.register", req.Username, false)
		h.recordLoginLog(c, "", req.Username, services.LoginMethodRegister, "failed", err.Error())
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}
	resetLoginFailures(utils.GetClientIP(c))
	resetLoginFailuresByUsername(utils.GetClientIP(c), req.Username)
	middleware.LogOperation(c, "auth.register", req.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, services.LoginMethodRegister, "success", "")
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
	if isLoginLockedByUsername(utils.GetClientIP(c), req.Username) {
		middleware.LogOperation(c, "auth.login", req.Username, fmt.Errorf("登录被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.login", req.Username, models.SeverityHigh, "尝试过于频繁，登录被频率限制锁定")
		handleLoginBlocked(c)
		return
	}
	resp, method, err := h.authService.Login(&req)
	if err != nil {
		registerLoginFailure(utils.GetClientIP(c))
		registerLoginFailureByUsername(utils.GetClientIP(c), req.Username)
		// 详细原因进日志与登录日志，界面只给通用提示，避免暴露 LDAP/OAuth 等认证源细节
		utils.Warn("用户登录失败", utils.String("username", req.Username), utils.Err(err))
		middleware.LogOperation(c, "auth.login", req.Username, err)
		h.recordAuthFailure(c, "auth.login", req.Username, h.authService.IsAdminUsername(req.Username))
		// 失败时无法可靠判定账号来源，按登录入口记录
		h.recordLoginLog(c, "", req.Username, services.LoginMethodPasswordForm, "failed", err.Error())
		response.HandleUnauthorized(c, services.UserFacingLoginError(err))
		return
	}
	resetLoginFailures(utils.GetClientIP(c))
	resetLoginFailuresByUsername(utils.GetClientIP(c), req.Username)
	middleware.LogOperation(c, "auth.login", req.Username, nil)
	recordAuthSuccess(c, resp.Username)
	// 成功时记录真实认证来源（local / ldap）
	h.recordLoginLog(c, resp.UUID, resp.Username, method, "success", "")
	// 登录成功即写 HttpOnly Cookie，统一 Vue 与 lite 两端的会话凭证。
	middleware.SetAuthCookie(c, resp.Token)
	// 本地账号登录成功后再探测目录同名账号，命中则附带一次性冲突提示（不阻断登录）
	h.detectDirectoryUsernameConflict(resp, method)
	response.HandleSuccess(c, http.StatusOK, "登录成功", authSessionResponse(resp))
}

// Logout 登出：清除 HttpOnly Cookie，同时清理前端由服务端维护的会话凭证。
// Cookie 为 HttpOnly，前端脚本无法直接删除，必须由后端清除。
func (h *Handler) Logout(c *gin.Context) {
	middleware.ClearAuthCookie(c)
	response.HandleSuccess(c, http.StatusOK, "已退出登录", nil)
}

// Refresh 令牌刷新：仅供已认证用户调用（路由受 AuthRequired 保护），
// 不校验密码与验证码。令牌接近过期时换取新令牌并同步 HttpOnly Cookie；
// 未到刷新点时不改动凭证。响应体结构与 GetCurrentUser 一致（uuid/username/role/createdAt）。
func (h *Handler) Refresh(c *gin.Context) {
	user, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, err.Error())
		return
	}
	data := gin.H{
		"uuid": user.UUID, "username": user.Username,
		"display_name": user.DisplayName, "nickname": user.Nickname,
		"role": user.Role, "created_at": user.CreatedAt,
		"auth_source":              user.AuthSource,
		"identity_notice_pending":  user.IdentityNoticePending,
		"identity_notice_provider": services.IdentityNoticeProvider(user),
	}
	if newToken := h.refreshTokenIfNeeded(c); newToken != "" {
		// 令牌被刷新时只同步 HttpOnly Cookie，凭证不回传响应体（不暴露给脚本）。
		middleware.SetAuthCookie(c, newToken)
	}
	response.HandleSuccess(c, http.StatusOK, "", data)
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
		"display_name": user.DisplayName, "nickname": user.Nickname,
		"role": user.Role, "created_at": user.CreatedAt,
		"auth_source":              user.AuthSource,
		"identity_notice_pending":  user.IdentityNoticePending,
		"identity_notice_provider": services.IdentityNoticeProvider(user),
	}
	if newToken != "" {
		// 令牌被刷新时只同步 HttpOnly Cookie，不回传响应体（凭证不对脚本暴露）。
		middleware.SetAuthCookie(c, newToken)
	}
	response.HandleSuccess(c, http.StatusOK, "", data)
}

// AckIdentityNotice 用户确认「管理员已为你完成外部身份绑定」提示，清除待提示标记。
// 前端在弹框关闭后调用，保证该提示只出现一次。
func (h *Handler) AckIdentityNotice(c *gin.Context) {
	user, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, err.Error())
		return
	}
	if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).
		UpdateColumn("identity_notice_pending", false).Error; err != nil {
		utils.Warn("清除身份绑定提示标记失败", utils.String("user", user.Username), utils.Err(err))
		response.HandleInternalServerError(c, "操作失败，请稍后重试")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", nil)
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
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

// DeleteAccountRequest 注销账号请求（需再次输入当前密码确认）
type DeleteAccountRequest struct {
	Password string `json:"password" binding:"required"`
}

// DeleteAccount 注销当前登录账号，连带清除该用户的私有/临时文件存储
func (h *Handler) DeleteAccount(c *gin.Context) {
	var req DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	user, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, err.Error())
		return
	}

	if err := h.authService.DeleteAccount(user.UUID, req.Password); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	middleware.InvalidateUserDisabledCache(user.UUID)
	middleware.LogOperation(c, "auth.delete_account", user.Username, nil)
	middleware.ClearAuthCookie(c)
	response.HandleSuccess(c, http.StatusOK, "账号已注销", nil)
}

// UpdateProfileRequest 修改个人资料请求（用户名不可修改）
type UpdateProfileRequest struct {
	DisplayName string `json:"display_name" binding:"max=64"`
	Nickname    string `json:"nickname" binding:"max=32"`
}

// UpdateProfile 修改当前用户资料（姓名、昵称）
func (h *Handler) UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	current, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, err.Error())
		return
	}

	user, err := h.authService.UpdateProfile(current.UUID, req.DisplayName, req.Nickname)
	if err != nil {
		middleware.LogOperation(c, "auth.update_profile", current.Username, err)
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	middleware.LogOperation(c, "auth.update_profile", user.Username, nil)
	response.HandleSuccess(c, http.StatusOK, "资料已更新", gin.H{
		"uuid":         user.UUID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"nickname":     user.Nickname,
		"role":         user.Role,
		"created_at":   user.CreatedAt,
	})
}

// OAuth2Status 返回外部 OAuth2 认证的可用状态，供登录页决定是否展示入口。
// 不返回提供方名称等后端实现细节，避免界面暴露具体认证源。
func (h *Handler) OAuth2Status(c *gin.Context) {
	enabled := h.oauth2Service != nil && h.oauth2Service.IsEnabled()
	response.HandleSuccess(c, http.StatusOK, "", gin.H{"enabled": enabled})
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
		response.HandleInternalServerError(c, "外部认证暂时不可用，请稍后重试")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"authorize_url": authURL,
		"state":         state,
	})
}

// oauth2UserFacingError 外部认证失败时展示给用户的通用提示，
// 具体原因（连接失败、令牌交换失败、用户不存在等）只写日志。
const oauth2UserFacingError = "外部认证未完成，请返回登录页重试或联系管理员"

// OAuth2Callback 处理外部认证回调：校验 state → 换取用户信息 → 映射本地用户 → 写会话 Cookie 并跳回前端
func (h *Handler) OAuth2Callback(c *gin.Context) {
	if h.oauth2Service == nil || !h.oauth2Service.IsEnabled() {
		utils.Warn("OAuth2 回调时外部认证未启用")
		h.redirectOAuth2Error(c)
		return
	}
	if errMsg := strings.TrimSpace(c.Query("error")); errMsg != "" {
		desc := c.Query("error_description")
		utils.Warn("OAuth2 提供方返回错误",
			utils.String("error", errMsg), utils.String("error_description", desc))
		// 失败按入口记录，便于个人登录记录还原尝试
		h.recordLoginLog(c, "", "", services.LoginMethodOAuth2, "failed", "外部认证提供方返回错误")
		h.redirectOAuth2Error(c)
		return
	}
	if err := h.oauth2Service.verifyState(c.Query("state")); err != nil {
		utils.Warn("OAuth2 回调 state 校验失败", utils.Err(err))
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.oauth2", "", models.SeverityMedium, "OAuth2 回调 state 校验失败: "+err.Error())
		h.recordLoginLog(c, "", "", services.LoginMethodOAuth2, "failed", "state 校验失败")
		h.redirectOAuth2Error(c)
		return
	}

	user, err := h.oauth2Service.Authenticate(c.Query("code"), h.oauth2CallbackURL(c))
	if err != nil {
		// 同名冲突且配置为自助处理：不建会话，签发一次性确认票据并跳回前端确认页。
		// 管理员账号、已禁用账号、非本地归属账号仍按原逻辑拒绝。
		var conflict *services.IdentityConflictError
		if errors.As(err, &conflict) {
			// 冲突错误对外保持通用文案，日志里保留可追踪的外部身份信息。
			utils.Warn("OAuth2 登录检测到同名本地账号",
				utils.String("username", conflict.Username),
				utils.String("auth_type", conflict.AuthType),
				utils.String("provider", conflict.ProviderName),
				utils.String("external_username", conflict.ExternalUsername))
			if h.issueIdentityConfirmTicket(c, conflict) {
				return
			}
		}
		utils.Warn("OAuth2 认证失败", utils.Err(err))
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.oauth2", "", models.SeverityMedium, "OAuth2 认证失败: "+err.Error())
		h.recordLoginLog(c, "", "", services.LoginMethodOAuth2, "failed", err.Error())
		h.redirectOAuth2Error(c)
		return
	}

	resp, err := h.authService.LoginByUser(user)
	if err != nil {
		utils.Warn("OAuth2 用户登录失败", utils.String("username", user.Username), utils.Err(err))
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.oauth2", user.Username, models.SeverityMedium, "OAuth2 用户登录失败: "+err.Error())
		h.recordLoginLog(c, "", user.Username, services.LoginMethodOAuth2, "failed", err.Error())
		h.redirectOAuth2Error(c)
		return
	}
	middleware.LogOperation(c, "auth.oauth2.login", user.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, services.LoginMethodOAuth2, "success", "")

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

// redirectOAuth2Error 认证失败时跳回前端，仅携带通用提示
func (h *Handler) redirectOAuth2Error(c *gin.Context) {
	// 外部认证失败时回收可能残留的同名冲突票据，避免下次进入回调页误展示过期的确认信息。
	middleware.ClearIdentityConfirmCookie(c)
	q := url.Values{}
	q.Set("error", oauth2UserFacingError)
	c.Redirect(http.StatusFound, "/#/oauth2/callback?"+q.Encode())
}
