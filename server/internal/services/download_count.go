package services

import (
	"sync"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

const (
	// DownloadCountFlushInterval 下载计数攒批落库的时间上限（距最近一次入队最多等待该时长）
	DownloadCountFlushInterval = 2 * time.Second
	// DownloadCountMaxPending 内存待落库计数映射达到该条目数即提前刷盘，控制峰值内存
	DownloadCountMaxPending = 256
	// DownloadCountBuffer 增量请求排队缓冲上限。缓冲满时丢弃计数（背压保护），不阻塞下载热路径
	DownloadCountBuffer = 1024
	// downloadCountBatchSize 单条 CASE 批量 UPDATE 覆盖的最大路径数
	downloadCountBatchSize = 500
)

// downloadCountRecorder 下载计数批量记录器。
// 下载热路径只需把 fullPath 追加进有界缓冲（非阻塞 send），后台 worker 周期攒批，
// 按路径聚合后用单条 UPDATE（download_count = download_count + n）批量落库，
// 消除"每次下载同步 UPDATE 一次索引表"的写放大与请求线程阻塞。
// 计数最终一致（最迟 DownloadCountFlushInterval 内落库），对下载计数器可接受。
type downloadCountRecorder struct {
	db   *gorm.DB
	ch   chan string
	done chan struct{}
	wg   sync.WaitGroup

	mu      sync.Mutex
	pending map[string]int
}

var (
	countRec     *downloadCountRecorder
	countRecLock sync.Mutex // 保护 countRec 的初始化/停用
)

// InitDownloadCountRecorder 初始化全局下载计数批量记录器（幂等）。
func InitDownloadCountRecorder(db *gorm.DB) {
	if db == nil {
		return
	}
	countRecLock.Lock()
	defer countRecLock.Unlock()
	if countRec != nil {
		return
	}
	r := &downloadCountRecorder{
		db:      db,
		ch:      make(chan string, DownloadCountBuffer),
		done:    make(chan struct{}),
		pending: make(map[string]int),
	}
	r.wg.Add(1)
	go r.worker()
	countRec = r
}

// SubmitDownloadCountIncrement 提交一次下载计数增量（非阻塞）。缓冲满时丢弃以保护热路径。
func SubmitDownloadCountIncrement(fullPath string) {
	if fullPath == "" {
		return
	}
	r := countRec
	if r == nil {
		return
	}
	select {
	case r.ch <- fullPath:
	default:
		utils.Warn("下载计数缓冲已满，丢弃一条计数", utils.String("fullPath", fullPath))
	}
}

// StopDownloadCountRecorder 停止记录器并排空未落库计数（进程退出/热重启前调用）。
func StopDownloadCountRecorder() {
	countRecLock.Lock()
	r := countRec
	if r == nil {
		countRecLock.Unlock()
		return
	}
	countRec = nil
	close(r.done)
	countRecLock.Unlock()

	r.wg.Wait()
}

func (r *downloadCountRecorder) worker() {
	defer r.wg.Done()
	ticker := time.NewTicker(DownloadCountFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case path := <-r.ch:
			r.accumulate(path)
			if r.pendingSize() >= DownloadCountMaxPending {
				r.flush()
			}
		case <-ticker.C:
			r.flush()
		case <-r.done:
			// 停止：先刷新已累计，再排空通道中尚未累计的增量，最后再刷新一次
			r.drainAll()
			return
		}
	}
}

// accumulate 累加一条增量
func (r *downloadCountRecorder) accumulate(path string) {
	r.mu.Lock()
	r.pending[path]++
	r.mu.Unlock()
}

func (r *downloadCountRecorder) pendingSize() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// drainAll 停止时耗尽所有已排队与已累计增量（须在 worker 内调用）
func (r *downloadCountRecorder) drainAll() {
	for {
		select {
		case path := <-r.ch:
			r.accumulate(path)
		default:
			r.flush()
			return
		}
	}
}

// flush 把当前累计的计数批量落库
func (r *downloadCountRecorder) flush() {
	r.mu.Lock()
	if len(r.pending) == 0 {
		r.mu.Unlock()
		return
	}
	type agg struct{ path string; n int }
	rows := make([]agg, 0, len(r.pending))
	for p, n := range r.pending {
		rows = append(rows, agg{path: p, n: n})
	}
	r.pending = make(map[string]int, len(rows))
	r.mu.Unlock()

	table := models.FileRecordPublic{}.TableName()
	stmt := "UPDATE " + table + " SET download_count = download_count + CASE full_path"
	var args []interface{}
	sqlArgs := make([]interface{}, 0)

	for i := 0; i < len(rows); i += downloadCountBatchSize {
		end := i + downloadCountBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		sub := rows[i:end]

		// 拼 CASE 分支（占位符）
		caseSQL := stmt
		args = args[:0]
		for _, rw := range sub {
			caseSQL += " WHEN ? THEN ?"
			args = append(args, rw.path, rw.n)
		}
		caseSQL += " END WHERE full_path IN (?) AND status = ?"
		paths := make([]string, len(sub))
		for j, rw := range sub {
			paths[j] = rw.path
		}
		sqlArgs = append(args, paths, models.FileStatusActive)

		if err := r.db.Exec(caseSQL, sqlArgs...).Error; err != nil {
			utils.Error("批量更新文件下载次数失败", utils.Int("rows", len(sub)), utils.Err(err))
		}
	}
}