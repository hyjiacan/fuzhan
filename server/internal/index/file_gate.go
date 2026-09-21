package index

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// settleState 文件写入稳定判定状态。
type settleState int

const (
	// stateObserving 初始/仍在变化，需继续观察
	stateObserving settleState = iota
	// stateNeedMore 已连续稳定，但次数未达标，继续确认
	stateNeedMore
	// stateSettled 判定为已写完
	stateSettled
)

// SettledGate 独立的"文件是否写完"判定状态机。
// 监听层（watcher）只负责上报原生 Create/Write 事件，是否写完由本状态机统一判定，
// 建索引与触发 hash 统一从 onSettled 回调流出，不因入口差异而分化。
//
// 判定推进 = 定时 poll + 事件唤醒：poll 会把一次 os.Stat 的结果喂入状态机。
// 文件写完即不再产生写事件，因此不能纯靠事件推进，必须自持定时器（见边界 1）。
// 指标为 size 与 mtime 双指标，任一变化即重置连续稳定计数。
//
// 已覆盖边界（对应实现注释）：
//  1. 写完即静默 → 自持定时器 poll 推进
//  2. 已写完又追加/覆盖写 → 依赖保留期内再次 OnChange 触发 onDirty 回调
//  3. 瞬时小文件/空文件 → 首次 stat 仅记录基线（observed 哨兵），不参与稳定计数
//  4. 目录 → fast-path 直接判定写完
//  5. 写中断残留半截、force 强制 settle 读到半截 → 本层无法消除，由 hash 侧算前算后 size 校验兜底
//  6. 观察期被删/重命名 → stat 报 IsNotExist，终止该路径
//  7. stat 暂时失败(权限/IO) → 视为"暂不可观测"，重试，绝不作为稳定；连续失败达 MaxStatErrors 放弃
//  8. 持续写入防活锁 → 超过 ForceTimeout 强制判定写完（正确性交由 hash 兜底）
//  9. 并发写同一路径 → 每路径一条目，均在互斥锁保护下转移状态
//
// 10. 生命周期 → Cancel 公开终止；SettledRetention 到期清理条目，避免 map 膨胀
//
// 线程安全：公开方法可在任意 goroutine 调用；回调在锁外触发。配置字段应在
// 首次 OnChange 之前设定，运行期不再修改。
type SettledGate struct {
	// rootNames 根目录名 → 根目录路径（用于拼接 stat 路径）
	rootNames map[string]string

	// RequiredStable 需连续 N 次 size/mtime 均不变才判定写完（须 >=1）
	RequiredStable int
	// PollInterval 观察/清理轮询间隔（<=0 表示纯事件驱动，不自动 poll）
	PollInterval time.Duration
	// ForceTimeout 从开始观察到强制判定写完的最长等待，防止持续写入活锁（<=0 表示不启用）
	ForceTimeout time.Duration
	// SettledRetention 判定写完后的条目保留时长，期间再次写入可回调 onDirty（<=0 表示立即清理、不检测后续写入）
	SettledRetention time.Duration
	// MaxStatErrors 连续 stat 失败达到该次数后放弃该路径（<=0 表示永不放弃、一直重试）
	MaxStatErrors int

	// statFn 读取文件属性，可注入以便测试
	statFn func(path string) (os.FileInfo, error)

	onSettled func(rootName, relPath string) // 判定写完回调
	onDirty   func(rootName, relPath string) // 已写完又被写入回调（可空）

	mu      sync.Mutex
	entries map[string]*settleEntry
}

// settleEntry 单个路径的判定状态。
type settleEntry struct {
	rootName string
	relPath  string

	state       settleState
	observed    bool // 是否已完成首次基线 stat
	lastSize    int64
	lastModTime time.Time
	stableCount int
	statErrs    int // 连续 stat 失败次数
	created     time.Time
	settledAt   time.Time
	timer       *time.Timer
}

// NewSettledGate 创建文件写完判定状态机。
// rootNames 为根目录映射；onSettled 在判定写完时回调；onDirty 可选，用于
// "已写完又被写入"时通知下游（如把 hash 状态置回 pending）。两者均可为空，
// 为空时不产生副作用。
func NewSettledGate(rootNames map[string]string, onSettled, onDirty func(string, string)) *SettledGate {
	return &SettledGate{
		rootNames:        rootNames,
		RequiredStable:   3,
		PollInterval:     2 * time.Second,
		ForceTimeout:     30 * time.Second,
		SettledRetention: 30 * time.Second,
		MaxStatErrors:    5,
		statFn:           os.Stat,
		onSettled:        onSettled,
		onDirty:          onDirty,
		entries:          make(map[string]*settleEntry),
	}
}

func (g *SettledGate) key(rootName, relPath string) string {
	return rootName + ":" + relPath
}

// OnChange 上报一次文件写事件（Create/Write）。
// 若该路径此前已判定写完且仍在保留期内，会先回调 onDirty；随后重置观察并按一轮
// stat 推进状态机（事件唤醒）。
func (g *SettledGate) OnChange(rootName, relPath string) {
	key := g.key(rootName, relPath)

	g.mu.Lock()
	e, exists := g.entries[key]
	wasSettled := exists && e.state == stateSettled &&
		g.SettledRetention > 0 && g.onDirty != nil
	if exists {
		if e.timer != nil {
			e.timer.Stop()
		}
		// 重置为全新观察，created 清空以便 force 重新计时
		g.entries[key] = &settleEntry{rootName: rootName, relPath: relPath}
	} else {
		g.entries[key] = &settleEntry{rootName: rootName, relPath: relPath}
	}
	g.mu.Unlock()

	if wasSettled {
		g.callDirty(rootName, relPath)
	}
	g.poll(key)
}

