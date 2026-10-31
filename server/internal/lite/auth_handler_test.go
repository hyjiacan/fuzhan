package lite

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/captcha"
	"fuzhan/internal/models"
	"fuzhan/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setCaptchaConfig 临时设置验证码配置，测试结束后恢复，避免污染其它用例。
func setCaptchaConfig(t *testing.T, mode string, triggerAfter int) {
	t.Helper()
	prev := appconfig.GlobalConfig.Account.Captcha
	appconfig.LockConfig()
	appconfig.GlobalConfig.Account.Captcha = appconfig.CaptchaConfig{Mode: mode, TriggerAfter: triggerAfter}
	appconfig.UnlockConfig()
	t.Cleanup(func() {
		appconfig.LockConfig()
		appconfig.GlobalConfig.Account.Captcha = prev
		appconfig.UnlockConfig()
	})
}

// TestLiteCaptchaRequired 覆盖 lite 端验证码三态判定：disabled 不要求、
// always 始终要求、after_n 未达阈值不要求、达到阈值后要求。
func TestLiteCaptchaRequired(t *testing.T) {
	const ip = "198.51.100.10"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	// disabled：任意失败次数都不要求
	setCaptchaConfig(t, appconfig.CaptchaModeDisabled, 3)
	for i := 0; i < 4; i++ {
		liteRegisterFailure(ip)
	}
	if liteCaptchaRequired(ip) {
		t.Fatal("disabled 模式下 liteCaptchaRequired 应为 false")
	}
	liteResetFailures(ip)

	// after_n：未达阈值不要求，达到阈值后要求
	setCaptchaConfig(t, appconfig.CaptchaModeAfterN, 3)
	liteRegisterFailure(ip)
	liteRegisterFailure(ip)
	if liteCaptchaRequired(ip) {
		t.Fatal("after_n 未达阈值（2<3）时 liteCaptchaRequired 应为 false")
	}
	liteRegisterFailure(ip)
	if !liteCaptchaRequired(ip) {
		t.Fatal("after_n 达到阈值（3>=3）后 liteCaptchaRequired 应为 true")
	}
	liteResetFailures(ip)

	// after_n + 默认阈值（triggerAfter 非正数时按默认 3）
	setCaptchaConfig(t, appconfig.CaptchaModeAfterN, 0)
	liteRegisterFailure(ip)
	liteRegisterFailure(ip)
	if liteCaptchaRequired(ip) {
		t.Fatal("after_n 默认阈值未达（2<3）时 liteCaptchaRequired 应为 false")
	}
	liteRegisterFailure(ip)
	if !liteCaptchaRequired(ip) {
		t.Fatal("after_n 默认阈值达到（3>=3）后 liteCaptchaRequired 应为 true")
	}

	// always：无失败记录也始终要求
	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)
	if !liteCaptchaRequired("192.0.2.99") {
		t.Fatal("always 模式下 liteCaptchaRequired 应为 true")
	}
}

// TestCaptchaImageDisabledAndBelowThresholdEmpty 验证 disabled 或 after_n 未达阈值时
// CaptchaImage 返回空串（模板 {{if}} 不渲染任何额外 DOM）。
func TestCaptchaImageDisabledAndBelowThresholdEmpty(t *testing.T) {
	const ip = "198.51.100.11"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	setCaptchaConfig(t, appconfig.CaptchaModeDisabled, 3)
	if got := CaptchaImage(ip); got != "" {
		t.Fatalf("disabled 模式 CaptchaImage 应为空串，实际 %q", got)
	}

	setCaptchaConfig(t, appconfig.CaptchaModeAfterN, 3)
	liteRegisterFailure(ip)
	if got := CaptchaImage(ip); got != "" {
		t.Fatalf("after_n 未达阈值 CaptchaImage 应为空串，实际 %q", got)
	}
}

