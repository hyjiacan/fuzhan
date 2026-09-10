package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/utils"
	xxh3pkg "fuzhan/pkg/xxh3"
	"gorm.io/gorm"
)

// hashRecord 待计算哈希的文件记录（用于并发批次处理）
type hashRecord struct {
	ID       uint
	FilePath string
	RootName string
}

// HashWorker 哈希计算器
// 扫描所有 hash_status = 'pending' 的记录，按文件大小从小到大计算 hash。
// 单例运行：重复触发不重复启动，已在运行时新触发直接忽略。
type HashWorker struct {
	db          *gorm.DB
	rootNames   map[string]string
	running     bool
	mu          sync.Mutex
	progress    HashProgress
	stats       HashStats
	taskService TaskRecorder // 可选，用于记录哈希任务到 task_records
}

// HashProgress 哈希计算进度
type HashProgress struct {
	Total   int64  `json:"total"`
	Done    int64  `json:"done"`
	Failed  int64  `json:"failed"`
	Current string `json:"current"`
	Running bool   `json:"running"`
}

// HashStats 失败统计
type HashStats struct {
	FileNotFound     int `json:"fileNotFound"`
	PermissionDenied int `json:"permissionDenied"`
	RootNotFound     int `json:"rootNotFound"`
	OtherError       int `json:"otherError"`
}

// NewHashWorker 创建哈希计算器
func NewHashWorker(db *gorm.DB, rootNames map[string]string) *HashWorker {
	return &HashWorker{
		db:        db,
		rootNames: rootNames,
	}
}

// Trigger 触发哈希计算
func (w *HashWorker) Trigger(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go w.run(ctx)
}

// Progress 返回当前进度
func (w *HashWorker) Progress() HashProgress {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.progress
}

// Stats 返回失败统计
func (w *HashWorker) Stats() HashStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// IsRunning 返回是否正在运行
func (w *HashWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// run 执行哈希计算
func (w *HashWorker) run(ctx context.Context) {
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	utils.Info("哈希计算任务开始")

	// 创建任务记录（供任务管理页面展示）
	var taskID uint
	if w.taskService != nil {
		if task, err := w.taskService.CreateTask("hash", "哈希计算", "auto"); err == nil {
			taskID = task.ID
			w.taskService.StartTask(taskID)
		}
	}

	tables := []string{
		"file_records_public",
		"file_records_temp",
		"file_records_private",
	}

	// 统计总待处理数
	var total int64
	for _, name := range tables {
		var count int64
		if err := w.db.Table(name).
			Where("hash_status = ? AND is_dir = ?", "pending", false).
			Count(&count).Error; err == nil {
			total += count
		}
	}

	w.mu.Lock()
	w.progress = HashProgress{Total: total, Running: true}
	w.stats = HashStats{}
	w.mu.Unlock()

	var totalDone int64
	var totalFailed int64

	// 定时报告协程：每 2 秒更新一次进度，确保前端轮询时能获取到最新状态
	reportCtx, reportCancel := context.WithCancel(ctx)
	defer reportCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reportCtx.Done():
				return
			case <-ticker.C:
				w.mu.Lock()
				w.progress.Done = totalDone
				w.progress.Failed = totalFailed
				w.mu.Unlock()

				// 日志输出进度
				pct := 0
				if total > 0 {
					pct = int(float64(totalDone+totalFailed) / float64(total) * 100)
				}
				utils.Info("哈希计算进度",
					utils.Int64("total", total),
					utils.Int64("done", totalDone),
					utils.Int64("failed", totalFailed),
					utils.Int("percent", pct))
			}
		}
	}()

	for _, name := range tables {
		// 统计当前表的总待处理数
		var tableTotal int64
		if err := w.db.Table(name).
			Where("hash_status = ? AND is_dir = ?", "pending", false).
			Count(&tableTotal).Error; err == nil && tableTotal == 0 {
			continue // 跳过空表
		}

		done, failed := w.processTable(ctx, name, tableTotal)
		totalDone += done
		totalFailed += failed
		// 进度更新由定时协程负责，此处不再重复更新
	}

	w.mu.Lock()
	w.progress.Running = false
	w.progress.Current = ""
	w.progress.Done = totalDone
	w.progress.Failed = totalFailed
	w.mu.Unlock()

	if taskID > 0 && w.taskService != nil {
		w.taskService.UpdateTaskProgress(taskID, 100, totalDone, total)
		details := fmt.Sprintf("已计算 %d 个文件", totalDone)
		if totalFailed > 0 {
			details += fmt.Sprintf("，失败 %d 个", totalFailed)
		}
		w.taskService.UpdateTaskDetails(taskID, details)
		if totalFailed > 0 {
			w.taskService.FailTask(taskID, fmt.Sprintf("部分失败: %d/%d", totalFailed, total))
		} else {
			w.taskService.CompleteTask(taskID)
		}
	}

	utils.Info("哈希计算任务完成",
		utils.Int64("total", total),
		utils.Int64("done", totalDone),
		utils.Int64("failed", totalFailed))
}

