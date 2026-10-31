package auth

import (
	"testing"
	"time"

	"fuzhan/internal/appconfig"
)

func TestLoginFailureCountForIP(t *testing.T) {
	const ip = "127.0.0.1"
	resetLoginFailures(ip)
	t.Cleanup(func() { resetLoginFailures(ip) })

	for i := 0; i < 3; i++ {
		registerLoginFailure(ip)
	}
	if got := LoginFailureCountForIP(ip); got != 3 {
		t.Fatalf("expected 3 failures, got %d", got)
	}
}

func TestLoginFailureCountForIPNotResetByLock(t *testing.T) {
	const ip = "10.0.0.1"
	resetLoginFailures(ip)
	t.Cleanup(func() { resetLoginFailures(ip) })

	for i := 0; i < 3; i++ {
		registerLoginFailure(ip)
	}
	// 再失败 3 次，累计 6 次，期间触发锁定并清零 count
	for i := 0; i < 3; i++ {
		registerLoginFailure(ip)
	}
	if !isLoginLocked(ip) {
		t.Fatal("expected ip to be locked")
	}
	if got := LoginFailureCountForIP(ip); got != 6 {
		t.Fatalf("expected displayFailures == 6, got %d", got)
	}

	// 锁定期内再失败一次，累计值应递增且仍处于锁定状态
	registerLoginFailure(ip)
	if got := LoginFailureCountForIP(ip); got != 7 {
		t.Fatalf("expected displayFailures == 7 after extra failure, got %d", got)
	}
	if !isLoginLocked(ip) {
		t.Fatal("expected ip to remain locked after extra failure")
	}
}

func TestLoginFailureCountForIPReset(t *testing.T) {
	const ip = "10.0.0.2"
	resetLoginFailures(ip)
	t.Cleanup(func() { resetLoginFailures(ip) })

	registerLoginFailure(ip)
	if got := LoginFailureCountForIP(ip); got != 1 {
		t.Fatalf("expected 1 failure, got %d", got)
	}
	resetLoginFailures(ip)
	if got := LoginFailureCountForIP(ip); got != 0 {
		t.Fatalf("expected 0 after reset, got %d", got)
	}
}

func TestLoginFailureCountForIPEmpty(t *testing.T) {
	if got := LoginFailureCountForIP(""); got != 0 {
		t.Fatalf("expected 0 for empty ip, got %d", got)
	}
}

// TestCaptchaRequired 覆盖验证码三态判定：disabled 不要求、always 始终要求、
// after_n 在失败次数达到阈值（<=0 时按默认 3）后才要求。
func TestCaptchaRequired(t *testing.T) {
	prevCfg := appconfig.GlobalConfig
	defer func() { appconfig.GlobalConfig = prevCfg }()

	const ip = "10.0.0.9"
	resetLoginFailures(ip)
	t.Cleanup(func() { resetLoginFailures(ip) })

	// disabled / 空值 / 未知值均不要求
	for _, mode := range []string{"", "disabled", "unknown"} {
		appconfig.GlobalConfig = appconfig.Config{Account: appconfig.AccountConfig{Captcha: appconfig.CaptchaConfig{Mode: mode}}}
		if CaptchaRequired(ip) {
			t.Errorf("mode=%q: CaptchaRequired = true, want false", mode)
		}
	}

	// always 始终要求（失败 0 次也要求）
	appconfig.GlobalConfig = appconfig.Config{Account: appconfig.AccountConfig{Captcha: appconfig.CaptchaConfig{Mode: "always"}}}
	if !CaptchaRequired(ip) {
		t.Error("mode=always: CaptchaRequired = false, want true")
	}

	// after_n：失败次数 < 阈值不要求，达到阈值后要求
	appconfig.GlobalConfig = appconfig.Config{Account: appconfig.AccountConfig{Captcha: appconfig.CaptchaConfig{Mode: "after_n", TriggerAfter: 3}}}
	registerLoginFailure(ip)
	registerLoginFailure(ip)
	if CaptchaRequired(ip) {
		t.Fatal("after_n: 2 failures, CaptchaRequired = true, want false")
	}
	registerLoginFailure(ip)
	if !CaptchaRequired(ip) {
		t.Fatal("after_n: 3 failures, CaptchaRequired = false, want true")
	}

	// after_n 且阈值为 0：按默认 3 计算
	resetLoginFailures(ip)
	appconfig.GlobalConfig = appconfig.Config{Account: appconfig.AccountConfig{Captcha: appconfig.CaptchaConfig{Mode: "after_n", TriggerAfter: 0}}}
	registerLoginFailure(ip)
	registerLoginFailure(ip)
	if CaptchaRequired(ip) {
		t.Fatal("after_n default: 2 failures, CaptchaRequired = true, want false")
	}
	registerLoginFailure(ip)
	if !CaptchaRequired(ip) {
		t.Fatal("after_n default: 3 failures, CaptchaRequired = false, want true")
	}
}

func TestLoginFailureCountForIPWindowExpired(t *testing.T) {
	const ip = "10.0.0.3"
	resetLoginFailures(ip)
	t.Cleanup(func() { resetLoginFailures(ip) })

	registerLoginFailure(ip)

	loginLimitMu.Lock()
	if rec, ok := loginLimitCache[ip]; ok {
		rec.windowStart = time.Now().Add(-loginLimitWindow - time.Minute)
	}
	loginLimitMu.Unlock()

	registerLoginFailure(ip)
	if got := LoginFailureCountForIP(ip); got != 1 {
		t.Fatalf("expected displayFailures to restart at 1, got %d", got)
	}
}
