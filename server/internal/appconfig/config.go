package appconfig

import (
	"embed"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/utils"
)

// FileInfo 文件信息结构体
type FileInfo struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	ModifiedTime  string `json:"modifiedTime"`
	Size          int64  `json:"size"`
	RootName      string `json:"rootName,omitempty"`
	Quota         int64  `json:"quota,omitempty"`
	Used          int64  `json:"used,omitempty"`
	Xxh3Hash      string `json:"xxh3Hash,omitempty"`
	Notes         string `json:"notes,omitempty"`
	DownloadCount int64  `json:"downloadCount,omitempty"`
	RecordID      uint   `json:"recordId,omitempty"`
}

// APPConfig 独立的 APP 配置结构体
type APPConfig struct {
	Name        string `yaml:"name"`
	Initialized bool   `yaml:"initialized"`
}

// TLSConfig TLS 证书配置
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// HTTPConfig HTTP 配置
type HTTPConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"` // 默认 8080
}

// HTTPSConfig HTTPS 配置
type HTTPSConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"` // 默认 8443
}

// AnonymousConfig 匿名用户配置
type AnonymousConfig struct {
	Username string `yaml:"username"` // 匿名/公开目录用户名，默认 "public"
}

// AccountConfig 账户配置（用户管理和认证相关）
type AccountConfig struct {
	Anonymous         AnonymousConfig `yaml:"anonymous"`          // 匿名用户配置
	ReservedUsernames []string        `yaml:"reserved_usernames"` // 保留用户名列表，注册时不可使用
	AllowRegistration bool            `yaml:"allow_registration"` // 是否允许匿名注册新账户，默认 false
}

// FTPConfig FTP 服务器配置
type FTPConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"` // 默认 21
	// PassivePortStart/PassivePortEnd 被动模式数据端口范围（默认 2122-2221）
	PassivePortStart int `yaml:"passive_port_start,omitempty"`
	PassivePortEnd   int `yaml:"passive_port_end,omitempty"`
}

// FTPSConfig FTPS 服务器配置
type FTPSConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"` // 默认 990
}

// WebDAVConfig WebDAV 配置
type WebDAVConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ServerConfig 服务器配置结构体
type ServerConfig struct {
	Host      string       `yaml:"host"`
	JWTSecret string       `yaml:"jwt_secret"` // JWT 签名密钥，至少 32 字符
	TLS       TLSConfig    `yaml:"tls"`
	HTTP      HTTPConfig   `yaml:"http"`
	HTTPS     HTTPSConfig  `yaml:"https"`
	FTP       FTPConfig    `yaml:"ftp"`
	FTPS      FTPSConfig   `yaml:"ftps"`
	WebDAV    WebDAVConfig `yaml:"webdav"`
}

// DirectoryConfig 目录配置结构体
type DirectoryConfig struct {
	Path  string `yaml:"path"`
	Quota int64  `yaml:"quota,omitempty"`
	// AllowUpload 是否允许向该公共目录上传文件。空（nil）表示允许（默认值）。
	// 前端 JSON 字段为 uploadEnabled；设置页未配置时保持 nil 即允许上传。
	AllowUpload *bool `yaml:"allow_upload,omitempty" json:"uploadEnabled"`
}

// UploadAllowed 返回该公共目录是否允许上传文件（未配置时默认允许）。
func (d DirectoryConfig) UploadAllowed() bool {
	return d.AllowUpload == nil || *d.AllowUpload
}

// DirectoryConfigWithStringQuota 支持字符串格式配额的目录配置结构体
type DirectoryConfigWithStringQuota struct {
	Path        string `yaml:"path"`
	Quota       string `yaml:"quota,omitempty"`
	AllowUpload *bool  `yaml:"allow_upload,omitempty"`
}

// PrivateQuotaConfig 临时文件配额配置结构体
type PrivateQuotaConfig struct {
	GlobalQuota  int64 `yaml:"global,omitempty"`
	PerUserQuota int64 `yaml:"per_user,omitempty"`
}

// PrivateQuotaConfigWithString 私有文件配额配置结构体（字符串格式）
type PrivateQuotaConfigWithString struct {
	GlobalQuota  string `yaml:"global,omitempty"`
	PerUserQuota string `yaml:"per_user,omitempty"`
}

