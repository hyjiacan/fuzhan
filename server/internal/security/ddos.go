package security

import (
	"fmt"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// 高频/DDoS 检测器：按 IP 固定窗口计数，超阈值记 ddos.suspected 事件（仅告警不阻断）。
// 配置实时读取 appconfig.GetDDoSConfig，支持配置保存后热启停（SyncDDoSDetector）。

// DDoS 检测默认参数（配置项 <=0 时使用）
const (
	ddosDefaultWindow   = 10 * time.Second // 统计窗口默认 10 秒
	ddosDefaultMaxReqs  = 200              // 窗口内请求数阈值默认 200
	ddosDefaultCooldown = 10 * time.Minute // 告警冷却默认 10 分钟
	ddosCleanupInterval = 1 * time.Minute  // 过期条目清理周期
	ddosMaxEntries      = 10000            // 跟踪条目硬上限，保证内存有界
)

// ddosEntry 单 IP 的检测状态
type ddosEntry struct {
	count       int       // 当前窗口内请求数
	windowStart time.Time // 当前窗口起点
	lastAlert   time.Time // 上次告警时间（冷却去重用）
}

var (
	ddosMu      sync.Mutex
	ddosEntries map[string]*ddosEntry
	ddosRunning bool
	ddosStopCh  chan struct{}
)

// ddosParams 解析后的生效参数（配置缺省回退默认值）
type ddosParams struct {
	window   time.Duration
	maxReqs  int
	cooldown time.Duration
}

// resolveDDoSParams 读取配置并填充默认值
func resolveDDoSParams() ddosParams {
	cfg := appconfig.GetDDoSConfig()
	p := ddosParams{
		window:   ddosDefaultWindow,
		maxReqs:  ddosDefaultMaxReqs,
		cooldown: ddosDefaultCooldown,
	}
	if cfg.WindowSeconds > 0 {
		p.window = time.Duration(cfg.WindowSeconds) * time.Second
	}
	if cfg.MaxRequests > 0 {
		p.maxReqs = cfg.MaxRequests
	}
	if cfg.CooldownMinutes > 0 {
		p.cooldown = time.Duration(cfg.CooldownMinutes) * time.Minute
	}
	return p
}

// InitDDoSDetector 启动阶段初始化检测器：未启用仅中文日志提示，不启动协程。
func InitDDoSDetector() {
	if !appconfig.GetDDoSConfig().Enabled {
		utils.Info("DDoS 高频检测未启用")
		return
	}
	startDDoSDetector()
}

// SyncDDoSDetector 配置保存钩子：按最新 enabled 状态启停检测器。
func SyncDDoSDetector() {
	enabled := appconfig.GetDDoSConfig().Enabled
	ddosMu.Lock()
	running := ddosRunning
	ddosMu.Unlock()
	switch {
	case enabled && !running:
		startDDoSDetector()
	case !enabled && running:
		StopDDoSDetector()
	}
}

// startDDoSDetector 启动检测协程（幂等）
func startDDoSDetector() {
	ddosMu.Lock()
	if ddosRunning {
		ddosMu.Unlock()
		return
	}
	ddosRunning = true
	ddosEntries = make(map[string]*ddosEntry)
	ddosStopCh = make(chan struct{})
	stopCh := ddosStopCh
	ddosMu.Unlock()

	p := resolveDDoSParams()
	utils.Info("DDoS 高频检测已启动（仅告警不阻断）",
		utils.Int("window_seconds", int(p.window.Seconds())),
		utils.Int("max_requests", p.maxReqs),
		utils.Int("cooldown_minutes", int(p.cooldown.Minutes())))
	go ddosCleanupLoop(stopCh)
}

// StopDDoSDetector 停止检测协程并释放计数表
func StopDDoSDetector() {
	ddosMu.Lock()
	if ddosRunning {
		ddosRunning = false
		close(ddosStopCh)
		ddosEntries = nil
	}
	ddosMu.Unlock()
}

// ddosCleanupLoop 周期清理窗口与冷却均已过期的条目，防止 IP 表无限增长
func ddosCleanupLoop(stopCh chan struct{}) {
	ticker := time.NewTicker(ddosCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p := resolveDDoSParams()
			now := utils.Now()
			ddosMu.Lock()
			for ip, e := range ddosEntries {
				if now.Sub(e.windowStart) >= p.window && now.Sub(e.lastAlert) >= p.cooldown {
					delete(ddosEntries, ip)
				}
			}
			ddosMu.Unlock()
		case <-stopCh:
			return
		}
	}
}

// DDoSObserve 观测一条请求（监控中间件全量调用）。
// 检测器未启用时零开销直接返回；超阈值且过冷却时记一条 ddos.suspected 高危事件，
// 仅告警不阻断请求。
func DDoSObserve(ip string) {
	if ip == "" || !appconfig.GetDDoSConfig().Enabled {
		return
	}
	p := resolveDDoSParams()
	now := utils.Now()

	ddosMu.Lock()
	if ddosEntries == nil {
		// 配置刚启用、检测协程尚未完成初始化时兜底建表，保证计数不 panic
		ddosEntries = make(map[string]*ddosEntry)
	}
	e := ddosEntries[ip]
	if e == nil {
		if len(ddosEntries) >= ddosMaxEntries {
			// 条目达硬上限：先清一轮过期项，仍满则放弃跟踪该 IP（内存有界优先）
			for k, v := range ddosEntries {
				if now.Sub(v.windowStart) >= p.window && now.Sub(v.lastAlert) >= p.cooldown {
					delete(ddosEntries, k)
				}
			}
			if len(ddosEntries) >= ddosMaxEntries {
				ddosMu.Unlock()
				return
			}
		}
		e = &ddosEntry{windowStart: now}
		ddosEntries[ip] = e
	}
	if now.Sub(e.windowStart) >= p.window {
		e.windowStart = now
		e.count = 0
	}
	e.count++
	alert := false
	count := e.count
	if e.count > p.maxReqs && now.Sub(e.lastAlert) >= p.cooldown {
		e.lastAlert = now
		alert = true
	}
	ddosMu.Unlock()

	if alert {
		Record(&models.SecurityEvent{
			EventType: models.EventDDoSSuspected,
			Severity:  models.SeverityHigh,
			ClientIP:  ip,
			Identity:  "anonymous",
			Operation: "http.flood",
			Target:    ip,
			Guard:     models.GuardDDoS,
			Reason: fmt.Sprintf("%d 秒窗口内检测到该 IP 请求 %d 次（阈值 %d），疑似高频/DDoS 攻击（仅告警不阻断）",
				int(p.window.Seconds()), count, p.maxReqs),
		})
	}
}
