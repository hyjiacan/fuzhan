package webhook

import (
	"sync"

	"fuzhan/internal/appconfig"
)

// 全局派发器单例：业务各处仅需调用 Dispatch/TestSend，
// 由 bootstrap 在启动阶段初始化，Server 主循环驱动 Run(ctx)，退出时 Stop。
var (
	globalMu sync.RWMutex
	global   *Dispatcher
)

// Init 初始化全局 Webhook 派发器（启动阶段调用一次）。
func Init(cfg appconfig.WebhookConfig) *Dispatcher {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = NewDispatcher(cfg)
	return global
}

// Get 返回全局派发器；未初始化时返回 nil。
// 调用方应使用 Dispatch（内部做空值保护），仅在需要直接操作实例时使用本函数。
func Get() *Dispatcher {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// Dispatch 向全局派发器提交一条事件。未初始化/未启用/队列满载时静默返回 false。
// 各触发点（任务完成/失败、安全事件、配置保存等）统一经此入口，避免各自判空。
func Dispatch(eventType, source string, payload interface{}) bool {
	globalMu.RLock()
	d := global
	globalMu.RUnlock()
	if d == nil {
		return false
	}
	return d.Send(eventType, source, payload)
}

// Enabled 返回当前是否启用 Webhook 推送（供触发点低成本短路）。
func Enabled() bool {
	globalMu.RLock()
	d := global
	globalMu.RUnlock()
	return d != nil && d.Enabled()
}

// Sync 配置热更新：同步全局派发器的启用开关（配置保存后由 AddOnSaveHook 触发）。
func Sync(cfg appconfig.WebhookConfig) {
	globalMu.RLock()
	d := global
	globalMu.RUnlock()
	if d != nil {
		d.Sync(cfg)
	}
}

// Stop 停止全局派发器并等待已排队事件排空（进程退出/热重启时调用，幂等）。
func Stop() {
	globalMu.RLock()
	d := global
	globalMu.RUnlock()
	if d != nil {
		d.Stop()
	}
}
