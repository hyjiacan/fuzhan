package githubrelease

import "time"

// --- 枚举与常量 ---
type GitHubReleaseTriggerType string

const (
	GitHubReleaseTriggerCron GitHubReleaseTriggerType = "cron"
	GitHubReleaseTriggerVIP  GitHubReleaseTriggerType = "vip"
)

type GitHubReleaseExecutionStatus string

const (
	GitHubReleaseStatusRunning        GitHubReleaseExecutionStatus = "running"
	GitHubReleaseStatusSuccess        GitHubReleaseExecutionStatus = "success"
	GitHubReleaseStatusPartialSuccess GitHubReleaseExecutionStatus = "partial_success"
	GitHubReleaseStatusFailed         GitHubReleaseExecutionStatus = "failed"
	GitHubReleaseStatusTimeout        GitHubReleaseExecutionStatus = "timeout"
)

type GitHubReleaseAssetStatus string

const (
	GitHubReleaseAssetPending      GitHubReleaseAssetStatus = "pending"
	GitHubReleaseAssetDownloading  GitHubReleaseAssetStatus = "downloading"
	GitHubReleaseAssetCompleted    GitHubReleaseAssetStatus = "completed"
	GitHubReleaseAssetFailed       GitHubReleaseAssetStatus = "failed"
	GitHubReleaseAssetDeadLetter   GitHubReleaseAssetStatus = "dead_letter"
	GitHubReleaseAssetChecksumFail GitHubReleaseAssetStatus = "checksum_mismatch"
)

type GitHubReleaseEventType string

const (
	GitHubReleaseEventBatchStarted       GitHubReleaseEventType = "batch_started"
	GitHubReleaseEventTaskStarted        GitHubReleaseEventType = "task_started"
	GitHubReleaseEventAssetDownloadStart GitHubReleaseEventType = "asset_download_started"
	GitHubReleaseEventAssetDownloadDone  GitHubReleaseEventType = "asset_download_completed"
	GitHubReleaseEventAssetDownloadFail  GitHubReleaseEventType = "asset_download_failed"
)

// --- 数据库模型 ---

// GitHubReleaseTask 对应表: github_release_tasks
type GitHubReleaseTask struct {
	ID                string    `gorm:"column:id;primaryKey;type:varchar(36)" json:"id"`
	Owner             string    `gorm:"column:owner;not null;type:varchar(100)" json:"owner"`
	Repo              string    `gorm:"column:repo;not null;type:varchar(100)" json:"repo"`
	TagFilter         string    `gorm:"column:tag_filter;type:varchar(255)" json:"tag_filter"`
	AssetFilter       string    `gorm:"column:asset_filter;type:varchar(255)" json:"asset_filter"`
	CustomOutputDir   string    `gorm:"column:custom_output_dir;type:varchar(500)" json:"custom_output_dir"`
	CustomAccelerator string    `gorm:"column:custom_accelerator;type:varchar(500)" json:"custom_accelerator"`
	Enabled           bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt         time.Time `gorm:"column:created_at;not null;autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null;autoUpdateTime" json:"updated_at"`
}

func (GitHubReleaseTask) TableName() string { return "github_release_tasks" }

// GitHubReleaseExecution 对应表: github_release_executions
type GitHubReleaseExecution struct {
	ID             string                       `gorm:"column:id;primaryKey;type:varchar(36)" json:"id"`
	TaskID         string                       `gorm:"column:github_release_task_id;not null;index;type:varchar(36)" json:"task_id"`
	TriggerType    GitHubReleaseTriggerType     `gorm:"column:trigger_type;not null;type:varchar(20)" json:"trigger_type"`
	Status         GitHubReleaseExecutionStatus `gorm:"column:status;not null;index;type:varchar(30)" json:"status"`
	ConfigSnapshot *GitHubReleaseTask           `gorm:"column:config_snapshot;type:json" json:"config_snapshot"`
	HeartbeatAt    time.Time                    `gorm:"column:heartbeat_at;type:timestamp" json:"heartbeat_at"`
	FinishedAt     *time.Time                   `gorm:"column:finished_at;type:timestamp" json:"finished_at"`
}

func (GitHubReleaseExecution) TableName() string { return "github_release_executions" }

// GitHubReleaseAssetDownload 对应表: github_release_asset_downloads
type GitHubReleaseAssetDownload struct {
	ID              string                   `gorm:"column:id;primaryKey;type:varchar(36)" json:"id"`
	ExecutionID     string                   `gorm:"column:github_release_execution_id;not null;index;type:varchar(36)" json:"execution_id"`
	TaskID          string                   `gorm:"column:github_release_task_id;not null;index;type:varchar(36)" json:"task_id"`
	AssetName       string                   `gorm:"column:asset_name;not null;type:varchar(255)" json:"asset_name"`
	OriginalURL     string                   `gorm:"column:original_url;not null;type:text" json:"original_url"`
	Status          GitHubReleaseAssetStatus `gorm:"column:status;not null;index;type:varchar(30)" json:"status"`
	BytesTotal      int64                    `gorm:"column:bytes_total;type:bigint" json:"bytes_total"`
	BytesDownloaded int64                    `gorm:"column:bytes_downloaded;not null;default:0;type:bigint" json:"bytes_downloaded"`
	SupportsRange   bool                     `gorm:"column:supports_range;type:boolean" json:"supports_range"`
	LocalPath       string                   `gorm:"column:local_path;type:varchar(1000)" json:"local_path"`
	RetryCount      int                      `gorm:"column:retry_count;not null;default:0;type:int" json:"retry_count"`
	MaxRetries      int                      `gorm:"column:max_retries;not null;type:int" json:"max_retries"`
	ChecksumAlgo    string                   `gorm:"column:checksum_algo;type:varchar(20)" json:"checksum_algo"`
	ChecksumExpect  string                   `gorm:"column:checksum_expect;type:varchar(128)" json:"checksum_expect"`
	LastError       string                   `gorm:"column:last_error;type:text" json:"last_error"`
	UpdatedAt       time.Time                `gorm:"column:updated_at;not null;autoUpdateTime" json:"updated_at"`
}

func (GitHubReleaseAssetDownload) TableName() string { return "github_release_asset_downloads" }

// GitHubReleaseEvent 事件模型 (用于内存派发或持久化流水)
type GitHubReleaseEvent struct {
	Type        GitHubReleaseEventType `json:"type"`
	ExecutionID string                 `json:"execution_id"`
	TaskID      string                 `json:"task_id"`
	AssetID     string                 `json:"asset_id,omitempty"`
	Payload     map[string]any         `json:"payload"`
	Timestamp   time.Time              `json:"timestamp"`
}
