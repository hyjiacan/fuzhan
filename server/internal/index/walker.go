package index

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// walkEntry 单趟并发遍历中发现的一个目录项（目录与文件都会投递）。
// relPath 为相对根目录的路径，以 "/" 开头、斜杠分隔；根目录本身不被投递。
type walkEntry struct {
	relPath string
	info    os.FileInfo
}

// defaultWalkWorkers 返回目录遍历并发度：取 GOMAXPROCS 并收敛到 [2,8]。
// ReadDir 是磁盘 IO 型任务，过高的并发在机械盘上会因寻道抖动适得其反。
func defaultWalkWorkers() int {
	n := runtime.GOMAXPROCS(0)
	if n < 2 {
		n = 2
	}
	if n > 8 {
		n = 8
	}
	return n
}

// walkConcurrent 以 workers 个协程并行 ReadDir 遍历 rootPath 目录树，
// 将发现的每个目录项（目录与文件）投递到 out，遍历结束后关闭 out。
// 返回遍历过程中遇到的第一个错误；ctx 取消时安全终止，不会泄漏协程。
//
// 与 filepath.Walk 的行为保持一致：不跟随符号链接（DirEntry.Info 走 Lstat 语义），
// 因此指向目录的符号链接会被当作普通文件项投递。
func walkConcurrent(ctx context.Context, rootPath string, workers int, out chan<- walkEntry) error {
	if workers < 1 {
		workers = 1
	}

	var (
		mu       sync.Mutex
		cond     = sync.NewCond(&mu)
		pending  []string // 待遍历目录（相对路径，"" 表示根）
		inflight int      // 尚未处理完成的目录数（pending + 正在处理）
		abort    bool     // ctx 取消或出错后停止派发
		firstErr error
		errMu    sync.Mutex
	)

	// 监听 ctx：取消时置 abort 并唤醒所有等在 cond 上的协程，避免终止死锁
	doneCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			abort = true
			cond.Broadcast()
			mu.Unlock()
		case <-doneCh:
		}
	}()
	defer close(doneCh)

	setErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	mu.Lock()
	pending = append(pending, "")
	inflight = 1
	mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			var childDirs []string
			for {
				mu.Lock()
				for len(pending) == 0 && inflight > 0 && !abort {
					cond.Wait()
				}
				if abort || inflight == 0 {
					mu.Unlock()
					return
				}
				dir := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				mu.Unlock()

				full := rootPath
				if dir != "" {
					full = filepath.Join(rootPath, filepath.FromSlash(dir))
				}

				childDirs = childDirs[:0]
				aborted := false
				entries, rerr := os.ReadDir(full)
				if rerr == nil {
					for _, e := range entries {
						if aborted {
							break
						}
						relChild := e.Name()
						if dir != "" {
							relChild = dir + "/" + e.Name()
						}
						// e.IsDir() 基于 lstat，与 filepath.Walk 的语义一致
						if e.IsDir() {
							info, ierr := e.Info()
							if ierr != nil {
								setErr(ierr)
								aborted = true
								continue
							}
							// 目录项也要投递（供索引记录），同时入队待展开
							select {
							case out <- walkEntry{relPath: "/" + relChild, info: info}:
								childDirs = append(childDirs, relChild)
							case <-ctx.Done():
								aborted = true
							}
							continue
						}
						info, ierr := e.Info()
						if ierr != nil {
							setErr(ierr)
							aborted = true
							continue
						}
						select {
						case out <- walkEntry{relPath: "/" + relChild, info: info}:
						case <-ctx.Done():
							aborted = true
						}
					}
				} else {
					setErr(rerr)
				}

				// 结算本目录：入队子目录并调整 inflight
				mu.Lock()
				pending = append(pending, childDirs...)
				inflight += len(childDirs) - 1
				if inflight == 0 {
					cond.Broadcast()
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	close(out)
	errMu.Lock()
	defer errMu.Unlock()
	return firstErr
}