package lite

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/captcha"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/security"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"
)

// AllowRegistration 判断注册入口是否展示/受理：仅由「允许注册」开关控制
// （是否启用私有存储、是否启用强制登录都不影响注册入口是否开放）。
func AllowRegistration() bool {
	return appconfig.GlobalConfig.Account.AllowRegistration
}

// authErrView 认证页渲染值：标题 + 错误提示 + 回跳地址（表单隐藏域原样保留给用户修正后重新提交）
type authErrView struct {
	Title    string // 页面 <title>
	AppName  string
	LogoPath string
	Heading  string        // 表单区标题（登录/注册）
	Error    string        // 错误提示（空串则不显示）
	Redirect string        // 登录成功后回跳地址（隐藏域 / 页间跳转链接，模板自动转义）
	Username string        // 用户名回填（失败后保留用户输入，密码不回显）
	Captcha  template.HTML // 验证码区块（已内嵌完整 HTML，空串表示无需验证码）
}

// CaptchaImage 渲染验证码区块（零 JS，SSR 内嵌）：需要验证码时输出
// 隐藏域（验证码 id）+ PNG 图片（data URI）+ 文本输入框，无需时返回空串。
// 每次调用都重新 Generate 新图，天然支持重试（旧验证码随提交被一次性销毁）。
func CaptchaImage(ip string) template.HTML {
	if !liteCaptchaRequired(ip) {
		return template.HTML("")
	}
	id, dataURI, err := captcha.Generate()
	if err != nil {
		utils.Error("生成图形验证码失败", utils.Err(err))
		return template.HTML("")
	}
	return template.HTML(fmt.Sprintf(
		`<div class="captcha-row"><img src="%s" alt="验证码"><input type="hidden" name="captcha_challenge" value="%s"><input type="text" name="captcha_text" placeholder="验证码" required></div>`,
		template.HTMLEscapeString(dataURI), template.HTMLEscapeString(id)))
}

// liteCaptchaRequired lite 端验证码三态判定，复用 lite 独立失败计数：
// disabled 不要求；always 始终要求；after_n 当前窗口失败达到阈值后要求。
// mode 归一化（去空白 + 转小写）与 Web 端 CaptchaRequired 保持一致，未知值一律不要求。
func liteCaptchaRequired(ip string) bool {
	cfg := appconfig.GetConfig().Account.Captcha
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case appconfig.CaptchaModeAlways:
		return true
	case appconfig.CaptchaModeAfterN:
		threshold := cfg.TriggerAfter
		if threshold <= 0 {
			threshold = appconfig.DefaultCaptchaTriggerAfter
		}
		return LiteLoginFailureCountForIP(ip) >= threshold
	default: // disabled / 空值 / 未知值一律不要求，保证默认零行为影响
		return false
	}
}

// recordAuthEvent 将 lite 登录/注册相关风险写入安全事件库（供暴破分析与强提醒）
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
func (h *Handlers) recordAuthFailure(c *gin.Context, operation, username string, highValue bool) {
	decision := security.NoteLoginFailure(utils.GetClientIP(c), username, c.Request.UserAgent(), highValue)
	if decision == nil {
		return
	}
	recordAuthEvent(c, decision.EventType, operation, username, decision.Severity, decision.Reason)
}

// recordAuthSuccess 登录成功后检查「多次失败后从不同来源成功」的可疑登录场景。
func (h *Handlers) recordAuthSuccess(c *gin.Context, username string) {
	decision := security.NoteLoginSuccess(utils.GetClientIP(c), username, c.Request.UserAgent())
	if decision == nil {
		return
	}
	recordAuthEvent(c, decision.EventType, "auth.login", username, decision.Severity, decision.Reason)
}

// sanitizeRedirect 收敛回跳地址：仅允许站内相对路径（须以 "/" 开头且不含反斜杠、
// 非 // 或 /\ 开头的协议相对地址），并强制回跳到 /lite 内，防止开放重定向与越权回跳。
// 非法或为空时返回 /lite。
func sanitizeRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/lite"
	}
	if !strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "//") ||
		strings.HasPrefix(raw, "/\\") ||
		strings.Contains(raw, "\\") {
		return "/lite"
	}
	if raw == "/lite/login" || raw == "/lite/register" || raw == "/lite/logout" {
		return "/lite"
	}
	if raw != "/lite" && !strings.HasPrefix(raw, "/lite/") {
		return "/lite"
	}
	return raw
}

// parseAuthForm 解析并校验登录/注册表单（POST urlencoded）。
// 校验与本服务 JSON 接口一致：用户名 3-32（不含空白）、密码长度由调用方按场景传入
// （登录沿用 >=6 兼容既有账号，注册/设置新密码按新策略要求 >=8）；
// 返回校验错误信息（空串表示通过）。
func parseAuthForm(c *gin.Context, minPasswordLen int) (authFormData, string) {
	data := authFormData{
		Username: strings.TrimSpace(c.PostForm("username")),
		Password: c.PostForm("password"),
		Redirect: sanitizeRedirect(c.PostForm("redirect")),
	}
	if len(data.Username) < 3 || len(data.Username) > 32 || strings.ContainsAny(data.Username, " \t\r\n") {
		return data, "用户名长度需在 3-32 位之间"
	}
	if len(data.Password) < minPasswordLen || len(data.Password) > 128 {
		return data, fmt.Sprintf("密码长度需在 %d-128 位之间", minPasswordLen)
	}
	return data, ""
}