// TestCaptchaImageAlwaysRendersBlock 验证 always 模式 CaptchaImage 输出完整验证码区块：
// PNG data URI + 隐藏域 + 文本输入框。
func TestCaptchaImageAlwaysRendersBlock(t *testing.T) {
	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)

	got := string(CaptchaImage("198.51.100.12"))
	if got == "" {
		t.Fatal("always 模式 CaptchaImage 不应为空")
	}
	for _, want := range []string{
		`<div class="captcha-row">`,
		`<img src="data:image/png;base64,`,
		`name="captcha_challenge"`,
		`name="captcha_text"`,
		`placeholder="验证码"`,
		`required`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("验证码区块缺少 %q\n%s", want, got)
		}
	}
}

// TestCaptchaImageAfterNOnThresholdRendersBlock 验证 after_n 达到阈值后 CaptchaImage 非空且含 PNG data URI。
func TestCaptchaImageAfterNOnThresholdRendersBlock(t *testing.T) {
	const ip = "198.51.100.13"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	setCaptchaConfig(t, appconfig.CaptchaModeAfterN, 3)
	liteRegisterFailure(ip)
	liteRegisterFailure(ip)
	liteRegisterFailure(ip)

	got := string(CaptchaImage(ip))
	if got == "" {
		t.Fatal("after_n 达到阈值后 CaptchaImage 不应为空")
	}
	if !strings.Contains(got, "data:image/png;base64,") {
		t.Fatalf("验证码区块应含 PNG data URI\n%s", got)
	}
}

// setupLiteTestDB 创建内存 SQLite 数据库用于登录/注册链路测试。
func setupLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.LoginLog{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

// postLiteLogin 以指定 IP 向 /lite/login 发起 POST 登录请求（urlencoded），返回响应。
func postLiteLogin(ip, body string, db *gorm.DB) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewHandlers(nil, services.NewAuthService(db), db)
	router.POST("/lite/login", h.HandleLoginSubmit)

	req := httptest.NewRequest(http.MethodPost, "/lite/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestLoginSubmitRequiresCaptchaInAlwaysMode 验证 always 模式缺验证码的登录提交被拒绝（400）
// 且页面含「验证码错误」提示；disabled 模式验证码门禁放行，走向真实登录（无此用户 → 401）。
func TestLoginSubmitRequiresCaptchaInAlwaysMode(t *testing.T) {
	const ip = "198.51.100.15"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })
	db := setupLiteTestDB(t)

	// always 模式：缺验证码 → 400 + 验证码错误
	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)
	w := postLiteLogin(ip, "username=abc&password=abc123", db)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("always 模式缺验证码应返回 400，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "验证码错误或已过期") {
		t.Fatalf("页面应含验证码错误提示\n%s", w.Body.String())
	}

	// disabled 模式：验证码门禁放行，真实登录因账户不存在返回 401（而非验证码 400）
	liteResetFailures(ip)
	setCaptchaConfig(t, appconfig.CaptchaModeDisabled, 0)
	w = postLiteLogin(ip, "username=abc&password=abc123", db)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled 模式应放行验证码门禁并返回 401，实际 %d\n%s", w.Code, w.Body.String())
	}
}

// TestLoginSubmitAllowsCorrectAnswer 验证 always 模式下注入已知答案的验证码可放行门禁。
// 提交未注册用户时返回 401（用户名/密码错误）而非验证码 400，
// 证明验证码门禁已通过、进入真实登录流程。
func TestLoginSubmitAllowsCorrectAnswer(t *testing.T) {
	const ip = "198.51.100.16"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })
	db := setupLiteTestDB(t)

	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)
	const id, answer = "lite-test-correct-answer", "k3m9"
	captcha.SetTestAnswer(id, answer)

	body := "username=abc&password=abc123&captcha_challenge=" + id + "&captcha_text=" + answer
	w := postLiteLogin(ip, body, db)
	if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "验证码错误") {
		t.Fatalf("正确答案不应触发验证码 400，实际 %d\n%s", w.Code, w.Body.String())
	}
}
