package index

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/syncthing/notify"

	"fuzhan/internal/utils"
)

// Watcher 文件系统监听器，基于 syncthing/notify 实现递归文件监听。
// 当检测到文件变更时，通过 Syncer 增量同步到索引表。
type Watcher struct {
	syncer *Syncer

	// rootNames 根目录名 → 根目录路径映射（与 Syncer 共享）
	rootNames map[string]string

	// 事件消抖池
	pendingMu      sync.Mutex
	pending        map[string]*pendingEvent
	debounceWindow time.Duration
	sizeStableWait time.Duration
	sizeCheckWait  time.Duration

	// 重命名配对池（按 rootName 聚合同一短时间窗内的重命名路径）
	renameMu      sync.Mutex
	renamePending map[string]*renameAssembly
	renameWindow  time.Duration

	// gate 文件"是否写完"判定状态机。监听只上报原始事件，是否写完由它判定，
	// 判定写完通过 onFileSettled 建立索引，避免大文件写入中被误判提前建索引。
	gate *SettledGate

	// 忽略模式
	ignorePatterns []string

	// 监听通道和停止信号
	events  chan notify.EventInfo
	stopCh  chan struct{}
	running atomic.Bool

	// 最近同步检查回调（由 index.Service 注入，用于检测文件是否刚被上传写入索引）
	// 返回 true 表示文件最近已同步，watcher 应跳过处理
	skipRecentlySynced func(rootName, relPath string) bool
}

// pendingEvent 待处理的文件事件
type pendingEvent struct {
	path     string // 文件完整路径
	rootName string // 所属根目录
	relPath  string // 相对路径
	lastSize int64  // 上次检查的文件大小
	retries  int    // 重试次数，防止文件持续写入导致无限循环
	timerBox *time.Timer
}

// renameAssembly 单次重命名涉及的待配对路径集合。
// Windows 上对同一文件的重命名会产生"旧名、新名"两个 Rename 事件，
// 这里在短窗口内聚合同一 root 下的路径，随后还原 (旧名 -> 新名) 配对。
type renameAssembly struct {
	rootName string
	paths    map[string]struct{}
	timer    *time.Timer
	done     bool
}

// NewWatcher 创建文件监听器
func NewWatcher(syncer *Syncer, rootNames map[string]string) *Watcher {
	w := &Watcher{
		syncer:         syncer,
		rootNames:      rootNames,
		pending:        make(map[string]*pendingEvent),
		debounceWindow: 2 * time.Second, // 事件消抖窗口
		sizeStableWait: 2 * time.Second, // 文件大小稳定等待
		sizeCheckWait:  2 * time.Second, // 大小检查间隔
		renamePending:  make(map[string]*renameAssembly),
		renameWindow:   500 * time.Millisecond, // 重命名配对聚合窗口
		ignorePatterns: []string{
			".git", ".svn", ".hg", // VCS
			"node_modules", "__pycache__", // 依赖/缓存
			"Thumbs.db", ".DS_Store", // 系统文件
			"desktop.ini",
		},
	}
	w.gate = NewSettledGate(rootNames, w.onFileSettled, w.onFileDirty)
	return w
}

// Start 启动文件监听器
func (w *Watcher) Start() error {
	if w.running.Load() {
		utils.Warn("文件监听器已在运行，跳过重复启动")
		return nil
	}

	w.stopCh = make(chan struct{})
	w.running.Store(true)

	// 共享监听事件通道
	w.events = make(chan notify.EventInfo, 1024)

	for rootName, rootPath := range w.rootNames {
		// 使用 ... 后缀实现递归监听，统一正斜杠以兼容 Windows
		watchPath := filepath.ToSlash(rootPath) + "/..."
		// 只监听文件变更事件，避免 Read/Access/Attrib 等无关事件
		if err := notify.Watch(watchPath, w.events, notify.Create|notify.Write|notify.Remove|notify.Rename); err != nil {
			utils.Warn("添加文件监听失败，跳过此目录",
				utils.String("root", rootName),
				utils.String("path", rootPath),
				utils.Err(err))
			continue
		}
		utils.Info("文件监听器已添加目录",
			utils.String("root", rootName),
			utils.String("path", rootPath))
	}

	go w.processEvents(w.events)

	utils.Info("文件监听器已启动",
		utils.Int("roots", len(w.rootNames)),
		utils.String("debounce_window", w.debounceWindow.String()))
	return nil
}

