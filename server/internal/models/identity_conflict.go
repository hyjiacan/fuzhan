package models

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// 身份冲突处理结果（resolution 取值）
const (
	IdentityConflictResolutionBound   = "bound"   // 已由管理员绑定到同名本地账号
	IdentityConflictResolutionIgnored = "ignored" // 已由管理员忽略
)

// IdentityConflict 记录外部身份与本地同名账号的归属冲突。
//
// 外部认证（LDAP / OAuth2）发现同名本地账号已存在时拒绝登录，并把冲突落库，
// 供管理员在用户管理页集中裁决（绑定到同名账号 / 忽略）。
// 同一 (auth_type, provider_name, external_id) 只保留一条记录，重复触发累加尝试次数。
type IdentityConflict struct {
	ID             uint       `gorm:"primaryKey" json:"id"`                                                                     // 主键
	Username       string     `gorm:"size:64;not null;index:idx__identity_conflicts__username" json:"username"`                 // 冲突涉及的用户名
	AuthType       string     `gorm:"size:16;not null;index:idx__identity_conflicts__identity,priority:1" json:"auth_type"`     // 外部认证类型 ldap / oauth2
	ProviderName   string     `gorm:"size:64;not null;index:idx__identity_conflicts__identity,priority:2" json:"provider_name"` // provider 稳定标识
	ExternalID     string     `gorm:"size:512;not null;index:idx__identity_conflicts__identity,priority:3" json:"external_id"`  // 外部标识（LDAP DN / OAuth2 sub）
	ExternalName   string     `gorm:"size:128" json:"external_name"`                                                            // 外部显示名称（可能为空）
	RejectedSource string     `gorm:"size:16" json:"rejected_source"`                                                           // 被拒绝的本地归属（local / ldap / oauth2）
	AttemptCount   int64      `gorm:"not null;default:1" json:"attempt_count"`                                                  // 累计触发次数
	LastSeenAt     time.Time  `gorm:"not null;index:idx__identity_conflicts__last_seen_at" json:"last_seen_at"`                 // 最近一次触发时间（UTC）
	ResolvedAt     *time.Time `json:"resolved_at"`                                                                              // 处理时间（UTC），为空表示待处理
	Resolution     string     `gorm:"size:16" json:"resolution"`                                                                // 处理结果 bound / ignored
	CreatedAt      time.Time  `json:"created_at"`                                                                               // 创建时间
	UpdatedAt      time.Time  `json:"updated_at"`                                                                               // 更新时间
}

// TableName 指定表名
func (IdentityConflict) TableName() string {
	return "identity_conflicts"
}

// UpsertIdentityConflict 按 (auth_type, provider_name, external_id) 写入或更新冲突记录。
// 同一冲突再次触发时更新用户名、外部显示名、被拒绝来源，并累加尝试次数；
// 此前已被忽略的冲突会被重新打开（清空处理结果与处理时间），重新进入待处理列表。
func UpsertIdentityConflict(db *gorm.DB, conflict *IdentityConflict) error {
	if conflict.LastSeenAt.IsZero() {
		conflict.LastSeenAt = time.Now().UTC()
	}
	var existing IdentityConflict
	err := db.Where("auth_type = ? AND provider_name = ? AND external_id = ?",
		conflict.AuthType, conflict.ProviderName, conflict.ExternalID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if conflict.AttemptCount <= 0 {
			conflict.AttemptCount = 1
		}
		return db.Create(conflict).Error
	}
	if err != nil {
		return err
	}

	updates := map[string]any{
		"username":        conflict.Username,
		"rejected_source": conflict.RejectedSource,
		"attempt_count":   existing.AttemptCount + 1,
		"last_seen_at":    conflict.LastSeenAt,
		"updated_at":      time.Now().UTC(),
	}
	// 外部显示名可能缺失，缺失时不覆盖已记录的值
	if conflict.ExternalName != "" {
		updates["external_name"] = conflict.ExternalName
	}
	// 忽略后的冲突再次触发：重新打开，回到待处理状态
	if existing.Resolution == IdentityConflictResolutionIgnored {
		updates["resolved_at"] = nil
		updates["resolution"] = ""
	}
	return db.Model(&IdentityConflict{}).Where("id = ?", existing.ID).Updates(updates).Error
}