// PrivateStorageConfigWithStringQuota 支持字符串格式配额的临时文件配置结构体
type PrivateStorageConfigWithStringQuota struct {
	Enabled bool                         `yaml:"enabled"`
	Path    string                       `yaml:"path"`
	Quota   PrivateQuotaConfigWithString `yaml:"quota,omitempty"`
}

// PrivateStorageConfig 独立的临时文件配置结构体
type PrivateStorageConfig struct {
	Enabled      bool               `yaml:"enabled"`
	Path         string             `yaml:"path"`
	Quota        PrivateQuotaConfig `yaml:"quota,omitempty"`
	GlobalQuota  int64              `yaml:"-"` // 为了向后兼容，通过Quota.GlobalQuota访问
	PerUserQuota int64              `yaml:"-"`
}

// TempConfig 临时文件配置（基于IP）
type TempConfig struct {
	Enabled           bool                 `yaml:"enabled"`
	Path              string               `yaml:"path"`
	Quota             TempFilesQuotaConfig `yaml:"quota"`               // 配额配置
	DefaultExpireDays int                  `yaml:"default_expire_days"` // 默认过期天数，默认 7
	DeleteOnDownload  bool                 `yaml:"delete_on_download"`  // 下载后是否删除，默认 true
}

// TempFilesQuotaConfig 临时文件配额配置
type TempFilesQuotaConfig struct {
	Global string `yaml:"global"` // 全局总配额 (支持 10G, 100M 等格式)
	PerIP  string `yaml:"per_ip"` // 单个IP配额 (支持 10G, 500M 等格式)
}

// TempFilesConfigWithStringQuota 支持字符串格式配额的临时文件配置（基于IP）
type TempFilesConfigWithStringQuota struct {
	Enabled           bool                 `yaml:"enabled"`
	Path              string               `yaml:"path"`
	Quota             TempFilesQuotaConfig `yaml:"quota"`
	DefaultExpireDays int                  `yaml:"default_expire_days"`
	DeleteOnDownload  bool                 `yaml:"delete_on_download"`
}

// StorageConfig 存储配置结构体（storage 父节点）
type StorageConfig struct {
	Public            PublicStorageConfig  `yaml:"public"`
	Temp              TempConfig           `yaml:"temp"`
	Private           PrivateStorageConfig `yaml:"private"`
	AllowedExtensions []string             `yaml:"allowed_extensions"`
}

// PublicStorageConfig 公开存储配置
type PublicStorageConfig struct {
	RootDirs []DirectoryConfig `yaml:"root_dirs"`
}

// UploadConfig 上传配置结构体
type UploadConfig struct {
	Enabled     bool            `yaml:"enabled"`                 // 是否允许上传到公共目录（默认 true）
	ChunkSize   int64           `yaml:"chunk_size,omitempty"`    // 分片大小，默认为10MB
	MaxFileSize int64           `yaml:"max_file_size,omitempty"` // 最大文件大小限制，默认为0（无限制）
	URLUpload   URLUploadConfig `yaml:"url_upload"`              // URL上传配置
}

// DownloadConfig 下载配置
type DownloadConfig struct {
	RateLimit DownloadRateLimitConfig `yaml:"rate_limit"`
}

// DownloadRateLimitConfig 下载频率限制配置
// MaxRequests <= 0（或未配置）表示不限制；WindowMinutes 默认 1（分钟）。
type DownloadRateLimitConfig struct {
	WindowMinutes int `yaml:"window_minutes,omitempty"` // 统计窗口（分钟），默认 1
	MaxRequests   int `yaml:"max_requests,omitempty"`   // 窗口内每个 IP 最大请求数；0/未配置 = 不限制
	LockAfter     int `yaml:"lock_after,omitempty"`     // 无效访问失败次数达此值则锁定该 IP；0/未配置 = 不锁定
	LockMinutes   int `yaml:"lock_minutes,omitempty"`   // 锁定时长（分钟），默认 10
}

