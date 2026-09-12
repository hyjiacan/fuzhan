package accessguard

import (
	"sync"
	"testing"

	"fuzhan/internal/appconfig"
)

func setRateLimit(t *testing.T, ml appconfig.DownloadRateLimitConfig) {
	t.Helper()
	appconfig.LockConfig()
	appconfig.GlobalConfig.Download.RateLimit = ml
	appconfig.UnlockConfig()
}

func TestAcquireUnlimitedWhenMaxRequestsZero(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   0, // 0 = 不限制
		LockAfter:     0,
		LockMinutes:   10,
	})
	for i := 0; i < 1000; i++ {
		if !Acquire(SCOPE_SHARE, "1.2.3.4") {
			t.Fatalf("MaxRequests=0 时不应触发限流，第 %d 次被拒绝", i)
		}
	}
}

func TestAcquireBlocksOverLimit(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   3,
		LockAfter:     0,
		LockMinutes:   10,
	})
	// 第 1~3 次放行，第 4 次拒绝
	if !Acquire(SCOPE_SHARE, "1.2.3.4") || !Acquire(SCOPE_SHARE, "1.2.3.4") || !Acquire(SCOPE_SHARE, "1.2.3.4") {
		t.Fatal("前 3 次请求应放行")
	}
	if Acquire(SCOPE_SHARE, "1.2.3.4") {
		t.Fatal("超过 max_requests 后应被拒绝")
	}
	// 不同 IP / 不同 scope 互不影响
	if !Acquire(SCOPE_SHARE, "5.6.7.8") || !Acquire(SCOPE_TEMP, "1.2.3.4") {
		t.Fatal("不同 IP 或不同 scope 应独立计数")
	}
}

func TestFailLocksWhenLockAfterReached(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   1000,
		LockAfter:     2,
		LockMinutes:   10,
	})
	ip := "9.9.9.9"
	Fail(SCOPE_SHARE, ip) // fail=1
	Fail(SCOPE_SHARE, ip) // fail=2 -> 锁定
	if Acquire(SCOPE_SHARE, ip) {
		t.Fatal("达到 lock_after 阈值后应被锁定，Acquire 应返回 false")
	}
}

func TestIsLockedReflectsFailLock(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   1000,
		LockAfter:     2,
		LockMinutes:   10,
	})
	ip := "6.6.6.6"
	if IsLocked(SCOPE_WEBDAV, ip) {
		t.Fatal("初始状态不应锁定")
	}
	Fail(SCOPE_WEBDAV, ip)
	if IsLocked(SCOPE_WEBDAV, ip) {
		t.Fatal("未达阈值不应锁定")
	}
	Fail(SCOPE_WEBDAV, ip)
	if !IsLocked(SCOPE_WEBDAV, ip) {
		t.Fatal("达到阈值后 IsLocked 应返回 true")
	}
}

func TestFTPScopeAcquireRespectsLimit(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   2,
		LockAfter:     0,
		LockMinutes:   10,
	})
	ip := "10.20.30.40"
	if !Acquire(SCOPE_FTP, ip) || !Acquire(SCOPE_FTP, ip) {
		t.Fatal("前 2 次匿名 FTP 下载应放行")
	}
	if Acquire(SCOPE_FTP, ip) {
		t.Fatal("超阈值后匿名 FTP 下载应被拒绝")
	}
}

func TestNoLockWhenLockAfterZero(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   1000,
		LockAfter:     0, // 0 = 不锁定
		LockMinutes:   10,
	})
	ip := "8.8.8.8"
	for i := 0; i < 50; i++ {
		Fail(SCOPE_SHARE, ip)
	}
	if !Acquire(SCOPE_SHARE, ip) {
		t.Fatal("lock_after=0 时不应锁定，Acquire 应始终放行")
	}
}

// 并发场景：所有请求不会被同一 goroutine 序列化问题破坏，且不会出现数据竞争。
func TestAcquireConcurrent(t *testing.T) {
	setRateLimit(t, appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1,
		MaxRequests:   1,
		LockAfter:     0,
		LockMinutes:   10,
	})
	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if Acquire(SCOPE_TEMP, "7.7.7.7") {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed < 1 || allowed > 2 {
		t.Fatalf("MaxRequests=1 时并发仅允许约 1 次放行，实际允许 %d 次", allowed)
	}
}