// Stop 停止文件监听器
func (w *Watcher) Stop() {
	if !w.running.Load() {
		return
	}
	w.running.Store(false)

	// notify.Stop 接受通道作为参数，会移除所有与该通道关联的 watch
	notify.Stop(w.events)

	close(w.stopCh)

	// 清理 pending 池
	w.pendingMu.Lock()
	for _, ev := range w.pending {
		if ev.timerBox != nil {
			ev.timerBox.Stop()
		}
	}
	w.pending = nil
	w.pendingMu.Unlock()

	// 清理重命名配对池
	w.renameMu.Lock()
	for _, a := range w.renamePending {
		if a.timer != nil {
			a.timer.Stop()
		}
	}
	w.renamePending = nil
	w.renameMu.Unlock()

	// 清理写完判定状态机
	w.gate.Close()

	utils.Info("文件监听器已停止")
}

// processEvents 处理文件事件
func (w *Watcher) processEvents(events chan notify.EventInfo) {
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			path := ev.Path()
			// 过滤临时文件和隐藏文件
			if w.shouldIgnore(path) {
				continue
			}

			// 找出所属根目录
			rootName, relPath := w.resolvePath(path)
			if rootName == "" {
				continue
			}

			event := ev.Event()

			utils.Debug("文件监听事件",
				utils.String("path", relPath),
				utils.String("root", rootName),
				utils.String("event", event.String()))

			switch {
			case event&notify.Remove != 0:
				// 删除事件直接处理（无需等待稳定）
				w.handleRemove(rootName, relPath)

			case event&notify.Rename != 0:
				// 重命名事件在短窗口内聚合，还原 (旧名 -> 新名) 后原位更新索引
				w.handleRename(rootName, relPath)

			default:
				// Create / Write: 进入消抖池等待文件稳定
				// 如果是新建目录，自动添加递归监听（Linux inotify 不会自动递归监听新目录）
				if event&notify.Create != 0 {
					w.watchNewDirIfNeeded(path)
				}

				w.pendingMu.Lock()
				key := rootName + ":" + relPath
				if existing, ok := w.pending[key]; ok {
					// 已有待处理事件，重置定时器
					if existing.timerBox != nil {
						existing.timerBox.Stop()
					}
					existing.timerBox = time.AfterFunc(w.debounceWindow, func() {
						w.handleCreateOrWrite(existing)
					})
				} else {
					pe := &pendingEvent{
						path:     path,
						rootName: rootName,
						relPath:  relPath,
					}
					pe.timerBox = time.AfterFunc(w.debounceWindow, func() {
						w.handleCreateOrWrite(pe)
					})
					w.pending[key] = pe
				}
				w.pendingMu.Unlock()
			}
		}
	}
}

// handleCreateOrWrite 处理创建/写入事件（等待文件大小稳定后同步）
func (w *Watcher) handleCreateOrWrite(pe *pendingEvent) {
	// 检查文件是否最近已同步（由上传流程直接写入索引），防止重复处理
	if w.skipRecentlySynced != nil && w.skipRecentlySynced(pe.rootName, pe.relPath) {
		utils.Debug("文件最近已同步，跳过 watcher 处理",
			utils.String("path", pe.relPath),
			utils.String("root", pe.rootName))
		w.removePending(pe.rootName, pe.relPath)
		return
	}

	// 先快速确认文件是否存在
	info, err := os.Stat(pe.path)
	if err != nil {
		if os.IsNotExist(err) {
			utils.Debug("文件已不存在，跳过同步",
				utils.String("path", pe.relPath))
			w.removePending(pe.rootName, pe.relPath)
			return
		}
		utils.Warn("获取文件信息失败",
			utils.String("path", pe.relPath),
			utils.Err(err))
		w.removePending(pe.rootName, pe.relPath)
		return
	}

	currentSize := info.Size()

	// 如果文件大小与上次记录不同，说明还在写入，继续等待
	// 增加重试上限（maxRetries），防止日志等持续写入文件导致无限循环
	const maxRetries = 10
	if currentSize != pe.lastSize {
		pe.lastSize = currentSize
		pe.retries++
		if pe.retries > maxRetries || !w.running.Load() {
			// 超过重试上限或 watcher 已停止，强制同步
			w.removePending(pe.rootName, pe.relPath)
		} else {
			w.pendingMu.Lock()
			pe.timerBox = time.AfterFunc(w.sizeCheckWait, func() {
				w.handleCreateOrWrite(pe)
			})
			w.pendingMu.Unlock()
			return
		}
	}

	// 文件大小已稳定（或强制同步），先将事件从消抖池取出，再交给写完判定状态机；
	// 由状态机在判定真正写完（连续多次稳定）时通过 onFileSettled 建立索引，
	// 避免对大文件"写入中途缓冲停顿"被误判为已写完而提前建索引。
	w.removePending(pe.rootName, pe.relPath)

	if w.running.Load() {
		w.gate.OnChange(pe.rootName, pe.relPath)
	}
}

