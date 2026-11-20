package models

import (
	"time"
)

// 认证归属（users.auth_source）取值
const (
	AuthSourceLocal  = "local"
	AuthSourceLDAP   = "ldap"
	AuthSourceOAuth2 = "oauth2"
)

// 账号创建渠道（users.created_via）取值，创建后不可变
const (
	CreatedViaSelfRegister = "self_register"
	CreatedViaAdminCreate  = "admin_create"
	CreatedViaLDAPJIT      = "ldap_jit"
	CreatedViaOAuth2JIT    = "oauth2_jit"
	CreatedViaLegacy       = "legacy"
)

// 身份状态派生值（不落库，按当前配置读取时计算）
const (
	IdentityStateOK               = "ok"
	IdentityStateOrphaned         = "orphaned"
	IdentityStateProviderMismatch = "provider_mismatch"
	IdentityStateInconsistent     = "inconsistent"
)

// User 用户模型
type User struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	UUID         string `gorm:"uniqueIndex;size:36;not null" json:"uuid"` // 用户唯一标识，用于存储路径
	Username     string `gorm:"uniqueIndex;size:64;not null" json:"username"`
	DisplayName  string `gorm:"size:128" json:"display_name"` // 姓名/显示名，LDAP/OAuth 登录时同步
	Nickname     string `gorm:"size:128" json:"nickname"`     // 昵称，展示时优先级最高
	PasswordHash string `gorm:"size:256;not null" json:"-"`
	Role         string `gorm:"size:32;default:user" json:"role"` // 用户角色：admin, user
	Disabled     bool   `gorm:"default:false" json:"disabled"`    // 是否禁用
	// AuthSource 账号归属认证源：local / ldap / oauth2。
	// 决定登录路由，只能由管理员绑定/转换操作改变，登录行为不会自动改变它。
	AuthSource string `gorm:"size:16;not null;default:local;index:idx__users__auth_source" json:"auth_source"`
	// CreatedVia 账号创建渠道：self_register / admin_create / ldap_jit / oauth2_jit / legacy。
	// 表示首次创建渠道，创建后不可变，仅用于展示与筛选。
	CreatedVia string `gorm:"size:20;not null;default:legacy" json:"created_via"`
	// IdentityNoticePending 管理员为该账号绑定外部身份后置为 true，
	// 用户下次登录时由前端弹一次提示并确认（ack）后清除，不参与接口序列化。
	IdentityNoticePending bool      `gorm:"not null;default:false" json:"-"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// DisplayNameLabel 返回对外展示名，优先级：昵称 > 姓名 > 用户名
func (u *User) DisplayNameLabel() string {
	if u == nil {
		return ""
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}
