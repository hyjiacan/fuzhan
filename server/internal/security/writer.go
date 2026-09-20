package security

import (
	"sync"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

const (
	// WriterWorkers 全局有界安全事件写入器的固定 worker 数
	WriterWorkers = 2
	// WriterBuffer 全局有界安全事件写入器的排队缓冲上限（条）
	WriterBuffer = 1024
	// writerBatchSize 单个 worker 攒批批量提交的条数，降低逐条 insert 的写放大
	writerBatchSize = 100
)

// SecurityEventWriter 有界异步安全事件写入器。
// 攻击场景下事件可能成片爆发（暴破/扫描），用固定数量 worker 消费有界缓冲，
// 把协程数量与排队内存限制在固定上界内，同时避免每次拒绝请求同步写库拖慢响应。
// 缓冲写满时 Submit 阻塞（背压）；Stop 后 Submit 返回 false，不丢失已排队记录。
type SecurityEventWriter struct {
	repo      SecurityEventStore
	ch        chan *models.SecurityEvent
	done      chan struct{}
	wg        sync.WaitGroup
	once      sync.Once
	batchSize int
}

// NewSecurityEventWriter 创建有界安全事件写入器
func NewSecurityEventWriter(repo SecurityEventStore, workers, buffer int) *SecurityEventWriter {
	if workers <= 0 {
		workers = 1
	}
	if buffer <= 0 {
		buffer = 64
	}
	w := &SecurityEventWriter{
		repo:      repo,
		ch:        make(chan *models.SecurityEvent, buffer),
		done:      make(chan struct{}),
		batchSize: writerBatchSize,
	}
	for i := 0; i < workers; i++ {
		w.wg.Add(1)
		go w.worker()
	}
	return w
}

func (w *SecurityEventWriter) worker() {
	defer w.wg.Done()
	batch := make([]*models.SecurityEvent, 0, w.batchSize)
	flush := func() {
		w.writeBatch(batch)
		batch = batch[:0]
	}
	for {
		select {
		case ev := <-w.ch:
			batch = append(batch, ev)
			if len(batch) >= w.batchSize {
				flush()
			}
		case <-w.done:
			for {
				select {
				case ev := <-w.ch:
					batch = append(batch, ev)
					if len(batch) >= w.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (w *SecurityEventWriter) writeBatch(events []*models.SecurityEvent) {
	n := 0
	for _, ev := range events {
		if ev != nil {
			events[n] = ev
			n++
		}
	}
	if n == 0 {
		return
	}
	if err := w.repo.CreateBatch(events[:n]); err != nil {
		utils.Error("安全事件批量写入失败",
			utils.Int("count", n),
			utils.String("eventType", events[0].EventType),
			utils.Err(err))
	}
}

// Submit 提交一条安全事件。缓冲满时阻塞提供背压；Stop 后被安全丢弃并返回 false。
func (w *SecurityEventWriter) Submit(ev *models.SecurityEvent) bool {
	select {
	case w.ch <- ev:
		return true
	case <-w.done:
		return false
	}
}

// Stop 停止写入器，等待所有 worker 处理完已排队记录后返回
func (w *SecurityEventWriter) Stop() {
	w.once.Do(func() { close(w.done) })
	w.wg.Wait()
}

var (
	defaultWriterMu sync.RWMutex
	defaultWriter   *SecurityEventWriter
)

// InitWriter 初始化全局有界安全事件写入器，在启动阶段调用一次
func InitWriter(repo SecurityEventStore, workers, buffer int) *SecurityEventWriter {
	defaultWriterMu.Lock()
	defer defaultWriterMu.Unlock()
	defaultWriter = NewSecurityEventWriter(repo, workers, buffer)
	return defaultWriter
}

// Record 向全局写入器提交一条安全事件。
// 未初始化或已停止时返回 false，调用方可回退同步写入（一般无需，安全事件可容忍轻微丢失）。
func Record(ev *models.SecurityEvent) bool {
	if ev == nil {
		return false
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = utils.Now()
	}
	if ev.Severity == "" {
		ev.Severity = models.SeverityLow
	}
	if ev.Result == "" {
		ev.Result = "failure"
	}
	defaultWriterMu.RLock()
	w := defaultWriter
	defaultWriterMu.RUnlock()
	if w == nil {
		return false
	}
	return w.Submit(ev)
}

// StopWriter 停止全局安全事件写入器
func StopWriter() {
	defaultWriterMu.RLock()
	w := defaultWriter
	defaultWriterMu.RUnlock()
	if w != nil {
		w.Stop()
	}
}
