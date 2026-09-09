package accessguard

import (
	"sync"
	"time"
)

// 下载访问防护：基于 (scope, IP) 的滑动窗口频率限制 + 失败计数锁定。
// 用于免认证的分享/临时下载接口，缓解在线暴力枚举与资源滥用。

// scope 常量
const (
	SCOPE_SHARE = "share" // 私有分享下载 /share/:code
	SCOPE_TEMP  = "temp"  // 临时文件下载 /temp/:code/download
)

const (
	defaultWindow       = 1 * time.Minute  // 统计窗口
	defaultRequestMax   = 120              // 窗口内允许的最大请求数（无论成败）
	defaultLockAfter    = 20               // 窗口内失败次数达到该值则锁定
	defaultLockDuration = 10 * time.Minute // 锁定持续时长
	defaultCleanup      = 5 * time.Minute  // 过期记录清理周期
)

type record struct {
	total       int       // 窗口内请求总数
	fail        int       // 窗口内失败数
	windowStart time.Time // 当前窗口起点
	lockUntil   time.Time // 锁定截止时间
}

var (
	guardMu sync.Mutex
	cache   = make(map[string]*record)
)

func init() {
	go func() {
		ticker := time.NewTicker(defaultCleanup)
		defer ticker.Stop()
		for range ticker.C {
			guardMu.Lock()
			now := time.Now()
			for k, rec := range cache {
				// 清理：锁定期已过，且窗口已过期
				if now.After(rec.lockUntil) && now.Sub(rec.windowStart) > defaultWindow {
					delete(cache, k)
				}
			}
			guardMu.Unlock()
		}
	}()
}

func scopeKey(scope, ip string) string {
	return scope + "|" + ip
}

func currentRecord(scope, ip string, now time.Time) *record {
	key := scopeKey(scope, ip)
	rec := cache[key]
	if rec == nil || now.Sub(rec.windowStart) > defaultWindow {
		rec = &record{windowStart: now}
		cache[key] = rec
	}
	return rec
}

// Acquire 在请求入口调用：返回 true 放行，false 拒绝（已被锁定或窗口内超频）。
// 每次调用在窗口内累计一次访问（无论成败），用于整体频率限制。
func Acquire(scope, ip string) bool {
	if scope == "" || ip == "" {
		return true
	}
	guardMu.Lock()
	defer guardMu.Unlock()

	now := time.Now()
	rec := currentRecord(scope, ip, now)
	if now.Before(rec.lockUntil) {
		return false
	}
	rec.total++
	return rec.total <= defaultRequestMax
}

// Fail 在业务判定失败（无效/不存在的码）时调用，累计失败计数；达到阈值即锁定该 IP。
func Fail(scope, ip string) {
	if scope == "" || ip == "" {
		return
	}
	guardMu.Lock()
	defer guardMu.Unlock()

	now := time.Now()
	rec := currentRecord(scope, ip, now)
	if now.Before(rec.lockUntil) {
		return
	}
	rec.fail++
	if rec.fail >= defaultLockAfter {
		rec.lockUntil = now.Add(defaultLockDuration)
		rec.fail = 0
		rec.total = 0
	}
}
