package models

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ApiKey API Key 模型
//
// 用于第三方系统集成认证，管理员后台生成，绑定权限范围。
type ApiKey struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	KeyID  string `gorm:"size:32;uniqueIndex;not null" json:"key_id"` // 唯一标识
	ApiKey string `gorm:"size:256;uniqueIndex;not null" json:"-"`     // API Key 密钥（bcrypt 哈希存储）
	Name   string `gorm:"size:128;not null" json:"name"`              // 应用/密钥名称
	UserID uint   `gorm:"not null;index" json:"user_id"`              // 所属用户 ID
	Scopes string `gorm:"size:512;not null" json:"scopes"`            // 权限范围（逗号分隔: open_api:reader,open_api:writer）
	// AllowedPaths 允许操作的公开目录路径（换行分隔的 /rootName[/sub] 前缀）；
	// 空值或 "*" 表示所有路径。仅对 Open API 的公开目录操作生效。
	AllowedPaths string     `gorm:"size:4096" json:"allowed_paths"`
	Status       string     `gorm:"size:20;default:active;index" json:"status"` // active / disabled
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`                     // 最后使用时间
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`                       // 过期时间（nil 表示永不过期）
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TableName 指定表名
func (ApiKey) TableName() string {
	return "api_keys"
}

// 细粒度 OpenAPI scope（按接口授权）。
// 保留 reader/writer/admin 作为兼容层级：reader 覆盖全部读取类接口，writer 额外覆盖写入类操作；
// admin 仅供内部使用，管理接口不允许授予。
const (
	ScopeOpenAPIReader   = "open_api:reader"
	ScopeOpenAPIWriter   = "open_api:writer"
	ScopeOpenAPIAdmin    = "open_api:admin"
	ScopeFilesDuplicates = "open_api:files:duplicates"
	ScopeFilesInfo       = "open_api:files:info"
	ScopeFilesExists     = "open_api:files:exists"
	ScopeFilesUpload     = "open_api:files:upload"
	ScopeFilesOverwrite  = "open_api:files:overwrite"
	ScopeFilesDelete     = "open_api:files:delete"
	ScopeFilesDeleteDir  = "open_api:files:delete_dir"
	ScopeFilesRename     = "open_api:files:rename"
)

// ScopeCatalogItem 一项可授予的 OpenAPI 接口权限
type ScopeCatalogItem struct {
	Scope string `json:"scope"`
	Desc  string `json:"desc"`
	Group string `json:"group"`
}

