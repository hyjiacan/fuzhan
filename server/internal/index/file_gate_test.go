package index

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeFileInfo 实现 os.FileInfo 的最小桩，供注入 statFn 使用。
type fakeFileInfo struct {
	size    int64
	modTime time.Time
	dir     bool
}

func (f fakeFileInfo) Name() string       { return "" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() interface{}   { return nil }

func waitCond(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s", msg)
}

func newTestGate(roots map[string]string, onSettled, onDirty func(string, string)) *SettledGate {
	g := NewSettledGate(roots, onSettled, onDirty)
	g.RequiredStable = 2
	g.PollInterval = 5 * time.Millisecond
	g.SettledRetention = 0 // 默认立即清理，便于多数用例断言
	return g
}

var testRoots = map[string]string{"r": "C:\\fake\\root"}

// 边界 1：写完即不再产生事件，靠自持定时器 poll 推进，最终判定写完。
func TestGate_StableFile_EmitsSettled(t *testing.T) {
	var settled atomic.Bool
	var gotRoot, gotPath string
	mt := time.Now()
	g := newTestGate(testRoots, func(r, p string) {
		settled.Store(true)
		gotRoot, gotPath = r, p
	}, nil)
	g.statFn = func(_ string) (os.FileInfo, error) {
		return fakeFileInfo{size: 100, modTime: mt}, nil
	}

	g.OnChange("r", "a.iso")

	waitCond(t, 2*time.Second, settled.Load, "稳定文件未在超时内判定写完")
	if gotRoot != "r" || gotPath != "a.iso" {
		t.Fatalf("回调路径错误: root=%q path=%q", gotRoot, gotPath)
	}
}

// 边界 1 变体：文件先在增长，随后连续稳定，最终判定写完。
func TestGate_WritingThenStable(t *testing.T) {
	var settled atomic.Bool
	var calls int32
	mt := time.Now()
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.statFn = func(_ string) (os.FileInfo, error) {
		n := atomic.AddInt32(&calls, 1)
		var size int64 = 0
		if n > 1 {
			size = 100
		}
		return fakeFileInfo{size: size, modTime: mt}, nil
	}

	g.OnChange("r", "a.iso")

	waitCond(t, 2*time.Second, settled.Load, "先增长后稳定的文件未判定写完")
}

// 边界 6：观察期内被删除 → 终止，不触发 onSettled。
func TestGate_Deleted_NoEmit(t *testing.T) {
	var settled atomic.Bool
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.statFn = func(_ string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}

	g.OnChange("r", "a.iso")
	time.Sleep(100 * time.Millisecond)

	if settled.Load() {
		t.Fatal("文件已删除却被判定写完")
	}
	g.mu.Lock()
	sz := len(g.entries)
	g.mu.Unlock()
	if sz != 0 {
		t.Fatalf("删除后应清理条目, 仍有 %d 条", sz)
	}
}

// 边界 4：目录 fast-path 直接判定写完。
func TestGate_Directory_FastPath(t *testing.T) {
	var settled atomic.Bool
	mt := time.Now()
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.statFn = func(_ string) (os.FileInfo, error) {
		return fakeFileInfo{size: 0, modTime: mt, dir: true}, nil
	}

	g.OnChange("r", "dir")

	waitCond(t, 2*time.Second, settled.Load, "目录未通过 fast-path 判定写完")
}

// 边界 7：stat 暂时失败不应视为稳定；恢复后最终判定写完。
func TestGate_StatError_RetriesNotStable(t *testing.T) {
	var settled atomic.Bool
	var calls atomic.Int32
	mt := time.Now()
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.MaxStatErrors = 0 // 永不放弃
	g.statFn = func(_ string) (os.FileInfo, error) {
		n := calls.Add(1)
		if n <= 3 {
			return nil, errors.New("access denied")
		}
		return fakeFileInfo{size: 100, modTime: mt}, nil
	}

	g.OnChange("r", "a.iso")

	waitCond(t, 2*time.Second, settled.Load, "stat 暂时失败恢复后未判定写完")
	if calls.Load() <= 3 {
		t.Fatalf("stat 应发生多次: %d", calls.Load())
	}
}

// 边界 7 变体：连续 stat 失败达 MaxStatErrors → 放弃该路径，不 settle。
func TestGate_StatError_GiveUp(t *testing.T) {
	var settled atomic.Bool
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.MaxStatErrors = 2
	g.statFn = func(_ string) (os.FileInfo, error) {
		return nil, errors.New("i/o timeout")
	}

	g.OnChange("r", "a.iso")
	time.Sleep(100 * time.Millisecond)

	if settled.Load() {
		t.Fatal("stat 持续失败却判定写完")
	}
	g.mu.Lock()
	sz := len(g.entries)
	g.mu.Unlock()
	if sz != 0 {
		t.Fatalf("放弃后应清理条目, 仍有 %d 条", sz)
	}
}

// 边界 3：空文件/0 初始值，首轮 stat 仅记录基线，不因 requiredStable=1 立刻判定写完。
func TestGate_EmptyFile_RequiresBaseline(t *testing.T) {
	var settled atomic.Bool
	var calls int32
	mt := time.Now()
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.RequiredStable = 1
	g.statFn = func(_ string) (os.FileInfo, error) {
		atomic.AddInt32(&calls, 1)
		return fakeFileInfo{size: 0, modTime: mt}, nil
	}

	g.OnChange("r", "a.iso")

	// 首轮已完成基线，此轮不应触发 settle（需 baseline 之后再有 1 次稳定）
	c := atomic.LoadInt32(&calls)
	if c == 0 {
		t.Fatal("OnChange 未触发首次 poll")
	}
	// 即便同步阶段判定打开，首轮也只记录基线
	if settled.Load() {
		t.Fatal("首次基线即被误判写完")
	}
	waitCond(t, 2*time.Second, settled.Load, "空文件稳定后未判定写完")
}

// 边界 8：持续写入（永不稳定）超过 ForceTimeout → 强制判定写完，交由 hash 侧兜底。
func TestGate_ForceTimeout(t *testing.T) {
	var settled atomic.Bool
	var calls int32
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.ForceTimeout = 30 * time.Millisecond
	g.statFn = func(_ string) (os.FileInfo, error) {
		n := atomic.AddInt32(&calls, 1)
		return fakeFileInfo{size: int64(n) * 100, modTime: time.Now()}, nil
	}

	g.OnChange("r", "a.iso")

	waitCond(t, 2*time.Second, settled.Load, "持续写入未在 ForceTimeout 后被强制判定写完")
}

// 边界 10：Cancel 终止观察，不再触发 onSettled。
func TestGate_Cancel_StopsObservation(t *testing.T) {
	var settled atomic.Bool
	mt := time.Now()
	g := newTestGate(testRoots, func(_, _ string) { settled.Store(true) }, nil)
	g.statFn = func(_ string) (os.FileInfo, error) {
		return fakeFileInfo{size: 100, modTime: mt}, nil
	}

	g.OnChange("r", "a.iso")
	g.Cancel("r", "a.iso")
	time.Sleep(100 * time.Millisecond)

	if settled.Load() {
		t.Fatal("Cancel 后仍判定写完")
	}
	g.mu.Lock()
	sz := len(g.entries)
	g.mu.Unlock()
	if sz != 0 {
		t.Fatalf("Cancel 后应清理条目, 仍有 %d 条", sz)
	}
}

// 边界 2：已写完(Appended)又收到写入事件 → 触发 onDirty 回调（需保留期内）。
func TestGate_AppendAfterSettled_Dirty(t *testing.T) {
	var settled, dirty atomic.Bool
	mt := time.Now()
	g := newTestGate(testRoots,
		func(_, _ string) { settled.Store(true) },
		func(_, _ string) { dirty.Store(true) },
	)
	g.RequiredStable = 1
	g.SettledRetention = 10 * time.Second // 保留期内以便检测后续写入
	g.statFn = func(_ string) (os.FileInfo, error) {
		return fakeFileInfo{size: 100, modTime: mt}, nil
	}

	g.OnChange("r", "a.iso")
	waitCond(t, 2*time.Second, settled.Load, "首次写入未settle")

	// 已写完的文件被追加/覆盖写 → 新事件
	g.OnChange("r", "a.iso")
	waitCond(t, 2*time.Second, dirty.Load, "settle 后再次写入未触发 onDirty")
}

// 并发安全：多 goroutine 同时上报不同路径，均正确判定。
func TestGate_ConcurrentWrites(t *testing.T) {
	var settledCount atomic.Int32
	mt := time.Now()
	g := newTestGate(testRoots, func(_, _ string) { settledCount.Add(1) }, nil)
	g.statFn = func(_ string) (os.FileInfo, error) {
		return fakeFileInfo{size: 100, modTime: mt}, nil
	}

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.OnChange("r", fileGatePath(i))
		}(i)
	}
	wg.Wait()

	waitCond(t, 2*time.Second, func() bool { return settledCount.Load() == n }, "并发文件未全部settle")
}

func fileGatePath(i int) string {
	return "f" + string(rune('a'+i)) + ".iso"
}
