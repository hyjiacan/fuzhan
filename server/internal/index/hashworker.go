package index

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "gorm.io/gorm"
    xxh3pkg "fuzhan/pkg/xxh3"
    "fuzhan/internal/utils"
)

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
    FileNotFound    int `json:"fileNotFound"`
    PermissionDenied int `json:"permissionDenied"`
    RootNotFound    int `json:"rootNotFound"`
    OtherError      int `json:"otherError"`
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
        if task, err := w.taskService.CreateTask("hash", "哈希计算"); err == nil {
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
    for {
        select {
        case <-ctx.Done():
            return done, failed
        default:
        }

        type pendingRecord struct {
            ID       uint
            FilePath string
            RootName string
        }

        var records []pendingRecord
        if err := w.db.Table(tableName).
            Select("id, file_path, root_name").
            Where("hash_status = ? AND is_dir = ?", "pending", false).
            Order("file_size ASC").
            Limit(100).
            Find(&records).Error; err != nil {
            utils.Warn("查询待计算 hash 记录失败",
                utils.String("table", tableName),
                utils.Err(err))
            return done, failed
        }

        if len(records) == 0 {
            return done, failed
        }

        for _, rec := range records {
            select {
            case <-ctx.Done():
                return done, failed
            default:
            }

            rootPath, ok := w.rootNames[rec.RootName]
            if !ok {
                w.markFailed(tableName, rec.ID, "root_not_found: "+rec.RootName, 0)
                failed++
                w.incStat("rootNotFound")
                continue
            }

            fullPath := filepath.Join(rootPath, rec.FilePath)
            err := w.computeAndSave(tableName, rec.ID, fullPath)
            if err != nil {
                failed++
            } else {
                done++
            }

        }
    }
}

// computeAndSave 计算哈希并保存，返回错误表示失败
func (w *HashWorker) computeAndSave(tableName string, id uint, fullPath string) error {
    h, err := xxh3pkg.ComputeFileHash(fullPath)
    if err != nil {
        errMsg := categorizeError(fullPath, err)
        w.markFailed(tableName, id, errMsg, 0)
        return err
    }

    // 成功
    result := w.db.Table(tableName).Where("id = ?", id).Updates(map[string]interface{}{
        "xxh3_hash":   h,
        "hash_status": "done",
        "hash_error":  "",
        "hash_retries": 0,
    })
    if result.Error != nil {
        utils.Warn("更新哈希记录失败",
            utils.Int("id", int(id)),
            utils.String("table", tableName),
            utils.Err(result.Error))
        return result.Error
    }
    return nil
}

// markFailed 标记记录为失败
func (w *HashWorker) markFailed(tableName string, id uint, errMsg string, retries int) {
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
