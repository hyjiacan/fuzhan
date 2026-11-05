package models

import "time"

// LoginLog 登录日志（记录用户成功/失败的登录尝试，供个人中心查询）
type LoginLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     string    `gorm:"size:64;index" json:"userID"`                    // 用户 UUID（未关联用户时可为空）
	Username   string    `gorm:"size:128;index" json:"username"`                 // 登录用户名
	Method     string    `gorm:"size:32;not null;default:local" json:"method"`   // 登录方式：local / oauth2 / register
	ClientIP   string    `gorm:"size:45;index" json:"clientIP"`                  // 客户端 IP
	UserAgent  string    `gorm:"size:512" json:"userAgent"`                      // 客户端 UA
	Status     string    `gorm:"size:16;not null;default:success" json:"status"` // 结果：success / failed
	FailReason string    `gorm:"size:255" json:"failReason"`                     // 失败原因（Status=failed 时填写）
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`
}

// TableName 指定表名
func (LoginLog) TableName() string {
	return "login_logs"
}
