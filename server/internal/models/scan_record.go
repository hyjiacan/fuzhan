package models

import (
    "time"

    "gorm.io/gorm"
)

// ScanRecordStatus 扫描记录状态
type ScanRecordStatus string

const (
    // ScanRecordStatusRunning 扫描进行中
    ScanRecordStatusRunning ScanRecordStatus = "running"
    // ScanRecordStatusCompleted 扫描完成
    ScanRecordStatusCompleted ScanRecordStatus = "completed"
    // ScanRecordStatusFailed 扫描失败
    ScanRecordStatusFailed ScanRecordStatus = "failed"
)

// ScanRootResult 单个根目录的扫描结果
type ScanRootResult struct {
    RootName string `json:"rootName"`
    Scanned  int64  `json:"scanned"`
    Added    int64  `json:"added"`
    Deleted  int64  `json:"deleted"`
    Error    string `json:"error,omitempty"`
}

// ScanRecord 全量扫描记录
//
// 每次全量扫描创建一条记录, 记录扫描的开始、进度、结果和错误信息。
// 通过查询此表可以追溯每次扫描的详细情况。
type ScanRecord struct {
    ID            uint           `gorm:"primaryKey" json:"id"`
    Status        ScanRecordStatus `gorm:"size:20;default:running;index" json:"status"`
    StartedAt     time.Time      `gorm:"not null" json:"startedAt"`
    EndedAt       *time.Time     `json:"endedAt,omitempty"`
    TotalFiles    int64          `gorm:"default:0" json:"totalFiles"`
    ScannedFiles  int64          `gorm:"default:0" json:"scannedFiles"`
    AddedFiles    int64          `gorm:"default:0" json:"addedFiles"`
    DeletedFiles  int64          `gorm:"default:0" json:"deletedFiles"`
    RootResults   string         `gorm:"type:text" json:"rootResults,omitempty"`
    ErrorMessage  string         `gorm:"type:text" json:"errorMessage,omitempty"`
    CreatedAt     time.Time      `json:"createdAt"`
    UpdatedAt     time.Time      `json:"updatedAt"`
    DeletedAt     gorm.DeletedAt `gorm:"index" json:"deletedAt"`
}

// TableName 指定表名
func (ScanRecord) TableName() string {
    return "scan_records"
}
