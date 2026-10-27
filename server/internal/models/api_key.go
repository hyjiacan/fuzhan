package models

import (
	"strings"
	"time"
)

// ApiKey API Key 模型
//
// 用于第三方系统集成认证，管理员后台生成，绑定权限范围。
type ApiKey struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	KeyID      string     `gorm:"size:32;uniqueIndex;not null" json:"keyId"`  // 唯一标识
	ApiKey     string     `gorm:"size:256;uniqueIndex;not null" json:"-"`     // API Key 密钥（bcrypt 哈希存储）
	Name       string     `gorm:"size:128;not null" json:"name"`              // 应用/密钥名称
	UserID     uint       `gorm:"not null;index" json:"userId"`               // 所属用户 ID
	Scopes     string     `gorm:"size:512;not null" json:"scopes"`            // 权限范围（逗号分隔: open_api:reader,open_api:writer）
	Status     string     `gorm:"size:20;default:active;index" json:"status"` // active / disabled
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`                       // 最后使用时间
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`                        // 过期时间（nil 表示永不过期）
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// TableName 指定表名
func (ApiKey) TableName() string {
	return "api_keys"
}

// 细粒度 OpenAPI scope（按接口授权）。
// 保留 reader/writer/admin 作为兼容层级：reader/writer 覆盖全部读取类接口，
// admin 仅供内部使用，管理接口不允许授予。
const (
	ScopeOpenAPIReader   = "open_api:reader"
	ScopeOpenAPIWriter   = "open_api:writer"
	ScopeOpenAPIAdmin    = "open_api:admin"
	ScopeFilesDuplicates = "open_api:files:duplicates"
	ScopeFilesNotes      = "open_api:files:notes"
	ScopeFilesDepends    = "open_api:files:depends"
)

// ScopeCatalogItem 一项可授予的 OpenAPI 接口权限
type ScopeCatalogItem struct {
	Scope string `json:"scope"`
	Desc  string `json:"desc"`
	Group string `json:"group"`
}

// OpenAPIScopeCatalog 细粒度接口权限目录（仅包含需要认证的接口，公开接口无需授权）
func OpenAPIScopeCatalog() []ScopeCatalogItem {
	return []ScopeCatalogItem{
		{ScopeFilesDuplicates, "重复文件查询", "文件信息"},
		{ScopeFilesNotes, "文件备注查询", "文件信息"},
		{ScopeFilesDepends, "文件依赖树查询", "文件信息"},
	}
}

// IsGrantableScope 判断 scope 是否允许通过管理接口授予
func IsGrantableScope(scope string) bool {
	if scope == ScopeOpenAPIReader || scope == ScopeOpenAPIWriter {
		return true
	}
	for _, item := range OpenAPIScopeCatalog() {
		if item.Scope == scope {
			return true
		}
	}
	return false
}

// scopeHierarchy 定义 scope 的层级关系（包含关系）；兼容 reader/writer 授予全部读取类权限
var scopeHierarchy = map[string][]string{
	ScopeOpenAPIAdmin: {
		ScopeOpenAPIReader, ScopeOpenAPIWriter, ScopeOpenAPIAdmin,
		ScopeFilesDuplicates, ScopeFilesNotes, ScopeFilesDepends,
	},
	ScopeOpenAPIWriter: {
		ScopeOpenAPIReader, ScopeOpenAPIWriter,
		ScopeFilesDuplicates, ScopeFilesNotes, ScopeFilesDepends,
	},
	ScopeOpenAPIReader: {
		ScopeOpenAPIReader, ScopeFilesDuplicates, ScopeFilesNotes, ScopeFilesDepends,
	},
}

// HasScope 检查是否拥有指定 scope（包含层级关系）
func (k *ApiKey) HasScope(scope string) bool {
	if k.Scopes == "" {
		return false
	}
	for _, s := range strings.Split(k.Scopes, ",") {
		s = strings.TrimSpace(s)
		if s == scope || s == "*" {
			return true
		}
		// 检查层级关系
		if allowed, ok := scopeHierarchy[s]; ok {
			for _, a := range allowed {
				if a == scope {
					return true
				}
			}
		}
	}
	return false
}

// OAuthClient OAuth 2.0 客户端注册
type OAuthClient struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ClientID     string    `gorm:"size:64;uniqueIndex;not null" json:"clientId"`
	ClientSecret string    `gorm:"size:128;not null" json:"-"`            // 加盐哈希
	Name         string    `gorm:"size:128;not null" json:"name"`         // 应用名称
	RedirectURI  string    `gorm:"size:1024;not null" json:"redirectUri"` // 回调 URL
	Scopes       string    `gorm:"size:512;not null" json:"scopes"`       // 允许的 Scope
	UserID       uint      `gorm:"not null;index" json:"userId"`          // 创建者
	Status       string    `gorm:"size:20;default:active" json:"status"`  // active / disabled
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TableName 指定表名
func (OAuthClient) TableName() string {
	return "oauth_clients"
}

// AuthRecord 认证记录（用于统一认证管理）
type AuthRecord struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"not null;index" json:"userId"`           // 关联的本地用户 ID
	AuthType     string    `gorm:"size:32;not null;index" json:"authType"` // ldap / oauth2 / local
	ExternalID   string    `gorm:"size:256;not null" json:"externalId"`    // 外部 ID（LDAP DN、OAuth subject）
	ExternalName string    `gorm:"size:128" json:"externalName"`           // 外部显示名称
	ProviderName string    `gorm:"size:64" json:"providerName"`            // 提供方名称（如 oauth2:github, ldap:company）
	CreatedAt    time.Time `json:"createdAt"`
}

// TableName 指定表名
func (AuthRecord) TableName() string {
	return "auth_records"
}
