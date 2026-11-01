package security

import (
	"sync"
	"testing"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
)

// fakeEventStore 内存安全事件存储：仅实现写入器用到的 CreateBatch，其余方法走嵌入接口（未初始化即 panic 暴露误用）。
type fakeEventStore struct {
	repositories.SecurityEventStore
	mu     sync.Mutex
	events []*models.SecurityEvent
}

func (f *fakeEventStore) CreateBatch(evs []*models.SecurityEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, evs...)
	return nil
}

func (f *fakeEventStore) all() []*models.SecurityEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*models.SecurityEvent, len(f.events))
	copy(out, f.events)
	return out
}

// setDDoSConfig 改写 DDoS 配置并返回还原函数
func setDDoSConfig(enabled bool, windowSec, maxReq, cooldownMin int) func() {
	appconfig.LockConfig()
	old := appconfig.GlobalConfig.DDos
	appconfig.GlobalConfig.DDos = appconfig.DDosConfig{
		Enabled: enabled, WindowSeconds: windowSec, MaxRequests: maxReq, CooldownMinutes: cooldownMin,
	}
	appconfig.UnlockConfig()
	return func() {
		appconfig.LockConfig()
		appconfig.GlobalConfig.DDos = old
		appconfig.UnlockConfig()
	}
}

// newTestWriter 初始化全局写入器并返回「停止刷盘」函数（Stop 幂等，可重复调用）
func newTestWriter() (*fakeEventStore, func()) {
	store := &fakeEventStore{}
	InitWriter(store, 1, 256)
	return store, StopWriter
}

func TestDDoS_Disabled_NoEvents(t *testing.T) {
	restore := setDDoSConfig(false, 60, 3, 10)
	defer restore()
	store, stop := newTestWriter()

	for i := 0; i < 50; i++ {
		if DDoSObserve("10.0.0.1") {
			t.Fatalf("未启用时不应返回限流拦截（应为 false）")
		}
	}
	stop()

	if got := store.all(); len(got) != 0 {
		t.Fatalf("未启用时不应产生任何安全事件，got %d 条", len(got))
	}
}

// TestDDoS_OverThreshold_BlocksAndAlerts 验证超阈值后 DDoSObserve 返回 true（调用方限流拦截），
// 且冷却去重后恰好记 1 条高危安全事件。
func TestDDoS_OverThreshold_BlocksAndAlerts(t *testing.T) {
	restore := setDDoSConfig(true, 3600, 3, 10)
	defer restore()
	store, stop := newTestWriter()

	// 阈值 3：前 3 次放行，第 4 次起（含冷却期内）应持续返回 true
	wantBlocked := []bool{false, false, false, true, true, true, true, true, true, true}
	for i, want := range wantBlocked {
		if got := DDoSObserve("10.0.0.7"); got != want {
			t.Fatalf("第 %d 次观测：限流结果应为 %v，实际 %v", i+1, want, got)
		}
	}
	stop()

	events := store.all()
	if len(events) != 1 {
		t.Fatalf("超阈值且冷却去重后应恰好 1 条告警，got %d 条", len(events))
	}
	ev := events[0]
	if ev.EventType != models.EventDDoSSuspected || ev.Severity != models.SeverityHigh ||
		ev.Guard != models.GuardDDoS || ev.Operation != "http.flood" || ev.ClientIP != "10.0.0.7" {
		t.Fatalf("告警事件维度异常：%+v", ev)
	}
}

func TestDDoS_OverThreshold_ExactlyOneAlert(t *testing.T) {
	restore := setDDoSConfig(true, 3600, 3, 10)
	defer restore()
	store, stop := newTestWriter()

	for i := 0; i < 10; i++ {
		DDoSObserve("10.0.0.2")
	}
	stop()

	events := store.all()
	if len(events) != 1 {
		t.Fatalf("超阈值且冷却去重后应恰好 1 条告警，got %d 条", len(events))
	}
	ev := events[0]
	if ev.EventType != models.EventDDoSSuspected || ev.Severity != models.SeverityHigh ||
		ev.Guard != models.GuardDDoS || ev.Operation != "http.flood" || ev.ClientIP != "10.0.0.2" {
		t.Fatalf("告警事件维度异常：%+v", ev)
	}
}

func TestDDoS_WindowReset_CanAlertAgain(t *testing.T) {
	restore := setDDoSConfig(true, 1, 3, 10)
	defer restore()
	store, stop := newTestWriter()

	for i := 0; i < 4; i++ {
		DDoSObserve("10.0.0.3")
	}
	// 人工回拨窗口起点与冷却时间，模拟「窗口已重置、冷却已过」
	ddosMu.Lock()
	e := ddosEntries["10.0.0.3"]
	if e == nil {
		ddosMu.Unlock()
		t.Fatal("观测后应存在该 IP 的计数条目")
	}
	e.windowStart = utils.Now().Add(-2 * time.Second)
	e.lastAlert = utils.Now().Add(-11 * time.Minute)
	ddosMu.Unlock()

	for i := 0; i < 4; i++ {
		DDoSObserve("10.0.0.3")
	}
	stop()

	if got := len(store.all()); got != 2 {
		t.Fatalf("窗口重置且冷却过期后应再次告警（共 2 条），got %d 条", got)
	}
}

func TestDDoS_EmptyIP_NoPanic(t *testing.T) {
	restore := setDDoSConfig(true, 60, 3, 10)
	defer restore()
	DDoSObserve("")
}
