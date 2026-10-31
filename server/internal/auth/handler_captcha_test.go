package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/captcha"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

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

// serveWithIP 以独立客户端 IP 发起请求（GetClientIP 在未信任代理时回退 RemoteAddr）。
func serveWithIP(r http.Handler, method, path, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// probeVerifyCaptcha 直接调用验证码门禁，返回放行结果与响应。
func probeVerifyCaptcha(t *testing.T, ip, id, answer string) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	allowed := false
	r := gin.New()
	r.GET("/probe", func(c *gin.Context) {
		allowed = verifyCaptcha(c, id, answer)
		if allowed {
			c.Status(http.StatusOK)
		}
	})
	return allowed, serveWithIP(r, http.MethodGet, "/probe", ip)
}

// TestVerifyCaptchaDisabledAllowsEmpty 验证 disabled 模式下缺失验证码也放行。
func TestVerifyCaptchaDisabledAllowsEmpty(t *testing.T) {
	setCaptchaConfig(t, appconfig.CaptchaModeDisabled, 0)

	allowed, w := probeVerifyCaptcha(t, "198.51.100.1", "", "")
	if !allowed {
		t.Fatal("disabled 模式下 verifyCaptcha 应返回 true")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("放行时不应写入错误响应，实际状态 %d", w.Code)
	}
}

// TestVerifyCaptchaAlwaysRejectsEmptyOrWrong 验证 always 模式下缺失/错误验证码均返回 400。
func TestVerifyCaptchaAlwaysRejectsEmptyOrWrong(t *testing.T) {
	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)

	allowed, w := probeVerifyCaptcha(t, "198.51.100.2", "", "")
	if allowed {
		t.Fatal("always 模式下空验证码应返回 false")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空验证码应返回 400，实际状态 %d", w.Code)
	}

	allowed, w = probeVerifyCaptcha(t, "198.51.100.2", "unknown-id", "wrong")
	if allowed {
		t.Fatal("always 模式下错误验证码应返回 false")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("错误验证码应返回 400，实际状态 %d", w.Code)
	}

	var body struct {
		Errors struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if body.Errors.Code != "captcha_invalid" {
		t.Fatalf("错误响应 code = %q, want captcha_invalid", body.Errors.Code)
	}
}

// TestVerifyCaptchaAfterNTriggersAfterThreshold 验证 after_n 模式在失败达到阈值后开启验证码。
func TestVerifyCaptchaAfterNTriggersAfterThreshold(t *testing.T) {
	const ip = "198.51.100.3"
	resetLoginFailures(ip)
	t.Cleanup(func() { resetLoginFailures(ip) })
	setCaptchaConfig(t, appconfig.CaptchaModeAfterN, 3)

	registerLoginFailure(ip)
	registerLoginFailure(ip)
	allowed, _ := probeVerifyCaptcha(t, ip, "", "")
	if !allowed {
		t.Fatal("after_n 模式下失败 2 次不应要求验证码")
	}

	registerLoginFailure(ip)
	allowed, w := probeVerifyCaptcha(t, ip, "", "")
	if allowed {
		t.Fatal("after_n 模式下失败达到 3 次后应要求验证码")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("需要验证码但缺失时应返回 400，实际状态 %d", w.Code)
	}
}

