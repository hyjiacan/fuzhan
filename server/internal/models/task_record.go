package models

import "time"

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskType 任务类型
type TaskType string

const (
	TaskTypeScan             TaskType = "scan"
	TaskTypeHash             TaskType = "hash"
	TaskTypeConsistencyCheck TaskType = "consistency_check"
	TaskTypeCleanup          TaskType = "cleanup"
	TaskTypeURLDownload      TaskType = "url_download"
)

// TaskRecord 任务记录
type TaskRecord struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TaskType     string     `gorm:"size:50;not null;index" json:"taskType"`
	TaskName     string     `gorm:"size:255;not null" json:"taskName"`
	Status       string     `gorm:"size:20;not null;default:pending;index" json:"status"`
	Progress     int        `gorm:"default:0" json:"progress"`
	TotalItems   int64      `gorm:"default:0" json:"totalItems"`
	DoneItems    int64      `gorm:"default:0" json:"doneItems"`
	ErrorMessage string     `gorm:"type:text" json:"errorMessage"`
	Metadata     string     `gorm:"type:text" json:"metadata"` // JSON 格式的附加信息
	StartedAt    *time.Time `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// TableName 指定表名
func (TaskRecord) TableName() string {
	return "task_records"
}

// TaskRecordResponse 任务记录 API 响应
type TaskRecordResponse struct {
	ID           uint       `json:"id"`
	TaskType     string     `json:"taskType"`
	TaskName     string     `json:"taskName"`
	Status       string     `json:"status"`
	Progress     int        `json:"progress"`
	TotalItems   int64      `json:"totalItems"`
	DoneItems    int64      `json:"doneItems"`
	ErrorMessage string     `json:"errorMessage"`
	Metadata     string     `json:"metadata"`
	StartedAt    *time.Time `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// ToResponse 转换为响应格式
func (t *TaskRecord) ToResponse() TaskRecordResponse {
	return TaskRecordResponse{
		ID:           t.ID,
		TaskType:     t.TaskType,
		TaskName:     t.TaskName,
		Status:       t.Status,
		Progress:     t.Progress,
		TotalItems:   t.TotalItems,
		DoneItems:    t.DoneItems,
		ErrorMessage: t.ErrorMessage,
		Metadata:     t.Metadata,
		StartedAt:    t.StartedAt,
		EndedAt:      t.EndedAt,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}

// TaskListResponse 任务列表响应
type TaskListResponse struct {
	Active []TaskRecordResponse `json:"active"`
	Recent []TaskRecordResponse `json:"recent"`
}