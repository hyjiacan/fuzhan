package auth

import (
	"testing"
	"time"
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
	if got := LoginFailureCountForIP(ip); got < 5 {
		t.Fatalf("expected displayFailures >= 5, got %d", got)
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
