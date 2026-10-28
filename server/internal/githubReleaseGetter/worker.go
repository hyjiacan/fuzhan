package githubrelease

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

var ErrGitHubReleaseTaskAlreadyRunning = errors.New("github release task is already running")

type GitHubReleaseWorkerConfig struct {
	TaskTimeout time.Duration
	MaxRetries  int
}

type GitHubReleaseWorker struct {
	cfg        *GitHubReleaseWorkerConfig
	store      GitHubReleaseStore
	downloader *GitHubReleaseDownloader
	dispatcher *GitHubReleaseEventDispatcher
	logger     *slog.Logger

	runningTasks         sync.Map
	githubReleaseVIPChan chan string
}

func NewGitHubReleaseWorker(
	cfg *GitHubReleaseWorkerConfig,
	store GitHubReleaseStore,
	dl *GitHubReleaseDownloader,
	disp *GitHubReleaseEventDispatcher,
	logger *slog.Logger,
) *GitHubReleaseWorker {
	return &GitHubReleaseWorker{
		cfg:                  cfg,
		store:                store,
		downloader:           dl,
		dispatcher:           disp,
		logger:               logger,
		githubReleaseVIPChan: make(chan string, 100),
	}
}

// RunWorker 启动 Worker 主循环
func (w *GitHubReleaseWorker) RunWorker(ctx context.Context) {
	w.dispatcher.RunDispatcher(ctx)
	go w.processVIPChannel(ctx)
	go w.processCronSchedule(ctx)
	go w.runWatchdog(ctx)
	<-ctx.Done()
}

// TriggerVIP 外部 API 调用的 VIP 触发接口
func (w *GitHubReleaseWorker) TriggerVIP(taskID string) (string, error) {
	if _, loaded := w.runningTasks.Load(taskID); loaded {
		return "", ErrGitHubReleaseTaskAlreadyRunning
	}

	execID := generateUUID()
	select {
	case w.githubReleaseVIPChan <- taskID:
		return execID, nil
	default:
		return "", errors.New("github release vip channel full")
	}
}

func (w *GitHubReleaseWorker) processVIPChannel(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case taskID := <-w.githubReleaseVIPChan:
			task, err := w.store.GetTask(ctx, taskID)
			if err != nil || !task.Enabled {
				continue
			}
			go w.executeTask(ctx, task, GitHubReleaseTriggerVIP)
		}
	}
}

func (w *GitHubReleaseWorker) processCronSchedule(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour) // 模拟 Cron 触发
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runBatch(ctx)
		}
	}
}

func (w *GitHubReleaseWorker) runBatch(ctx context.Context) {
	w.dispatcher.DispatchEvent(GitHubReleaseEvent{Type: GitHubReleaseEventBatchStarted})
	tasks, _ := w.store.ListEnabledTasks(ctx)

	for _, task := range tasks {
		if _, loaded := w.runningTasks.LoadOrStore(task.ID, struct{}{}); loaded {
			w.logger.Info("GitHubRelease task skipped in batch: already running", "task_id", task.ID)
			continue
		}
		// 常规通道串行执行
		w.executeTask(ctx, &task, GitHubReleaseTriggerCron)
		w.runningTasks.Delete(task.ID)
	}
}

func (w *GitHubReleaseWorker) executeTask(ctx context.Context, task *GitHubReleaseTask, trigger GitHubReleaseTriggerType) {
	// VIP 通道并发执行时的二次互斥校验
	if trigger == GitHubReleaseTriggerVIP {
		if _, loaded := w.runningTasks.LoadOrStore(task.ID, struct{}{}); loaded {
			return
		}
		defer w.runningTasks.Delete(task.ID)
	}

	exec := &GitHubReleaseExecution{
		ID:             generateUUID(),
		TaskID:         task.ID,
		TriggerType:    trigger,
		Status:         GitHubReleaseStatusRunning,
		ConfigSnapshot: task, // 快照隔离
		HeartbeatAt:    time.Now(),
	}
	w.store.SaveExecution(ctx, exec)
	w.dispatcher.DispatchEvent(GitHubReleaseEvent{
		Type: GitHubReleaseEventTaskStarted, ExecutionID: exec.ID, TaskID: task.ID,
	})

	assets, _ := w.store.GetPendingAssets(ctx, exec.ID)
	hasFailure := false

	for i := range assets {
		asset := &assets[i]
		if asset.Status == GitHubReleaseAssetDeadLetter || asset.Status == GitHubReleaseAssetCompleted {
			continue
		}

		exec.HeartbeatAt = time.Now()
		w.store.UpdateExecutionHeartbeat(ctx, exec)

		err := w.downloader.DownloadAsset(ctx, exec, asset, task.CustomAccelerator)
		w.store.SaveAssetDownload(ctx, asset) // 进度与状态落盘

		if err != nil {
			hasFailure = true
		}
	}

	if hasFailure {
		exec.Status = GitHubReleaseStatusPartialSuccess
	} else {
		exec.Status = GitHubReleaseStatusSuccess
	}
	now := time.Now()
	exec.FinishedAt = &now
	w.store.UpdateExecutionStatus(ctx, exec)
}

func (w *GitHubReleaseWorker) runWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timeoutThreshold := time.Now().Add(-w.cfg.TaskTimeout * 2)
			w.store.MarkTimeoutExecutions(ctx, timeoutThreshold)
		}
	}
}

func generateUUID() string {
	// 实际项目中替换为 google/uuid 等库
	return "github-release-uuid-" + time.Now().Format("20060102150405")
}
