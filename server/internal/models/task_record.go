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
	ID           uint   `gorm:"primaryKey" json:"id"`
	TaskType     string `gorm:"size:50;not null;index" json:"task_type"`
	TaskName     string `gorm:"size:255;not null" json:"task_name"`
	Status       string `gorm:"size:20;not null;default:pending;index" json:"status"`
	Progress     int    `gorm:"default:0" json:"progress"`
	TotalItems   int64  `gorm:"default:0" json:"total_items"`
	DoneItems    int64  `gorm:"default:0" json:"done_items"`
	ErrorMessage string `gorm:"type:text" json:"error_message"`
	Metadata     string `gorm:"type:text" json:"metadata"` // JSON 格式的附加信息
	// Trigger 任务触发原因（如 timer/startup/manual/user/auto）
	Trigger string `gorm:"size:50" json:"trigger"`
	// Details 任务明细（可读文本，如扫描/哈希/清理统计）
	Details   string     `gorm:"type:text" json:"details"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TableName 指定表名
func (TaskRecord) TableName() string {
	return "task_records"
}

// TaskRecordResponse 任务记录 API 响应
type TaskRecordResponse struct {
	ID           uint       `json:"id"`
	TaskType     string     `json:"task_type"`
	TaskName     string     `json:"task_name"`
	Status       string     `json:"status"`
	Progress     int        `json:"progress"`
	TotalItems   int64      `json:"total_items"`
	DoneItems    int64      `json:"done_items"`
	ErrorMessage string     `json:"error_message"`
	Metadata     string     `json:"metadata"`
	Trigger      string     `json:"trigger"`
	Details      string     `json:"details"`
	StartedAt    *time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
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
		Trigger:      t.Trigger,
		Details:      t.Details,
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
