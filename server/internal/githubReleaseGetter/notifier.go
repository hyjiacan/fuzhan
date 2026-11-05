package githubrelease

import (
	"context"
	"log/slog"
	"time"
)

// GitHubReleaseNotifier 外部消费者需实现的回调接口
type GitHubReleaseNotifier interface {
	OnEvent(ctx context.Context, ev GitHubReleaseEvent) error
}

// GitHubReleaseEventDispatcher 内部事件派发器
type GitHubReleaseEventDispatcher struct {
	ch       chan GitHubReleaseEvent
	notifier GitHubReleaseNotifier
	logger   *slog.Logger
}

func NewGitHubReleaseEventDispatcher(bufSize int, notifier GitHubReleaseNotifier, logger *slog.Logger) *GitHubReleaseEventDispatcher {
	return &GitHubReleaseEventDispatcher{
		ch:       make(chan GitHubReleaseEvent, bufSize),
		notifier: notifier,
		logger:   logger,
	}
}

// DispatchEvent 非阻塞派发。若 Channel 满载，直接丢弃并记录背压日志。
func (d *GitHubReleaseEventDispatcher) DispatchEvent(ev GitHubReleaseEvent) {
	ev.Timestamp = time.Now()
	select {
	case d.ch <- ev:
		// 成功投入 Channel
	default:
		d.logger.Warn("GitHubRelease event channel full, dropping event to prevent backpressure",
			"event_type", ev.Type, "task_id", ev.TaskID)
	}
}

// RunDispatcher 启动后台 Goroutine 消费事件
func (d *GitHubReleaseEventDispatcher) RunDispatcher(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-d.ch:
				if err := d.notifier.OnEvent(ctx, ev); err != nil {
					d.logger.Error("GitHubRelease notifier consumer failed", "error", err, "event_type", ev.Type)
				}
			}
		}
	}()
}
