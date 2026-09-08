package models

import (
	"time"
)

// User 用户模型
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UUID         string    `gorm:"uniqueIndex;size:36;not null" json:"uuid"` // 用户唯一标识，用于存储路径
	Username     string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string    `gorm:"size:256;not null" json:"-"`
	Role         string    `gorm:"size:32;default:user" json:"role"` // 用户角色：admin, user
	Disabled     bool      `gorm:"default:false" json:"disabled"`    // 是否禁用
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}
