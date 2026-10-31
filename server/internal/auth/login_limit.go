package auth

import (
	"strings"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
)

// 登录/注册爆破防护：基于客户端 IP 的滑动窗口失败计数 + 锁定
// 同一 IP 在窗口内失败达到上限后，锁定一段时间，缓解暴力破解风险

const (
	loginLimitWindow   = 15 * time.Minute // 统计窗口
	loginMaxFailures   = 5                // 窗口内最大失败次数
	loginLockDuration  = 15 * time.Minute // 达到上限后的锁定时长
	loginCleanupPeriod = 5 * time.Minute  // 过期记录清理周期
)

type ipLoginRecord struct {
	count           int
	displayFailures int
	windowStart     time.Time
	lockUntil       time.Time
}

var (
	loginLimitMu    sync.Mutex
	loginLimitCache = make(map[string]*ipLoginRecord)
)

func init() {
	go func() {
		ticker := time.NewTicker(loginCleanupPeriod)
		defer ticker.Stop()
		for range ticker.C {
			loginLimitMu.Lock()
			now := time.Now()
			for ip, rec := range loginLimitCache {
				// 清空：已过锁定期的记录，或窗口已过期且计数不再增长
				if now.After(rec.lockUntil) && (now.Sub(rec.windowStart) > loginLimitWindow || rec.count == 0) {
					delete(loginLimitCache, ip)
				}
			}
			loginLimitMu.Unlock()
		}
	}()
}

// isLoginLocked 返回指定 IP 当前是否处于锁定状态
func isLoginLocked(ip string) bool {
	if ip == "" {
		return false
	}
	loginLimitMu.Lock()
	defer loginLimitMu.Unlock()
	rec, ok := loginLimitCache[ip]
	if !ok {
		return false
	}
	if time.Now().Before(rec.lockUntil) {
		return true
	}
	return false
}

// registerLoginFailure 记录一次失败，返回该 IP 是否因此进入锁定
func registerLoginFailure(ip string) bool {
	if ip == "" {
		return false
	}
	loginLimitMu.Lock()
	defer loginLimitMu.Unlock()

	now := time.Now()
	rec, ok := loginLimitCache[ip]
	if !ok || now.Sub(rec.windowStart) > loginLimitWindow {
		rec = &ipLoginRecord{windowStart: now}
		loginLimitCache[ip] = rec
	}
	rec.count++
	rec.displayFailures++
	if now.Before(rec.lockUntil) {
		// 仍在锁定期内，继续失败计数并保持锁定状态
		return true
	}
	if rec.count >= loginMaxFailures {
		rec.lockUntil = now.Add(loginLockDuration)
		rec.count = 0
		return true
	}
	return false
}

// resetLoginFailures 登录成功后清除该 IP 的失败记录
func resetLoginFailures(ip string) {
	if ip == "" {
		return
	}
	loginLimitMu.Lock()
	delete(loginLimitCache, ip)
	loginLimitMu.Unlock()
}

// LoginFailureCountForIP 返回指定 IP 当前窗口内的累计失败次数。
// 与锁定计数 count 不同：count 在触发锁定时会归零，本字段保留累计值，
// 供图形验证码「失败 N 次后开启」判定与前端展示使用。
// 窗口过期视为 0（清理 ticker 之前也不返回旧值）。
func LoginFailureCountForIP(ip string) int {
	if ip == "" {
		return 0
	}
	loginLimitMu.Lock()
	defer loginLimitMu.Unlock()
	rec, ok := loginLimitCache[ip]
	if !ok {
		return 0
	}
	if time.Now().Sub(rec.windowStart) > loginLimitWindow {
		return 0
	}
	return rec.displayFailures
}

// CaptchaRequired 判定指定 IP 当前是否需要图形验证码。
// 三态语义：disabled 不需要；always 始终需要；after_n 在当前窗口累计失败达到
// TriggerAfter（<=0 时按默认 3）后需要。供 options 展示与 Web 登录/注册前置校验共用。
func CaptchaRequired(ip string) bool {
	cfg := appconfig.GetConfig()
	switch strings.ToLower(strings.TrimSpace(cfg.Account.Captcha.Mode)) {
	case "always":
		return true
	case "after_n":
		threshold := cfg.Account.Captcha.TriggerAfter
		if threshold <= 0 {
			threshold = appconfig.DefaultCaptchaTriggerAfter
		}
		return LoginFailureCountForIP(ip) >= threshold
	default: // disabled / 空值 / 未知值一律不要求，保证默认零行为影响
		return false
	}
}
