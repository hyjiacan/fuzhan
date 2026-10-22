package services

import (
	"sync"
	"testing"
	"time"

	"fuzhan/internal/models"
)

// batchRecorder 内存记录仓库，用于断言有界记录器的落库时机
type batchRecorder struct {
	mu   sync.Mutex
	recs []*models.OperationRecord
	ch   chan []*models.OperationRecord
}

func (b *batchRecorder) Create(rec *models.OperationRecord) error {
	b.mu.Lock()
	b.recs = append(b.recs, rec)
	b.mu.Unlock()
	return nil
}

func (b *batchRecorder) CreateBatch(records []*models.OperationRecord) error {
	b.mu.Lock()
	b.recs = append(b.recs, records...)
	b.mu.Unlock()
	select {
	case b.ch <- records:
	default:
	}
	return nil
}

func (b *batchRecorder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.recs)
}

// TestRecordWriter_FlushOnInterval 低流量下不足一批的记录也应在超时后尽快落库，
// 否则管理界面的下载/预览记录会长时间不可见。
func TestRecordWriter_FlushOnInterval(t *testing.T) {
	repo := &batchRecorder{ch: make(chan []*models.OperationRecord, 4)}
	w := NewRecordWriter(repo, 1, 16)
	defer w.Stop()

	if !w.Submit(&models.OperationRecord{Action: "download", FileName: "a.txt"}) {
		t.Fatalf("提交记录失败")
	}

	select {
	case <-repo.ch:
		// 已按时落库
	case <-time.After(3 * recordWriterFlushInterval):
		t.Fatalf("单条记录未在 %v 内落库，实际落库 %d 条", 3*recordWriterFlushInterval, repo.count())
	}
	if got := repo.count(); got != 1 {
		t.Fatalf("期望落库 1 条记录，实际 %d 条", got)
	}
}