// onFileSettled 由写完判定状态机回调：文件判定写完时建立索引。
func (w *Watcher) onFileSettled(rootName, relPath string) {
	// 上传流程已直接写入索引（如分片上传收尾），跳过，避免重复处理
	if w.skipRecentlySynced != nil && w.skipRecentlySynced(rootName, relPath) {
		utils.Debug("文件最近已同步，跳过 gate 同步",
			utils.String("path", relPath),
			utils.String("root", rootName))
		return
	}
	utils.Info("文件写完同步",
		utils.String("path", relPath),
		utils.String("root", rootName))
	if err := w.syncer.SyncFile(rootName, relPath); err != nil {
		utils.Warn("文件写完同步失败",
			utils.String("path", relPath),
			utils.String("root", rootName),
			utils.Err(err))
	}
}

// onFileDirty 由写完判定状态机回调：已写完的文件又被追加/覆盖写，
// 将该记录 hash 状态置回 pending，待写入完成后重新计算。
func (w *Watcher) onFileDirty(rootName, relPath string) {
	utils.Debug("已写完文件被再次写入，标记 hash 待重算",
		utils.String("path", relPath),
		utils.String("root", rootName))
	w.syncer.MarkHashPending(rootName, relPath)
}

// handleRename 处理重命名事件：聚合到 rootName 关联的短窗口集合，等待旧名/新名两个事件配对。
func (w *Watcher) handleRename(rootName, relPath string) {
	w.renameMu.Lock()
	a, ok := w.renamePending[rootName]
	if !ok || a.done {
		a = &renameAssembly{
			rootName: rootName,
			paths:    make(map[string]struct{}),
		}
		w.renamePending[rootName] = a
		a.timer = time.AfterFunc(w.renameWindow, func() {
			w.resolveRenames(rootName)
		})
	}
	a.paths[relPath] = struct{}{}
	w.renameMu.Unlock()

	utils.Debug("文件重命名事件(待配对)",
		utils.String("path", relPath),
		utils.String("root", rootName))
}

// resolveRenames 重命名窗口到期后，配对集合中的旧名与新名并原位更新索引记录。
func (w *Watcher) resolveRenames(rootName string) {
	w.renameMu.Lock()
	a, ok := w.renamePending[rootName]
	if !ok || a.done {
		w.renameMu.Unlock()
		return
	}
	a.done = true
	delete(w.renamePending, rootName)
	paths := make([]string, 0, len(a.paths))
	for p := range a.paths {
		paths = append(paths, p)
	}
	w.renameMu.Unlock()

	if len(paths) == 0 {
		return
	}

	rootPath, ok := w.rootNames[rootName]
	if !ok {
		utils.Warn("重命名配对失败: 根目录不存在",
			utils.String("root", rootName))
		return
	}

	type renCand struct {
		path   string
		onDisk bool // 路径在磁盘上仍存在（重命名后的新名）
		hasDB  bool // 路径在索引表中已有 active 记录（重命名前的旧名）
	}
	cands := make([]renCand, 0, len(paths))
	for _, p := range paths {
		c := renCand{path: p}
		if _, err := os.Stat(filepath.Join(rootPath, p)); err == nil {
			c.onDisk = true
		}
		c.hasDB = w.syncer.HasActiveRecord(rootName, p)
		cands = append(cands, c)
	}

	var oldCands []string // 磁盘已消失 & db 有记录 → 旧名
	var newCands []string // 磁盘存在 & db 无记录 → 新名
	for _, c := range cands {
		switch {
		case !c.onDisk && c.hasDB:
			oldCands = append(oldCands, c.path)
		case c.onDisk && !c.hasDB:
			newCands = append(newCands, c.path)
		}
	}

	// 典型形态：一个旧名 -> 一个新名，直接原位更新索引（含目录递归、软删复活、检索索引同步）
	if len(oldCands) == 1 && len(newCands) == 1 {
		utils.Info("文件重命名同步",
			utils.String("root", rootName),
			utils.String("old", oldCands[0]),
			utils.String("new", newCands[0]))
		if err := w.syncer.MoveFile(rootName, oldCands[0], newCands[0]); err != nil {
			utils.Warn("重命名同步失败",
				utils.String("root", rootName),
				utils.String("old", oldCands[0]),
				utils.String("new", newCands[0]),
				utils.Err(err))
		}
		return
	}

	// 无法精确配对（多文件同时重命名 / 跨目录移动等），保守兜底：
	// 对"磁盘存在且 db 无记录"的新名走 SyncFile 建立索引，避免新路径长期缺记录；
	// 旧名不主动处理，交由下一次全量扫描对账，避免误配对/误删。
	for _, p := range newCands {
		utils.Info("重命名兜底: 为新路径建立索引",
			utils.String("root", rootName),
			utils.String("path", p))
		if err := w.syncer.SyncFile(rootName, p); err != nil {
			utils.Warn("重命名兜底同步失败",
				utils.String("root", rootName),
				utils.String("path", p),
				utils.Err(err))
		}
	}
	if len(oldCands) > 0 {
		utils.Debug("重命名未能配对, 旧名交由全量扫描对账",
			utils.String("root", rootName),
			utils.Int("count", len(oldCands)))
	}
}