// TestGetCaptchaReturnsIDAndPNGDataURI 验证获取验证码接口返回 captchaId 与 PNG data URI。
func TestGetCaptchaReturnsIDAndPNGDataURI(t *testing.T) {
	r := gin.New()
	r.GET("/captcha", NewHandler(nil, nil).GetCaptcha)

	w := serveWithIP(r, http.MethodGet, "/captcha", "198.51.100.4")
	if w.Code != http.StatusOK {
		t.Fatalf("获取验证码应返回 200，实际状态 %d", w.Code)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			CaptchaID string `json:"captchaId"`
			Image     string `json:"image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析验证码响应失败: %v", err)
	}
	if !body.Success {
		t.Fatalf("success = false, body = %s", w.Body.String())
	}
	if body.Data.CaptchaID == "" {
		t.Fatal("captchaId 不应为空")
	}
	if !strings.HasPrefix(body.Data.Image, "data:image/png;base64,") {
		t.Fatalf("image 应以 PNG data URI 开头，实际前缀 %q", trimPrefix(body.Data.Image))
	}
}

func trimPrefix(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

// TestCaptchaRateLimitBlocksAfterThirty 验证获取验证码接口固定每 IP 每分钟 30 次。
// 使用随每次测试运行而变化的唯一 IP（时间戳），避免 accessguard 全局缓存
// 在 -count=2 或多次运行间残留计数（前一次已把该 IP 打到 30，第二次会误判 429）。
func TestCaptchaRateLimitBlocksAfterThirty(t *testing.T) {
	uniq := time.Now().UnixNano()
	ip := "203.0.113." + strconv.FormatInt((uniq%250)+1, 10) // 1..250，避免与文档保留示例段混淆
	r := gin.New()
	r.GET("/captcha", middleware.CaptchaRateLimit(), NewHandler(nil, nil).GetCaptcha)

	for i := 0; i < 30; i++ {
		if w := serveWithIP(r, http.MethodGet, "/captcha", ip); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求应放行，实际状态 %d", i+1, w.Code)
		}
	}
	if w := serveWithIP(r, http.MethodGet, "/captcha", ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 31 次请求应被限流（429），实际状态 %d", w.Code)
	}
}

// TestVerifyCaptchaAlwaysAllowsCorrectAnswer 验证 always 模式下注入已知答案的
// 验证码可正常放行：verifyCaptcha 返回 true 且不写错误响应。
// captcha.Verify 为一次性（clear=true），每个 id 只能验证一次，故每次用新 id。
func TestVerifyCaptchaAlwaysAllowsCorrectAnswer(t *testing.T) {
	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)

	const id, answer = "auth-test-correct-answer", "k3m9"
	captcha.SetTestAnswer(id, answer)

	allowed, w := probeVerifyCaptcha(t, "198.51.100.5", id, answer)
	if !allowed {
		t.Fatal("always 模式下正确答案应放行")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("正确答案放行时不应写入错误响应，实际状态 %d", w.Code)
	}

	var body struct {
		Success bool `json:"success"`
		Errors  any  `json:"errors"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Errors != nil {
		t.Fatalf("正确答案放行时响应不应携带 errors，实际 body = %s", w.Body.String())
	}
}

// TestRefreshReturnsCurrentUserWithoutCaptcha 验证 /auth/refresh 在 always 验证码模式下
// 仍可返回当前用户信息（不校验密码/验证码）。通过注入 userUUID 上下文模拟 AuthRequired 登录态，
// 不传 Authorization 头，故 refreshTokenIfNeeded 不产生新令牌，仅返回用户信息。
func TestRefreshReturnsCurrentUserWithoutCaptcha(t *testing.T) {
	setCaptchaConfig(t, appconfig.CaptchaModeAlways, 0)
	db := setupTestDB(t)
	uuid := "11111111-2222-3333-4444-555555555555"
	db.Create(&models.User{UUID: uuid, Username: "refresh-user", PasswordHash: "hash", Role: "user"})

	h := NewHandler(services.NewAuthService(db), db)
	r := gin.New()
	r.POST("/refresh", func(c *gin.Context) {
		c.Set("userUUID", uuid)
	}, h.Refresh)

	w := serveWithIP(r, http.MethodPost, "/refresh", "198.51.100.6")
	if w.Code != http.StatusOK {
		t.Fatalf("已登录刷新应返回 200，实际状态 %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			UUID      string `json:"uuid"`
			Username  string `json:"username"`
			Role      string `json:"role"`
			CreatedAt string `json:"createdAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析刷新响应失败: %v", err)
	}
	if !body.Success || body.Data.UUID != uuid || body.Data.Username != "refresh-user" || body.Data.Role != "user" {
		t.Fatalf("响应体与 GetCurrentUser 结构不一致，body = %s", w.Body.String())
	}
}