// URLUploadConfig URL上传安全配置
type URLUploadConfig struct {
	// 是否启用安全限制（默认 false）
	Enabled bool `yaml:"enabled"`
	// 允许的 IP 网段列表（CIDR 格式，如 1.2.3.4/24），为空表示不限制（允许所有 IP）
	AllowedIPRanges []string `yaml:"allowed_ip_ranges"`
	// 下载 HTTPS/FTPS URL 时是否跳过 TLS 证书验证（默认 false，即验证证书）
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// Config 主配置结构体
type Config struct {
	// WorkDir 工作目录。映射到顶层 yaml 键 work_dir。
	// 为空时默认取二进制文件（fuzhan.exe）所在目录；可用 ${work_dir} 占位符被其它路径字段引用。
	WorkDir  string         `yaml:"work_dir"`
	App      APPConfig      `yaml:"app"`
	Account  AccountConfig  `yaml:"account"`
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	Upload   UploadConfig   `yaml:"upload"`
	Download DownloadConfig `yaml:"download"`
	Preview  PreviewConfig  `yaml:"preview"`
	// MimeTable 用户自定义「扩展名 -> MIME 类型」映射表。
	// 预览时用于识别 go 内置 mime.TypeByExtension 无法识别或内置表未覆盖的扩展名，
	// 用户表优先于内置表 fallback。未配置（空）时不影响既有行为。
	MimeTable map[string]string `yaml:"mime_table"`
	Log       utils.LogConfig   `yaml:"log"`
	Security  SecurityConfig    `yaml:"security"`
	Auth      AuthConfig        `yaml:"auth"`
	OpenAPI   OpenAPIConfig     `yaml:"open_api"`
	Index     IndexConfig       `yaml:"index"`    // 文件索引配置
	Resource  ResourceConfig    `yaml:"resource"` // 服务器资源监控配置
}

// ResourceConfig 服务器资源监控配置
type ResourceConfig struct {
	// Enabled 是否启用资源监控（未配置为启动时仅日志提示功能未启用）
	Enabled bool `yaml:"enabled"`
	// SamplingInterval 内存实时采样间隔（秒），默认 5。实时曲线按此频率刷新，不落库。
	SamplingInterval int `yaml:"sampling_interval,omitempty"`
	// CollectInterval 历史数据采集间隔（秒），默认 60（每分钟一条），落库并用于历史曲线。
	CollectInterval int `yaml:"collect_interval,omitempty"`
	// RetentionDays 历史数据保留天数，默认 7，超出部分定期清理。
	RetentionDays int `yaml:"retention_days,omitempty"`
}

// IndexConfig 文件索引配置
type IndexConfig struct {
	// ScanStartDelaySeconds 启动时扫描延迟（秒），默认 10
	ScanStartDelaySeconds int `yaml:"scan_start_delay_seconds"`
	// ScanCronExpression 定时全量扫描的 cron 表达式，默认 "0 1 * * *"（每天凌晨 1:00）
	ScanCronExpression string `yaml:"scan_cron_expression,omitempty"`
	// SearchReconcileCronExpression 检索索引对齐任务的 cron 表达式，
	// 默认 "0 5 * * *"（每天凌晨 5:00，与定时扫描错开）。为空使用默认值。
	SearchReconcileCronExpression string `yaml:"search_reconcile_cron_expression,omitempty"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	LDAP   LDAPClientConfig `yaml:"ldap"`
	OAuth2 OAuth2Config     `yaml:"oauth2"`
}

// OAuth2Config 外部 OAuth2/OIDC 认证配置。
// 采用授权码模式（Authorization Code）：本服务作为客户端，用户登录时跳转到外部
// 认证系统完成认证，再回调换取令牌并映射为本地用户。
type OAuth2Config struct {
	Enabled      bool     `yaml:"enabled"`
	ProviderName string   `yaml:"provider_name,omitempty"` // 显示名称，如 "企业 SSO" / "GitHub"
	ClientID     string   `yaml:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret,omitempty"`
	AuthURL      string   `yaml:"auth_url,omitempty"`      // 授权端点（OIDC 可留空，由 issuer 自动发现）
	TokenURL     string   `yaml:"token_url,omitempty"`     // 令牌端点
	UserInfoURL  string   `yaml:"user_info_url,omitempty"` // 用户信息端点
	Issuer       string   `yaml:"issuer,omitempty"`        // OIDC Issuer，用于自动发现端点（可选）
	Scopes       []string `yaml:"scopes,omitempty"`        // 请求的 scope，默认 openid profile email
	// RedirectURL 回调地址。留空时按当前请求的 scheme/host 推断，便于多域名部署。
	RedirectURL string `yaml:"redirect_url,omitempty"`
	// UsernameClaim 用户名取值字段，默认 preferred_username，回退 sub
	UsernameClaim string `yaml:"username_claim,omitempty"`
	// DisplayNameClaim 显示名取值字段，默认 name
	DisplayNameClaim string `yaml:"display_name_claim,omitempty"`
	// EmailClaim 邮箱取值字段，默认 email
	EmailClaim string `yaml:"email_claim,omitempty"`
	// AutoCreateUser 认证通过后自动创建本地用户（关闭时仅允许已存在的本地用户登录）
	AutoCreateUser bool `yaml:"auto_create_user,omitempty"`
	// InsecureSkipVerify 是否跳过 TLS 证书校验（默认 false，仅测试/自签名环境开启）
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty"`
}

// OpenAPIConfig Open API 配置
type OpenAPIConfig struct {
	Enabled           bool     `yaml:"enabled"`
	IPAccessMode      string   `yaml:"ip_access_mode"` // disable / whitelist / blacklist
	IPWhitelist       []string `yaml:"ip_whitelist"`   // CIDR 白名单
	IPBlacklist       []string `yaml:"ip_blacklist"`   // CIDR 黑名单
	RateLimitEnabled  bool     `yaml:"rate_limit_enabled"`
	RequestsPerMinute int      `yaml:"requests_per_minute"`
	BurstSize         int      `yaml:"burst_size"`
}

// LDAPClientConfig LDAP 客户端配置
type LDAPClientConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	UseSSL         bool   `yaml:"use_ssl"`
	BaseDN         string `yaml:"base_dn"`
	BindDN         string `yaml:"bind_dn"`
	BindPassword   string `yaml:"bind_password"`
	UserFilter     string `yaml:"user_filter"`
	SyncInterval   int    `yaml:"sync_interval"`
	AutoCreateUser bool   `yaml:"auto_create_user"`
	// InsecureSkipVerify 是否跳过 LDAPS 证书校验（默认 false，安全）
	// 仅测试环境或使用自签名证书时可设为 true
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	TrustProxy     bool     `yaml:"trust_proxy"`     // 是否信任代理头 (X-Forwarded-For, X-Real-IP)
	AllowedOrigins []string `yaml:"allowed_origins"` // 允许的 CORS 来源列表，* 表示所有
}