// handleRemove 处理删除事件
func (w *Watcher) handleRemove(rootName, relPath string) {
	utils.Info("文件删除事件",
		utils.String("path", relPath),
		utils.String("root", rootName))

	// 终止该路径在写完判定状态机中的观察，避免继续 poll 已删除文件
	w.gate.Cancel(rootName, relPath)

	if err := w.syncer.RemoveFile(rootName, relPath); err != nil {
		utils.Warn("删除同步失败",
			utils.String("path", relPath),
			utils.String("root", rootName),
			utils.Err(err))
	}
}

// removePending 从消抖池中移除并清理定时器
func (w *Watcher) removePending(rootName, relPath string) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	key := rootName + ":" + relPath
	if pe, ok := w.pending[key]; ok {
		if pe.timerBox != nil {
			pe.timerBox.Stop()
		}
		delete(w.pending, key)
	}
}

// resolvePath 根据文件路径解析所属根目录和相对路径
func (w *Watcher) resolvePath(absPath string) (rootName, relPath string) {
	// 规范化路径
	absPath = filepath.Clean(absPath)

	for name, rootPath := range w.rootNames {
		cleanRoot := filepath.Clean(rootPath)
		// 检查文件是否在此根目录下
		rel, err := filepath.Rel(cleanRoot, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		return name, filepath.ToSlash(rel)
	}
	return "", ""
}

// shouldIgnore 判断文件路径是否应忽略
func (w *Watcher) shouldIgnore(path string) bool {
	name := filepath.Base(path)

	// 隐藏文件（以 . 开头）
	if strings.HasPrefix(name, ".") {
		return true
	}

	// uploading 后缀的临时文件
	if strings.HasSuffix(name, ".uploading") {
		return true
	}

	// 检查忽略目录模式
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		for _, pattern := range w.ignorePatterns {
			if part == pattern {
				return true
			}
		}
	}

	return false
}

// watchNewDirIfNeeded 如果是新创建的目录，自动添加递归监听
// Linux 下 inotify 不会自动监听新建的子目录，需要显式添加
func (w *Watcher) watchNewDirIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	watchPath := filepath.ToSlash(path) + "/..."
	err = notify.Watch(watchPath, w.events, notify.Create|notify.Write|notify.Remove|notify.Rename)
	if err != nil {
		// Already watched 等错误是预期的（Windows 下 ReadDirectoryChangesW 已自动递归）
		utils.Debug("添加新目录监听失败（可能已被自动监听）",
			utils.String("path", path),
			utils.Err(err))
	} else {
		utils.Info("已自动对新目录添加文件监听",
			utils.String("path", path))
	}
}

// SetDebounceWindow 设置事件消抖窗口
func (w *Watcher) SetDebounceWindow(d time.Duration) {
	w.debounceWindow = d
	utils.Info("文件监听器消抖窗口已更新",
		utils.String("debounce", d.String()))
}

// IsRunning 返回监听器是否在运行
func (w *Watcher) IsRunning() bool {
	return w.running.Load()
}
