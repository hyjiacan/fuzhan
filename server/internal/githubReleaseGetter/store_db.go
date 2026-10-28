package githubrelease

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// GitHubReleaseDBStore 基于关系型数据库的存储实现
// 注意：内部使用 gorm 仅为当前技术选型，对外仅暴露 GitHubReleaseStore 接口
type GitHubReleaseDBStore struct {
	db *gorm.DB
}

// NewGitHubReleaseDBStore 创建数据库存储实例
func NewGitHubReleaseDBStore(db *gorm.DB) GitHubReleaseStore {
	return &GitHubReleaseDBStore{db: db}
}

func (s *GitHubReleaseDBStore) GetTask(ctx context.Context, id string) (*GitHubReleaseTask, error) {
	var task GitHubReleaseTask
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *GitHubReleaseDBStore) ListEnabledTasks(ctx context.Context) ([]GitHubReleaseTask, error) {
	var tasks []GitHubReleaseTask
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&tasks).Error
	return tasks, err
}

func (s *GitHubReleaseDBStore) SaveExecution(ctx context.Context, exec *GitHubReleaseExecution) error {
	return s.db.WithContext(ctx).Create(exec).Error
}

func (s *GitHubReleaseDBStore) UpdateExecutionHeartbeat(ctx context.Context, exec *GitHubReleaseExecution) error {
	return s.db.WithContext(ctx).
		Model(&GitHubReleaseExecution{}).
		Where("id = ?", exec.ID).
		Update("heartbeat_at", exec.HeartbeatAt).Error
}

func (s *GitHubReleaseDBStore) UpdateExecutionStatus(ctx context.Context, exec *GitHubReleaseExecution) error {
	return s.db.WithContext(ctx).
		Model(&GitHubReleaseExecution{}).
		Where("id = ?", exec.ID).
		Updates(map[string]interface{}{
			"status":      exec.Status,
			"finished_at": exec.FinishedAt,
		}).Error
}

func (s *GitHubReleaseDBStore) MarkTimeoutExecutions(ctx context.Context, threshold time.Time) error {
	return s.db.WithContext(ctx).
		Model(&GitHubReleaseExecution{}).
		Where("status = ? AND heartbeat_at < ?", GitHubReleaseStatusRunning, threshold).
		Update("status", GitHubReleaseStatusTimeout).Error
}

func (s *GitHubReleaseDBStore) GetPendingAssets(ctx context.Context, executionID string) ([]GitHubReleaseAssetDownload, error) {
	var assets []GitHubReleaseAssetDownload
	err := s.db.WithContext(ctx).
		Where("github_release_execution_id = ? AND status IN ?", executionID, []GitHubReleaseAssetStatus{
			GitHubReleaseAssetPending,
			GitHubReleaseAssetDownloading,
			GitHubReleaseAssetFailed,
		}).Find(&assets).Error
	return assets, err
}

func (s *GitHubReleaseDBStore) SaveAssetDownload(ctx context.Context, asset *GitHubReleaseAssetDownload) error {
	return s.db.WithContext(ctx).
		Where("id = ?", asset.ID).
		Assign(*asset).
		FirstOrCreate(asset).Error
}
