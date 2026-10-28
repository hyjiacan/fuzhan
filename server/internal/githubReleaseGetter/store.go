package githubrelease

import (
	"context"
	"time"
)

// GitHubReleaseStore 数据库访问接口
type GitHubReleaseStore interface {
	GetTask(ctx context.Context, id string) (*GitHubReleaseTask, error)
	ListEnabledTasks(ctx context.Context) ([]GitHubReleaseTask, error)

	SaveExecution(ctx context.Context, exec *GitHubReleaseExecution) error
	UpdateExecutionHeartbeat(ctx context.Context, exec *GitHubReleaseExecution) error
	UpdateExecutionStatus(ctx context.Context, exec *GitHubReleaseExecution) error
	MarkTimeoutExecutions(ctx context.Context, threshold time.Time) error

	GetPendingAssets(ctx context.Context, executionID string) ([]GitHubReleaseAssetDownload, error)
	SaveAssetDownload(ctx context.Context, asset *GitHubReleaseAssetDownload) error
}
