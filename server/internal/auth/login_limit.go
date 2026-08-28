package auth

import (
    "sync"
    "time"
)

// 登录/注册爆破防护：基于客户端 IP 的滑动窗口失败计数 + 锁定
// 同一 IP 在窗口内失败达到上限后，锁定一段时间，缓解暴力破解风险

const (
    loginLimitWindow    = 15 * time.Minute // 统计窗口
    loginMaxFailures    = 5                // 窗口内最大失败次数
    loginLockDuration   = 15 * time.Minute // 达到上限后的锁定时长
    loginCleanupPeriod  = 5 * time.Minute  // 过期记录清理周期
)

type ipLoginRecord struct {
    count      int
    windowStart time.Time
    lockUntil  time.Time
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
    if now.Before(rec.lockUntil) {
        // 仍在锁定期内，继续失败延长锁定
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