// GetSecurityConfig 获取安全配置
func GetSecurityConfig() SecurityConfig {
	configMu.RLock()
	sec := GlobalConfig.Security
	configMu.RUnlock()
	// 未配置任何来源（nil 或空列表）时表示不限制，允许所有来源
	if len(sec.AllowedOrigins) == 0 {
		sec.AllowedOrigins = []string{"*"}
	}
	return sec
}

// GetReservedUsernames 获取完整的保留用户名列表
// 配置为空（nil 或空列表）时表示不限制，仅保留系统的匿名用户名。
func GetReservedUsernames() []string {
	configMu.RLock()
	account := GlobalConfig.Account
	configMu.RUnlock()
	reserved := account.ReservedUsernames
	// 将匿名用户名动态加入保留列表：配置的匿名用户名，以及 FTP/WebDAV
	// 始终接受的 "anonymous" 别名，避免注册同名账户后被匿名通道劫持
	names := []string{account.Anonymous.Username, "anonymous"}
	for _, name := range names {
		if name == "" {
			continue
		}
		found := false
		for _, u := range reserved {
			if strings.EqualFold(u, name) {
				found = true
				break
			}
		}
		if !found {
			reserved = append(reserved, name)
		}
	}
	return reserved
}

// PreviewConfig 预览配置
type PreviewConfig struct {
	// AllowMimes 支持预览的 MIME 类型 (逗号分隔，优先匹配)
	AllowMimes string `yaml:"allow_mimes"`
	// AllowExts 支持预览的文件扩展名 (逗号分隔，作为 MIME 的补充)
	AllowExts string `yaml:"allow_exts"`
	// MaxInlineSize 直接返回内容的大小上限 (支持 1M, 2M 等格式)
	MaxInlineSize string `yaml:"max_inline_size"`
	// TextChunkSize 文本文件分块大小 (支持 50K, 100K 等格式)
	TextChunkSize string `yaml:"text_chunk_size"`
}

// PrivateFileMeta 临时文件元数据
type PrivateFileMeta struct {
	Filename   string    `json:"filename"`
	UploadTime time.Time `json:"uploadTime"`
	FileSize   int64     `json:"fileSize"`
	Owner      string    `json:"owner"`
	Code       string    `json:"code"`
	ExpireTime time.Time `json:"expireTime"`
	// 添加文件哈希用于去重
	FileHash string `json:"fileHash,omitempty"`
}