// authFormData 轻量版登录/注册表单携带的数据（POST urlencoded）
type authFormData struct {
	Username string
	Password string
	Redirect string // 登录成功后回跳地址（原样回填表单隐藏域）
}

// appNameOf 返回配置的站点名称（空时回退 "Fuzhan"）
func appNameOf(name string) string {
	if name == "" {
		return "Fuzhan"
	}
	return name
}

// renderAuthPage 渲染轻量版认证页（登录/注册共用一套模板），含错误提示、用户名回填与回跳地址。
func renderAuthPage(c *gin.Context, heading, errMsg string, data authFormData, status int) {
	tmpl, tmplErr := authTmpl()
	if tmplErr != nil {
		utils.Error("解析轻量版认证页模板失败", utils.Err(tmplErr))
		http.Error(c.Writer, "internal server error", http.StatusInternalServerError)
		return
	}
	v := authErrView{
		Title:    appNameOf(appconfig.GlobalConfig.App.Name) + " - " + heading + " - " + liteName,
		AppName:  appNameOf(appconfig.GlobalConfig.App.Name),
		LogoPath: "/assets/icons/logo.png",
		Heading:  heading,
		Error:    errMsg,
		Redirect: data.Redirect,
		Username: data.Username,
		Captcha:  CaptchaImage(utils.GetClientIP(c)),
	}
	c.Status(status)
	if err := tmpl.Execute(c.Writer, v); err != nil {
		utils.Error("渲染轻量版认证页失败", utils.Err(err))
	}
}

// redirectIfLoggedIn 若请求已携带有效会话 Cookie（JWT 可解析），302 回跳
// redirect 参数指定的站内地址（默认 /lite，经 sanitizeRedirect 收敛）。
// 供登录/注册页「已登录则自动跳过」使用：判定仅依赖 Cookie，一致性
// 与 lite.go 渲染页头登录态（jwt.ParseJWT + TokenFromRequest）保持一致。
func redirectIfLoggedIn(c *gin.Context, rawRedirect string) bool {
	if _, err := jwt.ParseJWT(middleware.TokenFromRequest(c.Request)); err != nil {
		return false
	}
	c.Redirect(http.StatusFound, sanitizeRedirect(rawRedirect))
	return true
}

// HandleLoginPage GET /lite/login：渲染轻量版登录页。
// 登录入口与 web 一致始终可用（后端登录接口无开关门槛），由页面入口按需隐藏。
// 已持有有效会话 Cookie 时自动回跳，不再渲染登录页。
func (h *Handlers) HandleLoginPage(c *gin.Context) {
	if redirectIfLoggedIn(c, c.Query("redirect")) {
		return
	}
	renderAuthPage(c, "登录", "", authFormData{Redirect: sanitizeRedirect(c.Query("redirect"))}, http.StatusOK)
}

