package models

import "time"

// 告警消息级别
const (
	AlertLevelWarning  = "warning"  // 预警：达到告警阈值
	AlertLevelCritical = "critical" // 严重：接近容量上限
)

// AlertMessage 存储告警消息，仅管理员可见（Web 消息通道）。
type AlertMessage struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Level        string    `gorm:"size:16;not null;default:warning" json:"level"`
	Title        string    `gorm:"size:255;not null" json:"title"`
	Content      string    `gorm:"type:text" json:"content"`
	UsagePercent float64   `gorm:"not null;default:0" json:"usagePercent"`
	RootName     string    `gorm:"size:64" json:"rootName"`
	Read         bool      `gorm:"column:is_read;not null;default:false;index" json:"read"`
	CreatedAt    time.Time `gorm:"index" json:"createdAt"`
}

// TableName 指定表名
func (AlertMessage) TableName() string {
	return "alert_messages"
}