// Cancel 终止某个路径的观察（删除/重命名等路径失效场景）。
func (g *SettledGate) Cancel(rootName, relPath string) {
	g.terminate(g.key(rootName, relPath))
}

// Close 停止全部观察并清理内部状态。
func (g *SettledGate) Close() {
	g.mu.Lock()
	for k := range g.entries {
		g.deleteLocked(k)
	}
	g.mu.Unlock()
}

// poll 执行一轮 stat 并推进状态机。
func (g *SettledGate) poll(key string) {
	// 锁外取路径信息与执行 stat，避免持锁做 IO
	g.mu.Lock()
	e, ok := g.entries[key]
	if !ok {
		g.mu.Unlock()
		return
	}
	rootName, relPath := e.rootName, e.relPath
	rootPath, hasRoot := g.rootNames[rootName]
	g.mu.Unlock()
	if !hasRoot {
		g.terminate(key)
		return
	}

	fi, err := g.statFn(filepath.Join(rootPath, relPath))

	g.mu.Lock()
	e, ok = g.entries[key]
	if !ok {
		g.mu.Unlock()
		return
	}
	now := time.Now()
	if e.created.IsZero() {
		e.created = now
	}

	if err != nil {
		if os.IsNotExist(err) {
			// 边界 6：观察期内被删除/重命名，终止该路径
			g.mu.Unlock()
			g.terminate(key)
			return
		}
		// 边界 7：stat 暂时失败，不算稳定，重试；连续失败达上限则放弃
		e.statErrs++
		if g.MaxStatErrors > 0 && e.statErrs >= g.MaxStatErrors {
			g.mu.Unlock()
			g.terminate(key)
			return
		}
		g.scheduleLocked(key)
		g.mu.Unlock()
		return
	}
	e.statErrs = 0

	// 已判定写完后的保留期检查（销毁条目的清理轮询）
	if e.state == stateSettled {
		if g.SettledRetention > 0 && now.Sub(e.settledAt) >= g.SettledRetention {
			g.deleteLocked(key)
		} else {
			g.scheduleLocked(key)
		}
		g.mu.Unlock()
		return
	}

	// 边界 4：目录 fast-path，直接判定写完
	if fi.IsDir() {
		g.settleLocked(key, now)
		g.mu.Unlock()
		g.callSettled(rootName, relPath)
		return
	}

	// 边界 3：首次基线只记录不比较，避免空文件/0 初始值误判稳定
	if !e.observed {
		e.observed = true
		e.lastSize = fi.Size()
		e.lastModTime = fi.ModTime()
		g.scheduleLocked(key)
		g.mu.Unlock()
		return
	}

	// 边界 8：持续写入超过上限，强制判定写完，交由 hash 侧校验兜底
	if g.ForceTimeout > 0 && now.Sub(e.created) > g.ForceTimeout {
		g.settleLocked(key, now)
		g.mu.Unlock()
		g.callSettled(rootName, relPath)
		return
	}

	changed := fi.Size() != e.lastSize || !fi.ModTime().Equal(e.lastModTime)
	if changed {
		// 仍在变化：重置计数，回到 Observing
		e.lastSize = fi.Size()
		e.lastModTime = fi.ModTime()
		e.stableCount = 0
		e.state = stateObserving
		g.scheduleLocked(key)
		g.mu.Unlock()
		return
	}

	// 连续稳定次数递增
	e.stableCount++
	if e.stableCount >= g.RequiredStable {
		g.settleLocked(key, now)
		g.mu.Unlock()
		g.callSettled(rootName, relPath)
		return
	}
	e.state = stateNeedMore
	g.scheduleLocked(key)
	g.mu.Unlock()
}

// settleLocked 判定写完。调用者需持有锁。
// SettledRetention>0 时保留条目以支持后续 onDirty 检测，否则立即删除。
func (g *SettledGate) settleLocked(key string, now time.Time) {
	e := g.entries[key]
	if e == nil {
		return
	}
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	if g.SettledRetention > 0 {
		e.state = stateSettled
		e.settledAt = now
		g.scheduleLocked(key) // 计划保留期后清理
	} else {
		delete(g.entries, key)
	}
}

// scheduleLocked 计划下一次 poll。调用者需持有锁。
// PollInterval<=0 表示纯事件驱动：不自动调度，仅依赖外部 OnChange。
func (g *SettledGate) scheduleLocked(key string) {
	e := g.entries[key]
	if e == nil || g.PollInterval <= 0 {
		return
	}
	if e.timer != nil {
		e.timer.Stop()
	}
	e.timer = time.AfterFunc(g.PollInterval, func() {
		g.poll(key)
	})
}

// deleteLocked 停止定时器并移除条目。调用者需持有锁。
func (g *SettledGate) deleteLocked(key string) {
	if e, ok := g.entries[key]; ok {
		if e.timer != nil {
			e.timer.Stop()
		}
		delete(g.entries, key)
	}
}

func (g *SettledGate) terminate(key string) {
	g.mu.Lock()
	g.deleteLocked(key)
	g.mu.Unlock()
}

func (g *SettledGate) callSettled(rootName, relPath string) {
	if g.onSettled != nil {
		g.onSettled(rootName, relPath)
	}
}

func (g *SettledGate) callDirty(rootName, relPath string) {
	if g.onDirty != nil {
		g.onDirty(rootName, relPath)
	}
}
