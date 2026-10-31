package lite

import (
	"sync"
	"time"
)

// 轻量版登录/注册爆破防护：与 auth 包参数保持一致（窗口 15min、最多 5 次失败、锁定 15min）。
// auth 包内的防护函数为包私有无法复用，此处独立实现并只用于 /lite 认证端口。

const (
	liteLimitWindow   = 15 * time.Minute // 统计窗口
	liteMaxFailures   = 5                // 窗口内最大失败次数
	liteLockDuration  = 15 * time.Minute // 达到上限后的锁定时长
	liteCleanupPeriod = 5 * time.Minute  // 过期记录清理周期
)

type liteIPRecord struct {
	count           int
	displayFailures int
	windowStart     time.Time
	lockUntil       time.Time
}

var (
	liteLimitMu    sync.Mutex
	liteLimitCache = make(map[string]*liteIPRecord)
)

func init() {
	go func() {
		ticker := time.NewTicker(liteCleanupPeriod)
		defer ticker.Stop()
		for range ticker.C {
			liteLimitMu.Lock()
			now := time.Now()
			for ip, rec := range liteLimitCache {
				if now.After(rec.lockUntil) && (now.Sub(rec.windowStart) > liteLimitWindow || rec.count == 0) {
					delete(liteLimitCache, ip)
				}
			}
			liteLimitMu.Unlock()
		}
	}()
}

// liteIsLoginLocked 返回指定 IP 当前是否处于锁定状态
func liteIsLoginLocked(ip string) bool {
	if ip == "" {
		return false
	}
	liteLimitMu.Lock()
	defer liteLimitMu.Unlock()
	rec, ok := liteLimitCache[ip]
	if !ok {
		return false
	}
	return time.Now().Before(rec.lockUntil)
}

// liteRegisterFailure 记录一次失败，返回该 IP 是否因此进入锁定
func liteRegisterFailure(ip string) bool {
	if ip == "" {
		return false
	}
	liteLimitMu.Lock()
	defer liteLimitMu.Unlock()

	now := time.Now()
	rec, ok := liteLimitCache[ip]
	if !ok || now.Sub(rec.windowStart) > liteLimitWindow {
		rec = &liteIPRecord{windowStart: now}
		liteLimitCache[ip] = rec
	}
	rec.count++
	rec.displayFailures++
	if now.Before(rec.lockUntil) {
		// 仍在锁定期内，继续失败计数并保持锁定状态
		return true
	}
	if rec.count >= liteMaxFailures {
		rec.lockUntil = now.Add(liteLockDuration)
		rec.count = 0
		return true
	}
	return false
}

// liteResetFailures 登录成功后清除该 IP 的失败记录
func liteResetFailures(ip string) {
	if ip == "" {
		return
	}
	liteLimitMu.Lock()
	delete(liteLimitCache, ip)
	liteLimitMu.Unlock()
}

// LiteLoginFailureCountForIP 返回指定 IP 当前窗口内的累计失败次数（lite 验证码触发用）。
// 窗口过期视为 0（清理 ticker 之前也不返回旧值）。
func LiteLoginFailureCountForIP(ip string) int {
	if ip == "" {
		return 0
	}
	liteLimitMu.Lock()
	defer liteLimitMu.Unlock()
	rec, ok := liteLimitCache[ip]
	if !ok {
		return 0
	}
	if time.Now().Sub(rec.windowStart) > liteLimitWindow {
		return 0
	}
	return rec.displayFailures
}