// OpenAPIScopeCatalog 可授予的接口权限目录（Open API 所有接口均需凭证访问）
func OpenAPIScopeCatalog() []ScopeCatalogItem {
	return []ScopeCatalogItem{
		{ScopeOpenAPIReader, "读取类接口（列表 / 搜索 / 下载 / 详情 / 存在性查询）", "兼容权限"},
		{ScopeOpenAPIWriter, "读写类接口（含上传、覆盖、删除文件/目录、重命名）", "兼容权限"},
		{ScopeFilesDuplicates, "重复文件查询", "文件信息"},
		{ScopeFilesInfo, "文件详情查询（含备注与依赖树）", "文件信息"},
		{ScopeFilesExists, "文件存在性查询", "文件信息"},
		{ScopeFilesUpload, "文件上传", "文件操作"},
		{ScopeFilesOverwrite, "覆盖已有文件", "文件操作"},
		{ScopeFilesDelete, "删除文件", "文件操作"},
		{ScopeFilesDeleteDir, "删除目录（递归删除目录及其内容）", "文件操作"},
		{ScopeFilesRename, "重命名/移动文件", "文件操作"},
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

// scopeHierarchy 定义 scope 的层级关系（包含关系）；兼容 reader/writer 授予全部读取类权限，
// 上传、覆盖、删除、重命名属于写入能力，仅 writer/admin 覆盖。
var scopeHierarchy = map[string][]string{
	ScopeOpenAPIAdmin: {
		ScopeOpenAPIReader, ScopeOpenAPIWriter, ScopeOpenAPIAdmin,
		ScopeFilesDuplicates, ScopeFilesInfo, ScopeFilesExists,
		ScopeFilesUpload, ScopeFilesOverwrite, ScopeFilesDelete, ScopeFilesDeleteDir, ScopeFilesRename,
	},
	ScopeOpenAPIWriter: {
		ScopeOpenAPIReader, ScopeOpenAPIWriter,
		ScopeFilesDuplicates, ScopeFilesInfo, ScopeFilesExists,
		ScopeFilesUpload, ScopeFilesOverwrite, ScopeFilesDelete, ScopeFilesDeleteDir, ScopeFilesRename,
	},
	ScopeOpenAPIReader: {
		ScopeOpenAPIReader, ScopeFilesDuplicates, ScopeFilesInfo, ScopeFilesExists,
	},
}

// ScopeContains 判断逗号分隔的已授权 scope 是否覆盖 required（支持 * 与层级关系）。
// 这是 scope 判定的唯一实现，RBAC 与 API Key 都必须复用它，避免层级定义分叉。
func ScopeContains(scopes, required string) bool {
	if strings.TrimSpace(scopes) == "" {
		return false
	}
	for _, s := range strings.Split(scopes, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if s == required || s == "*" {
			return true
		}
		if allowed, ok := scopeHierarchy[s]; ok {
			for _, a := range allowed {
				if a == required {
					return true
				}
			}
		}
	}
	return false
}

// HasScope 检查是否拥有指定 scope（包含层级关系）
func (k *ApiKey) HasScope(scope string) bool {
	return ScopeContains(k.Scopes, scope)
}

// NormalizeAllowedPaths 校验并规范化 API Key 的允许路径配置。
// 支持换行或逗号分隔的多条路径；空值、"*"、"/" 表示允许所有公开目录路径。
// 路径统一为 /rootName[/sub] 形式，拒绝包含 . 或 .. 的片段。
func NormalizeAllowedPaths(raw string) (string, error) {
	entries := splitAllowedPathEntries(raw)
	if len(entries) == 0 {
		return "", nil
	}
	out := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry == "*" || entry == "/" {
			return "", nil
		}
		p, ok := normalizeAllowedPath(entry)
		if !ok {
			return "", fmt.Errorf("允许路径格式非法: %s", entry)
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return "", nil
	}
	return strings.Join(out, "\n"), nil
}

// ParseAllowedPaths 解析允许路径配置并返回规范化条目；nil 表示所有路径。
// 传入值应已由 NormalizeAllowedPaths 规范化，非法条目在此静默忽略。
func ParseAllowedPaths(raw string) []string {
	entries := splitAllowedPathEntries(raw)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry == "*" || entry == "/" {
			return nil
		}
		p, ok := normalizeAllowedPath(entry)
		if !ok {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AllowsPath 判断目标路径（/rootName[/sub]）是否在允许范围内；未配置允许路径时始终允许。
func AllowsPath(raw, cleanPath string) bool {
	entries := ParseAllowedPaths(raw)
	if len(entries) == 0 {
		return true
	}
	p, ok := normalizeAllowedPath(cleanPath)
	if !ok || p == "/" {
		return false
	}
	for _, entry := range entries {
		if p == entry || strings.HasPrefix(p, entry+"/") {
			return true
		}
	}
	return false
}

// splitAllowedPathEntries 拆分允许路径配置（换行或逗号分隔，去空白，忽略空项）。
func splitAllowedPathEntries(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if v := strings.TrimSpace(f); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// normalizeAllowedPath 把路径规范化为 /rootName[/sub]；包含 . 或 .. 时返回 ok=false。
func normalizeAllowedPath(raw string) (string, bool) {
	p := filepath.ToSlash(strings.TrimSpace(raw))
	if p == "" {
		return "", false
	}
	parts := make([]string, 0, 8)
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", false
		default:
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "/", true
	}
	return "/" + strings.Join(parts, "/"), true
}

// OAuthClient OAuth 2.0 客户端注册
type OAuthClient struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ClientID     string    `gorm:"size:64;uniqueIndex;not null" json:"client_id"`
	ClientSecret string    `gorm:"size:128;not null" json:"-"`              // 加盐哈希
	Name         string    `gorm:"size:128;not null" json:"name"`           // 应用名称
	RedirectURI  string    `gorm:"size:1024;not null" json:"redirect_uri"`  // 回调 URL
	Scopes       string    `gorm:"size:512;not null" json:"scopes"`         // 允许的 Scope
	UserID       uint      `gorm:"not null;index" json:"user_id"`           // 创建者
	Status       string    `gorm:"size:20;default:active" json:"status"`    // active / disabled
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 指定表名
func (OAuthClient) TableName() string {
	return "oauth_clients"
}

// AuthRecord 认证记录（用于统一认证管理）
type AuthRecord struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"not null;index" json:"user_id"`             // 关联的本地用户 ID
	AuthType     string    `gorm:"size:32;not null;index" json:"auth_type"`   // ldap / oauth2 / local
	ExternalID   string    `gorm:"size:256;not null" json:"external_id"`      // 外部 ID（LDAP DN、OAuth subject）
	ExternalName string    `gorm:"size:128" json:"external_name"`             // 外部显示名称
	ProviderName string    `gorm:"size:64" json:"provider_name"`              // 提供方名称（如 oauth2:github, ldap:company）
	CreatedAt    time.Time `json:"created_at"`
}

// TableName 指定表名
func (AuthRecord) TableName() string {
	return "auth_records"
}