// HandleLoginSubmit POST /lite/login：校验 → 登录 → 写会话 Cookie → 302 回跳 /lite。
// 失败时重新渲染登录页并携带错误提示；命中爆破锁定（429）时也渲染登录页提示稍后再试。
func (h *Handlers) HandleLoginSubmit(c *gin.Context) {
	// 登录沿用最低 6 位：既有弱密码用户不受影响（不迁移哈希，仅要求已知密码正确）
	data, verr := parseAuthForm(c, 6)
	if verr != "" {
		renderAuthPage(c, "登录", verr, data, http.StatusBadRequest)
		return
	}

	// 图形验证码门禁（SSR 内嵌，零 JS）：lite 端需要验证码时校验表单的
	// captcha_challenge/captcha_text，失败重渲染登录页（新图），不计数、不进锁定。
	if liteCaptchaRequired(utils.GetClientIP(c)) {
		challenge := c.PostForm("captcha_challenge")
		text := c.PostForm("captcha_text")
		if !captcha.Verify(challenge, text) {
			// 单次验证码错误不构成安全事件，仅计入 IP 频率，达阈值时才入库。
			h.recordAuthFailure(c, "auth.captcha", data.Username, h.AuthService.IsAdminUsername(data.Username))
			renderAuthPage(c, "登录", "验证码错误或已过期，请重试", data, http.StatusBadRequest)
			return
		}
	}

	ip := utils.GetClientIP(c)
	if liteIsLoginLocked(ip) {
		middleware.LogOperation(c, "auth.login", data.Username, errors.New("登录被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.login", data.Username, models.SeverityHigh, "尝试过于频繁，登录被频率限制锁定")
		renderAuthPage(c, "登录", "尝试过于频繁，请稍后再试", data, http.StatusTooManyRequests)
		return
	}

	resp, err := h.AuthService.Login(&services.LoginRequest{Username: data.Username, Password: data.Password})
	if err != nil {
		liteRegisterFailure(ip)
		middleware.LogOperation(c, "auth.login", data.Username, err)
		h.recordAuthFailure(c, "auth.login", data.Username, h.AuthService.IsAdminUsername(data.Username))
		h.recordLoginLog(c, "", data.Username, "local", "failed", err.Error())
		renderAuthPage(c, "登录", err.Error(), data, http.StatusUnauthorized)
		return
	}

	liteResetFailures(ip)
	middleware.LogOperation(c, "auth.login", data.Username, nil)
	h.recordAuthSuccess(c, resp.Username)
	h.recordLoginLog(c, resp.UUID, resp.Username, "local", "success", "")
	// 把与 web 同源的 JWT 存入 HttpOnly Cookie：LoginGate 依据它放行 /lite、/download 等
	// 受保护请求（下载身份鉴别与 web 完全一致，仍走「换票 + URL 票据」链路）。
	middleware.SetAuthCookie(c, resp.Token)
	c.Redirect(http.StatusFound, data.Redirect)
}

// HandleRegisterPage GET /lite/register：渲染轻量版注册页。
// 「允许注册」关闭时 302 回 /lite（注册入口隐藏，后端不渲染注册页）。
// 已持有有效会话 Cookie 时自动回跳，不再渲染注册页。
func (h *Handlers) HandleRegisterPage(c *gin.Context) {
	if redirectIfLoggedIn(c, c.Query("redirect")) {
		return
	}
	if !AllowRegistration() {
		c.Redirect(http.StatusFound, "/lite")
		return
	}
	renderAuthPage(c, "注册", "", authFormData{Redirect: sanitizeRedirect(c.Query("redirect"))}, http.StatusOK)
}

// HandleRegisterSubmit POST /lite/register：校验 → 注册 → 直接签发会话 Cookie → 302 回跳 /lite。
// 与 web 一致：注册成功即视为已登录（服务端 Register 直接返回 token），不再要求二次登录；
// 失败时重新渲染注册页并携带错误提示。
func (h *Handlers) HandleRegisterSubmit(c *gin.Context) {
	if !AllowRegistration() {
		c.Redirect(http.StatusFound, "/lite")
		return
	}

	// 注册是新设密码场景：按新策略强制最短 8 位
	data, verr := parseAuthForm(c, 8)
	if verr != "" {
		renderAuthPage(c, "注册", verr, data, http.StatusBadRequest)
		return
	}

	// 图形验证码门禁（SSR 内嵌，零 JS）：lite 端需要验证码时校验表单的
	// captcha_challenge/captcha_text，失败重渲染注册页（新图），不计数、不进锁定。
	if liteCaptchaRequired(utils.GetClientIP(c)) {
		challenge := c.PostForm("captcha_challenge")
		text := c.PostForm("captcha_text")
		if !captcha.Verify(challenge, text) {
			// 单次验证码错误不构成安全事件，仅计入 IP 频率，达阈值时才入库。
			h.recordAuthFailure(c, "auth.captcha", data.Username, false)
			renderAuthPage(c, "注册", "验证码错误或已过期，请重试", data, http.StatusBadRequest)
			return
		}
	}

	ip := utils.GetClientIP(c)
	if liteIsLoginLocked(ip) {
		middleware.LogOperation(c, "auth.register", data.Username, errors.New("注册被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.register", data.Username, models.SeverityHigh, "尝试过于频繁，注册被频率限制锁定")
		renderAuthPage(c, "注册", "尝试过于频繁，请稍后再试", data, http.StatusTooManyRequests)
		return
	}

	resp, err := h.AuthService.Register(&services.RegisterRequest{Username: data.Username, Password: data.Password})
	if err != nil {
		liteRegisterFailure(ip)
		middleware.LogOperation(c, "auth.register", data.Username, err)
		h.recordAuthFailure(c, "auth.register", data.Username, false)
		h.recordLoginLog(c, "", data.Username, "register", "failed", err.Error())
		renderAuthPage(c, "注册", err.Error(), data, http.StatusBadRequest)
		return
	}

	liteResetFailures(ip)
	middleware.LogOperation(c, "auth.register", data.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, "register", "success", "")
	middleware.SetAuthCookie(c, resp.Token)
	c.Redirect(http.StatusFound, data.Redirect)
}

// HandleLogout POST /lite/logout：清除会话 Cookie 后回跳 /lite。
// 登出接口匿名可访问（在白名单内），带不带 Cookie 都允许；仅响应 POST，避免 GET 触发登出副作用。
func (h *Handlers) HandleLogout(c *gin.Context) {
	middleware.ClearAuthCookie(c)
	c.Redirect(http.StatusFound, "/lite")
}