// processTable 处理单个表中的 pending 记录
func (w *HashWorker) processTable(ctx context.Context, tableName string, tableTotal int64) (int64, int64) {
	var done, failed int64
	// 每批处理完会将 hash_status 置为 done，循环会继续拉取下一批 pending，直至清空。
	const batchSize = 500
	for {
		select {
		case <-ctx.Done():
			return done, failed
		default:
		}

		var records []hashRecord
		if err := w.db.Table(tableName).
			Select("id, file_path, root_name").
			Where("hash_status = ? AND is_dir = ?", "pending", false).
			Order("file_size ASC").
			Limit(batchSize).
			Find(&records).Error; err != nil {
			utils.Warn("查询待计算 hash 记录失败",
				utils.String("table", tableName),
				utils.Err(err))
			return done, failed
		}

		if len(records) == 0 {
			return done, failed
		}

		batchDone, batchFailed := w.processHashBatch(ctx, tableName, records)
		done += batchDone
		failed += batchFailed
	}
}

// processHashBatch 并发计算一批记录的哈希值，并在主 goroutine 统一写库。
// 哈希计算是纯磁盘 IO 类任务，可安全并行；SQLite 写操作集中串行执行，
// 避免并发写锁竞争。
func (w *HashWorker) processHashBatch(ctx context.Context, tableName string, records []hashRecord) (int64, int64) {
	type hashResult struct {
		id          uint
		hash        string
		errMsg      string
		errCategory string
		failed      bool
	}

	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 2 {
		workerCount = 2
	}
	if workerCount > len(records) {
		workerCount = len(records)
	}

	jobs := make(chan hashRecord, len(records))
	results := make(chan hashResult, len(records))

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for rec := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				res := hashResult{id: rec.ID}
				rootPath, ok := w.rootNames[rec.RootName]
				if !ok {
					res.failed = true
					res.errMsg = "root_not_found: " + rec.RootName
					res.errCategory = "rootNotFound"
				} else {
					fullPath := filepath.Join(rootPath, rec.FilePath)
					h, err := xxh3pkg.ComputeFileHash(fullPath)
					if err != nil {
						res.failed = true
						res.errMsg = categorizeError(fullPath, err)
						res.errCategory = classifyHashError(err)
					} else {
						res.hash = h
					}
				}
				results <- res
			}
		}()
	}
	for _, r := range records {
		jobs <- r
	}
	close(jobs)
	wg.Wait()
	close(results)

	succIDs := make([]uint, 0, len(records))
	succHashes := make([]string, 0, len(records))
	var done, failed int64
	for res := range results {
		if res.failed {
			w.markFailed(tableName, res.id, res.errMsg, 0)
			failed++
			w.incStat(res.errCategory)
			continue
		}
		succIDs = append(succIDs, res.id)
		succHashes = append(succHashes, res.hash)
	}

	if len(succIDs) > 0 {
		release := lockWrite()
		for i, id := range succIDs {
			if err := w.db.Table(tableName).Where("id = ?", id).Updates(map[string]interface{}{
				"xxh3_hash":    succHashes[i],
				"hash_status":  "done",
				"hash_error":   "",
				"hash_retries": 0,
			}).Error; err != nil {
				utils.Warn("更新哈希记录失败",
					utils.Int("id", int(id)),
					utils.String("table", tableName),
					utils.Err(err))
				continue
			}
			done++
		}
		release()
	}

	return done, failed
}

// classifyHashError 将哈希计算错误归类为失败统计类别（fileNotFound/permissionDenied/fileLocked/otherError）
func classifyHashError(err error) string {
	if os.IsNotExist(err) {
		return "fileNotFound"
	}
	if os.IsPermission(err) {
		return "permissionDenied"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "used by another process") ||
		strings.Contains(msg, "file locked") ||
		strings.Contains(msg, "text file busy") {
		return "fileLocked"
	}
	return "otherError"
}

// markFailed 标记记录为失败
func (w *HashWorker) markFailed(tableName string, id uint, errMsg string, retries int) {
	release := lockWrite()
	defer release()
	w.db.Table(tableName).Where("id = ?", id).Updates(map[string]interface{}{
		"hash_status":  "failed",
		"hash_error":   errMsg,
		"hash_retries": retries + 1,
	})
}

// incStat 递增失败统计
func (w *HashWorker) incStat(category string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch category {
	case "fileNotFound":
		w.stats.FileNotFound++
	case "permissionDenied":
		w.stats.PermissionDenied++
	case "rootNotFound":
		w.stats.RootNotFound++
	default:
		w.stats.OtherError++
	}
}

// categorizeError 分析错误类型并返回结构化错误描述
func categorizeError(path string, err error) string {
	if os.IsNotExist(err) {
		return fmt.Sprintf("file_not_found: %s", path)
	}
	if os.IsPermission(err) {
		return fmt.Sprintf("permission_denied: %s", path)
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "used by another process") ||
		strings.Contains(msg, "file locked") ||
		strings.Contains(msg, "text file busy") {
		return fmt.Sprintf("file_locked: %s", path)
	}
	return fmt.Sprintf("io_error: %s — %s", path, err.Error())
}
