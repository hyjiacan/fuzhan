package models

import (
	"time"
)

// URLDownloadStatus URL 下载任务状态
type URLDownloadStatus string

const (
	URLDownloadStatusPending     URLDownloadStatus = "pending"     // 等待下载
	URLDownloadStatusDownloading URLDownloadStatus = "downloading" // 下载中
	URLDownloadStatusCompleted   URLDownloadStatus = "completed"   // 下载完成
	URLDownloadStatusFailed      URLDownloadStatus = "failed"      // 下载失败
	URLDownloadStatusCancelled   URLDownloadStatus = "cancelled"   // 已取消
)

// URLDownloadStorageType 存储类型
type URLDownloadStorageType string

const (
	URLDownloadStorageTemp    URLDownloadStorageType = "temp"    // 临时文件
	URLDownloadStoragePrivate URLDownloadStorageType = "private" // 私有存储
	URLDownloadStorageRegular URLDownloadStorageType = "regular" // 普通文件
)

// URLDownloadTask URL 下载任务
type URLDownloadTask struct {
	ID              string                 `gorm:"primaryKey;size:36" json:"id"`
	UserID          *string                `gorm:"size:64;index" json:"userID,omitempty"`
	AnonymousID     *string                `gorm:"size:36;index" json:"anonymousID,omitempty"`
	URL             string                 `gorm:"size:2048;not null" json:"url"`
	FileName        string                 `gorm:"size:255;not null" json:"fileName"`
	TargetPath      string                 `gorm:"size:512" json:"targetPath"`
	StorageType     URLDownloadStorageType `gorm:"size:20;not null;default:temp" json:"storageType"`
	RootName        string                 `gorm:"size:64" json:"rootName,omitempty"` // 目标根目录名，供重试还原会话上下文
	Status          URLDownloadStatus      `gorm:"size:20;not null;default:pending;index:idx_url_task_status_queued,priority:1" json:"status"`
	ErrorMessage    string                 `gorm:"size:1024" json:"errorMessage,omitempty"`
	FileSize        int64                  `gorm:"default:0" json:"fileSize"`
	DownloadedBytes int64                  `gorm:"default:0" json:"downloadedBytes"`
	ETag            string                 `gorm:"size:255" json:"eTag,omitempty"`
	ResumeSupported bool                   `gorm:"default:false" json:"resumeSupported"`
	ResumeURL       string                 `gorm:"size:2048" json:"resumeURL,omitempty"`
	TempFilePath    string                 `gorm:"size:512" json:"tempFilePath,omitempty"`
	// ClientIP 入队时的请求方 IP，供后台队列 worker 还原临时文件归属与操作记录
	ClientIP string `gorm:"size:64" json:"clientIP,omitempty"`
	// SessionDir 目标目录（会话语义，相对根目录或临时目录），供 worker 落库相对路径
	SessionDir string `gorm:"size:512" json:"sessionDir,omitempty"`
	// RetryCount 已自动重试次数（不含首次尝试）
	RetryCount int `gorm:"default:0" json:"retryCount"`
	// Priority 调度优先级：0=新任务/手动重试（高），1=自动重试（低）。
	// 自动重试永远排在新任务与手动重试之后，保证用户可见任务优先。
	Priority int `gorm:"default:0" json:"priority"`
	// QueuedAt 入队时间，用于调度排序；重试时刷新以排到队尾（旧的未设置记录视为最早，优先领取）
	// 与 status 组成复合索引，支撑调度热路径的 WHERE status=? ORDER BY queued_at 查询。
	QueuedAt    time.Time  `gorm:"index;index:idx_url_task_status_queued,priority:2" json:"queuedAt"`
	Notified    bool       `gorm:"default:false;index" json:"notified"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// TableName 指定表名
func (URLDownloadTask) TableName() string {
	return "url_download_tasks"
}
