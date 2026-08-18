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
    ID               string                 `gorm:"primaryKey;size:36" json:"id"`
    UserID           *string                `gorm:"size:64;index" json:"userID,omitempty"`
    AnonymousID      *string                `gorm:"size:36;index" json:"anonymousID,omitempty"`
    URL              string                 `gorm:"size:2048;not null" json:"url"`
    FileName         string                 `gorm:"size:255;not null" json:"fileName"`
    TargetPath       string                 `gorm:"size:512" json:"targetPath"`
    StorageType      URLDownloadStorageType `gorm:"size:20;not null;default:temp" json:"storageType"`
    Status           URLDownloadStatus      `gorm:"size:20;not null;default:pending" json:"status"`
    ErrorMessage     string                 `gorm:"size:1024" json:"errorMessage,omitempty"`
    FileSize         int64                  `gorm:"default:0" json:"fileSize"`
    DownloadedBytes  int64                  `gorm:"default:0" json:"downloadedBytes"`
    ETag             string                 `gorm:"size:255" json:"eTag,omitempty"`
    ResumeSupported  bool                   `gorm:"default:false" json:"resumeSupported"`
    ResumeURL        string                 `gorm:"size:2048" json:"resumeURL,omitempty"`
    TempFilePath     string                 `gorm:"size:512" json:"tempFilePath,omitempty"`
    Notified         bool                   `gorm:"default:false;index" json:"notified"`
    CreatedAt        time.Time              `json:"createdAt"`
    CompletedAt      *time.Time             `json:"completedAt,omitempty"`
    UpdatedAt        time.Time              `json:"updatedAt"`
}

// TableName 指定表名
func (URLDownloadTask) TableName() string {
    return "url_download_tasks"
}