// FileReference 文件引用信息
type FileReference struct {
	FileHash    string    `json:"fileHash"`
	FilePath    string    `json:"-"` // 内部使用，不返回给前端
	FileSize    int64     `json:"fileSize"`
	RefCount    int       `json:"refCount"`
	CreatedTime time.Time `json:"createdTime"`
}

// PersistentFileMeta 持久文件元数据
type PersistentFileMeta struct {
	FileHash    string    `json:"fileHash"`
	FileSize    int64     `json:"fileSize"`
	CreatedTime time.Time `json:"createdTime"`
}

// UserIdentifier 用户标识信息
type UserIdentifier struct {
	ID        string    `json:"id"`
	IPAddress string    `json:"ipAddress"`
	CreatedAt time.Time `json:"createdAt"`
}

// UploadRecord 上传记录信息
type UploadRecord struct {
	IPAddress  string    `json:"ipAddress"`
	UploadTime time.Time `json:"uploadTime"`
	Filename   string    `json:"filename"`
	FilePath   string    `json:"path"` // 文件路径
}

// ChunkConfig 分片配置
type ChunkConfig struct {
	ChunkSize  int64  `yaml:"chunk_size"`
	MaxRetries int    `yaml:"max_retries"`
	TempDir    string `yaml:"temp_dir"`
}

// ChunkMetadata 分片元数据
type ChunkMetadata struct {
	Index int   `json:"index"` // 分片索引
	Size  int64 `json:"size"`  // 分片大小
}

// ChunkInfo 分片信息
type ChunkInfo struct {
	Index    int   `json:"index"`
	Offset   int64 `json:"offset"`
	Size     int64 `json:"size"`
	Uploaded bool  `json:"uploaded"`
}

// UploadStatus 上传状态
type UploadStatus struct {
	UploadedChunks []int       `json:"uploadedChunks"`
	Chunks         []ChunkInfo `json:"chunks"` // 分片表
	Completed      bool        `json:"completed"`
	FileSize       int64       `json:"fileSize"`    // 文件大小
	TotalChunks    int         `json:"totalChunks"` // 总分片数
	ChunkSize      int64       `json:"chunkSize"`   // 分片大小
}

// Global variables
var (
	GlobalConfig     Config
	configMu         sync.RWMutex
	EmbedAssets      embed.FS
	WorkDir          string
	UserHtmlTemplate string
	RootNames        map[string]string = map[string]string{}
)

// LockConfig 获取写锁，用于修改 GlobalConfig 时保护并发访问
func LockConfig() { configMu.Lock() }

// UnlockConfig 释放写锁
func UnlockConfig() { configMu.Unlock() }

// RLockConfig 获取读锁
func RLockConfig() { configMu.RLock() }

// RUnlockConfig 释放读锁
func RUnlockConfig() { configMu.RUnlock() }

// PublicDirUploadAllowed 判断指定公共目录（rootName 为目录路径最后一段）是否允许上传文件。
// 未找到该目录（未知 rootName）时按允许处理，避免误伤已存在的上传能力。
func PublicDirUploadAllowed(rootName string) bool {
	configMu.RLock()
	defer configMu.RUnlock()
	for _, d := range GlobalConfig.Storage.Public.RootDirs {
		if filepath.Base(d.Path) == rootName {
			return d.UploadAllowed()
		}
	}
	return true
}

// GetDownloadRateLimit 返回下载频率限制配置（供下载限流器每个请求实时读取，
// 支持配置热更新；MaxRequests <= 0 表示不限制）。
func GetDownloadRateLimit() DownloadRateLimitConfig {
	configMu.RLock()
	dl := GlobalConfig.Download.RateLimit
	configMu.RUnlock()
	return dl
}

// GetBackupsDir 获取备份目录
func GetBackupsDir() string {
	return filepath.Join(WorkDir, "backups")
}

// GetDataDir 获取数据目录（工作目录下的 data 子目录）。
// 默认 SQLite 数据文件与搜索索引均存放于此。
func GetDataDir() string {
	return filepath.Join(WorkDir, "data")
}

// ExpandWorkDirPath 将路径中的 ${work_dir} 占位符展开为工作目录绝对路径。
// 未包含占位符时原样返回，用于兼容旧配置中的相对路径。
func ExpandWorkDirPath(p string) string {
	if strings.Contains(p, "${work_dir}") {
		p = strings.ReplaceAll(p, "${work_dir}", WorkDir)
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	return p
}
