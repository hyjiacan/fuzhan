package auth

import (
	"strings"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
)

// 登录/注册爆破防护：基于客户端 IP 与账号（用户名）的滑动窗口失败计数 + 锁定
// 支持两套维度：独立 IP 与 IP+用户名 组合。
// 任一维度在窗口内失败达到上限后，锁定一段时间，缓解暴力破解风险；
// 不存在的账号名也会按 IP+用户名 组合维度入缓存并被锁定（无需预先建号）。
// 缓存用 map 即可，key 约定：独立 IP 直接用 ip 字符串（保持旧版 key，兼容既有行为），
// 组合维度用 ip + "|" + 用户名。

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

// isLoginLockedByUsername 返回指定 (IP, 用户名) 当前是否处于锁定状态。
// 锁定判定顺序：先查 IP 锁，再查 IP+用户名 组合锁；任一命中即视为锁定。
func isLoginLockedByUsername(ip, username string) bool {
	if isLoginLocked(ip) {
		return true
	}
	username = strings.TrimSpace(username)
	if ip == "" || username == "" {
		return false
	}
	loginLimitMu.Lock()
	defer loginLimitMu.Unlock()
	if rec, ok := loginLimitCache[ipUsernameLoginKey(ip, username)]; ok && time.Now().Before(rec.lockUntil) {
		return true
	}
	return false
}

// ipUsernameLoginKey 组装 IP+用户名 组合维度的缓存 key
func ipUsernameLoginKey(ip, username string) string {
	return ip + "|" + strings.TrimSpace(username)
}

// recordLoginFailureByKey 在指定 key 上记录一次失败，返回该 key 是否因此进入锁定。
// IP 与 IP+用户名 两套维度共用同一滑动窗口与锁定逻辑。
func recordLoginFailureByKey(key string) bool {
	if key == "" {
		return false
	}
	loginLimitMu.Lock()
	defer loginLimitMu.Unlock()

	now := time.Now()
	rec, ok := loginLimitCache[key]
	if !ok || now.Sub(rec.windowStart) > loginLimitWindow {
		rec = &ipLoginRecord{windowStart: now}
		loginLimitCache[key] = rec
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

// registerLoginFailure 记录一次失败（IP 维度），返回该 IP 是否因此进入锁定
func registerLoginFailure(ip string) bool {
	if ip == "" {
		return false
	}
	return recordLoginFailureByKey(ip)
}

// registerLoginFailureByUsername 记录一次失败（IP+用户名 组合维度），
// 返回该组合是否因此进入锁定。不存在的账号名同样会入缓存并被锁定，
// 从而拦截针对未知账号的暴力破解尝试（无需预先建号）。
func registerLoginFailureByUsername(ip, username string) bool {
	username = strings.TrimSpace(username)
	if ip == "" || username == "" {
		return false
	}
	return recordLoginFailureByKey(ipUsernameLoginKey(ip, username))
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

// resetLoginFailuresByUsername 登录/注册成功后清除 (IP, 用户名) 组合维度的失败记录，
// 避免合法用户被之前该账号的失败误锁。
func resetLoginFailuresByUsername(ip, username string) {
	username = strings.TrimSpace(username)
	if ip == "" || username == "" {
		return
	}
	loginLimitMu.Lock()
	delete(loginLimitCache, ipUsernameLoginKey(ip, username))
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
