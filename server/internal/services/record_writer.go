package services

import (
	"sync"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

const (
	// RecordWriterWorkers 全局有界记录器的固定 worker 数
	RecordWriterWorkers = 4
	// RecordWriterBuffer 全局有界记录器的排队缓冲上限（条）
	RecordWriterBuffer = 256
)

// RecordWriter 有界异步操作记录写入器。
// 用固定数量的 worker 消费一个有界缓冲队列，替代"每个操作都 go func 起一个协程"的写法，
// 从而在任何负载下都把协程数量与排队内存限制在固定上界内。
// 缓冲写满时 Submit 会阻塞（背压），而不是无限堆积或无限起协程。
type RecordWriter struct {
	repo RecordRepository
	ch   chan *models.OperationRecord
	done chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

// NewRecordWriter 创建有界记录器。workers 为固定协程数，buffer 为排队上限。
func NewRecordWriter(repo RecordRepository, workers, buffer int) *RecordWriter {
	if workers <= 0 {
		workers = 1
	}
	if buffer <= 0 {
		buffer = 64
	}
	w := &RecordWriter{
		repo: repo,
		ch:   make(chan *models.OperationRecord, buffer),
		done: make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		w.wg.Add(1)
		go w.worker()
	}
	return w
}

func (w *RecordWriter) worker() {
	defer w.wg.Done()
	for {
		select {
		case rec := <-w.ch:
			w.write(rec)
		case <-w.done:
			// 收到停止信号后，仍把缓冲中已排队的记录尽量写完再退出
			for {
				select {
				case rec := <-w.ch:
					w.write(rec)
				default:
					return
				}
			}
		}
	}
}

func (w *RecordWriter) write(rec *models.OperationRecord) {
	if rec == nil {
		return
	}
	if err := w.repo.Create(rec); err != nil {
		utils.Error("操作记录写入失败",
			utils.String("action", rec.Action),
			utils.String("file", rec.FileName),
			utils.Err(err))
	}
}

// Submit 提交一条操作记录。缓冲满时阻塞提供背压；Stop 后调用被安全丢弃，返回 false。
func (w *RecordWriter) Submit(rec *models.OperationRecord) bool {
	select {
	case w.ch <- rec:
		return true
	case <-w.done:
		return false
	}
}

// Stop 停止写入器，等待所有 worker 处理完已排队的记录后返回。
func (w *RecordWriter) Stop() {
	w.once.Do(func() {
		close(w.done)
	})
	w.wg.Wait()
}

var (
	defaultWriterMu sync.RWMutex
	defaultWriter   *RecordWriter
)

// InitRecordWriter 初始化全局有界记录器，在启动阶段调用一次。
func InitRecordWriter(repo RecordRepository, workers, buffer int) *RecordWriter {
	defaultWriterMu.Lock()
	defer defaultWriterMu.Unlock()
	defaultWriter = NewRecordWriter(repo, workers, buffer)
	return defaultWriter
}

// SubmitRecord 向全局有界记录器提交一条记录。
// 未初始化或已停止时返回 false，调用方可据此回退为同步写入，避免漏记。
func SubmitRecord(rec *models.OperationRecord) bool {
	defaultWriterMu.RLock()
	w := defaultWriter
	defaultWriterMu.RUnlock()
	if w == nil {
		return false
	}
	return w.Submit(rec)
}

// SubmitFailedRecord 记录一次失败的操作（用于下载行为分析失败/异常维度）。
// 复用全局有界记录器；携带失败原因，避免各下载入口重复初始化。
func SubmitFailedRecord(rec *models.OperationRecord, failReason string) {
	if rec == nil {
		return
	}
	rec.Status = models.RecordStatusFailed
	rec.FailReason = failReason
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = utils.Now()
	}
	SubmitRecord(rec)
}

// SubmitDownloadRecord 提交一条下载操作记录（成功或失败），默认补齐 Action=download、
// SourceType=public 与 CreatedAt。供公开/私有/临时下载入口统一复用。
// 返回是否已交给全局有界记录器；调用方在其返回 false 时可回退为同步写库（configPkg.GetDB().Create）。
func SubmitDownloadRecord(op *models.OperationRecord) bool {
	if op == nil {
		return false
	}
	if op.Action == "" {
		op.Action = "download"
	}
	if op.SourceType == "" {
		op.SourceType = models.SourceTypePublic
	}
	if op.CreatedAt.IsZero() {
		op.CreatedAt = utils.Now()
	}
	return SubmitRecord(op)
}

// StopRecordWriter 停止全局有界记录器。
func StopRecordWriter() {
	defaultWriterMu.RLock()
	w := defaultWriter
	defaultWriterMu.RUnlock()
	if w != nil {
		w.Stop()
	}
}
