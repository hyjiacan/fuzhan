package accessguard

import (
	"sync"
	"time"

	"fuzhan/internal/appconfig"
)

// 下载访问防护：基于 (scope, IP) 的滑动窗口频率限制 + 失败计数锁定。
// 用于免认证的分享/临时下载接口，缓解在线暴力枚举与资源滥用。
// 频率限制与锁定阈值均实时读取下载限流配置（appconfig.GetDownloadRateLimit），
// 支持配置热更新；MaxRequests<=0 表示不对请求频率做限制，LockAfter<=0 表示不锁定。

// scope 常量
const (
	SCOPE_SHARE  = "share"  // 私有分享下载 /share/:code
	SCOPE_TEMP   = "temp"   // 临时文件下载 /temp/:code/download
	SCOPE_FTP    = "ftp"    // 匿名 FTP 下载（RETR）
	SCOPE_WEBDAV = "webdav" // WebDAV（GET 下载限流 与 Basic Auth 失败锁定）
)

const (
	defaultWindow       = 1 * time.Minute  // 统计窗口（配置未设或 <=0 时使用）
	defaultLockDuration = 10 * time.Minute // 锁定持续时长（配置未设或 <=0 时使用）
	defaultCleanup      = 5 * time.Minute  // 过期记录清理周期
)

type record struct {
	total       int           // 窗口内请求总数
	fail        int           // 窗口内失败数
	windowStart time.Time     // 当前窗口起点
	windowDur   time.Duration // 当前滑动窗口时长
	lockUntil   time.Time     // 锁定截止时间
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
				if now.After(rec.lockUntil) && now.Sub(rec.windowStart) > rec.windowDur {
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

func currentRecord(scope, ip string, now time.Time, windowDur time.Duration) *record {
	key := scopeKey(scope, ip)
	rec := cache[key]
	if rec == nil || now.Sub(rec.windowStart) > windowDur {
		rec = &record{windowStart: now, windowDur: windowDur}
		cache[key] = rec
	}
	return rec
}

// Acquire 在请求入口调用：返回 true 放行，false 拒绝（已被锁定或窗口内超频）。
// 每次调用在窗口内累计一次访问（无论成败），用于整体频率限制。
// MaxRequests<=0 时不做频率限制，直接放行（不支持热开启，需重启或配置后生效）。
func Acquire(scope, ip string) bool {
	if scope == "" || ip == "" {
		return true
	}
	cfg := appconfig.GetDownloadRateLimit()
	if cfg.MaxRequests <= 0 {
		return true
	}
	windowDur := configWindow(cfg)

	guardMu.Lock()
	defer guardMu.Unlock()

	now := time.Now()
	rec := currentRecord(scope, ip, now, windowDur)
	if now.Before(rec.lockUntil) {
		return false
	}
	rec.total++
	return rec.total <= cfg.MaxRequests
}

// IsLocked 返回该 (scope, IP) 当前是否处于锁定期（用于入口预检，不消耗配额）。
func IsLocked(scope, ip string) bool {
	if scope == "" || ip == "" {
		return false
	}
	guardMu.Lock()
	defer guardMu.Unlock()
	rec := cache[scopeKey(scope, ip)]
	return rec != nil && time.Now().Before(rec.lockUntil)
}

// Fail 在业务判定失败（无效/不存在的码）时调用，累计失败计数；达到阈值即锁定该 IP。
// LockAfter<=0 时不锁定。
func Fail(scope, ip string) {
	if scope == "" || ip == "" {
		return
	}
	cfg := appconfig.GetDownloadRateLimit()
	if cfg.LockAfter <= 0 {
		return
	}
	windowDur := configWindow(cfg)
	lockDur := configLockDuration(cfg)

	guardMu.Lock()
	defer guardMu.Unlock()

	now := time.Now()
	rec := currentRecord(scope, ip, now, windowDur)
	if now.Before(rec.lockUntil) {
		return
	}
	rec.fail++
	if rec.fail >= cfg.LockAfter {
		rec.lockUntil = now.Add(lockDur)
		rec.fail = 0
		rec.total = 0
	}
}

func configWindow(cfg appconfig.DownloadRateLimitConfig) time.Duration {
	windowDur := time.Duration(cfg.WindowMinutes) * time.Minute
	if windowDur <= 0 {
		windowDur = defaultWindow
	}
	return windowDur
}

func configLockDuration(cfg appconfig.DownloadRateLimitConfig) time.Duration {
	lockDur := time.Duration(cfg.LockMinutes) * time.Minute
	if lockDur <= 0 {
		lockDur = defaultLockDuration
	}
	return lockDur
}
