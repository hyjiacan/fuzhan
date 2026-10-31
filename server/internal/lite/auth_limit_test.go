package lite

import (
	"testing"
	"time"
)

func TestLiteLoginFailureCountForIP(t *testing.T) {
	const ip = "127.0.0.1"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	for i := 0; i < 3; i++ {
		liteRegisterFailure(ip)
	}
	if got := LiteLoginFailureCountForIP(ip); got != 3 {
		t.Fatalf("expected 3 failures, got %d", got)
	}
}

func TestLiteLoginFailureCountForIPNotResetByLock(t *testing.T) {
	const ip = "10.0.0.1"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	for i := 0; i < 3; i++ {
		liteRegisterFailure(ip)
	}
	// 再失败 3 次，累计 6 次，期间触发锁定并清零 count
	for i := 0; i < 3; i++ {
		liteRegisterFailure(ip)
	}
	if !liteIsLoginLocked(ip) {
		t.Fatal("expected ip to be locked")
	}
	if got := LiteLoginFailureCountForIP(ip); got != 6 {
		t.Fatalf("expected displayFailures == 6, got %d", got)
	}

	// 锁定期内再失败一次，累计值应递增且仍处于锁定状态
	liteRegisterFailure(ip)
	if got := LiteLoginFailureCountForIP(ip); got != 7 {
		t.Fatalf("expected displayFailures == 7 after extra failure, got %d", got)
	}
	if !liteIsLoginLocked(ip) {
		t.Fatal("expected ip to remain locked after extra failure")
	}
}

func TestLiteLoginFailureCountForIPReset(t *testing.T) {
	const ip = "10.0.0.2"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	liteRegisterFailure(ip)
	if got := LiteLoginFailureCountForIP(ip); got != 1 {
		t.Fatalf("expected 1 failure, got %d", got)
	}
	liteResetFailures(ip)
	if got := LiteLoginFailureCountForIP(ip); got != 0 {
		t.Fatalf("expected 0 after reset, got %d", got)
	}
}

func TestLiteLoginFailureCountForIPEmpty(t *testing.T) {
	if got := LiteLoginFailureCountForIP(""); got != 0 {
		t.Fatalf("expected 0 for empty ip, got %d", got)
	}
}

func TestLiteLoginFailureCountForIPWindowExpired(t *testing.T) {
	const ip = "10.0.0.3"
	liteResetFailures(ip)
	t.Cleanup(func() { liteResetFailures(ip) })

	liteRegisterFailure(ip)

	liteLimitMu.Lock()
	if rec, ok := liteLimitCache[ip]; ok {
		rec.windowStart = time.Now().Add(-liteLimitWindow - time.Minute)
	}
	liteLimitMu.Unlock()

	liteRegisterFailure(ip)
	if got := LiteLoginFailureCountForIP(ip); got != 1 {
		t.Fatalf("expected displayFailures to restart at 1, got %d", got)
	}
}
