package appconfig

import (
	"embed"
	"path/filepath"
	"sort"
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
	// RequireLogin 是否强制登录：开启后整个 Web 界面（含管理后台）仅登录用户可交互，
	// 上传/下载/预览均需登录；临时文件分享（/temp/**）、私有分享（/share/:code）、
	// 初始化引导与登录接口保持匿名例外。默认 false（允许匿名访问）。
	RequireLogin bool `yaml:"require_login"`
	// Captcha 登录/注册图形验证码
	Captcha CaptchaConfig `yaml:"captcha"`
}

const (
	// CaptchaModeDisabled 验证码始终关闭
	CaptchaModeDisabled = "disabled"
	// CaptchaModeAlways 验证码始终开启
	CaptchaModeAlways = "always"
	// CaptchaModeAfterN 失败 N 次后开启验证码
	CaptchaModeAfterN = "after_n"
	// DefaultCaptchaTriggerAfter after_n 模式的默认失败阈值
	DefaultCaptchaTriggerAfter = 3
)

// CaptchaConfig 图形验证码配置
type CaptchaConfig struct {
	// Mode: disabled | always | after_n（禁用/始终开启/失败 N 次后开启）
	Mode string `yaml:"mode"`
	// TriggerAfter after_n 模式下触发验证码的失败次数阈值（<=0 时按默认 3）
	TriggerAfter int `yaml:"trigger_after"`
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

// 存储后端类型常量
const (
	StorageBackendLocal  = "local"  // 本地磁盘（默认）
	StorageBackendS3     = "s3"     // S3 兼容协议（AWS S3）
	StorageBackendMinIO  = "minio"  // S3 兼容协议（MinIO）
	StorageBackendOSS    = "oss"    // 阿里云 OSS
	StorageBackendWebDAV = "webdav" // WebDAV
)

// StorageConfig 存储配置结构体（storage 父节点）
type StorageConfig struct {
	// Backend 存储后端类型：local / s3 / minio / oss / webdav，默认 local
	Backend           string               `yaml:"backend"`
	Public            PublicStorageConfig  `yaml:"public"`
	Temp              TempConfig           `yaml:"temp"`
	Private           PrivateStorageConfig `yaml:"private"`
	AllowedExtensions []string             `yaml:"allowed_extensions"`
	S3                S3StorageConfig      `yaml:"s3,omitempty"`     // S3/MinIO 后端连接配置
	OSS               OSSStorageConfig     `yaml:"oss,omitempty"`    // 阿里云 OSS 后端连接配置
	WebDAV            WebDAVStorageConfig  `yaml:"webdav,omitempty"` // WebDAV 后端连接配置
}

// S3StorageConfig S3/MinIO 后端连接配置（storage.backend=s3/minio 时使用）
type S3StorageConfig struct {
	Endpoint  string `yaml:"endpoint"`          // S3 端点，如 https://s3.amazonaws.com 或 http://127.0.0.1:9000
	AccessKey string `yaml:"access_key"`        // AccessKey
	SecretKey string `yaml:"secret_key"`        // SecretKey
	Bucket    string `yaml:"bucket"`            // Bucket 名称
	Region    string `yaml:"region,omitempty"`  // 区域（MinIO 可留空）
	UseSSL    bool   `yaml:"use_ssl,omitempty"` // 是否启用 HTTPS
}

// OSSStorageConfig 阿里云 OSS 后端连接配置（storage.backend=oss 时使用）
type OSSStorageConfig struct {
	Endpoint  string `yaml:"endpoint"`   // OSS 端点，如 oss-cn-hangzhou.aliyuncs.com
	AccessKey string `yaml:"access_key"` // AccessKey
	SecretKey string `yaml:"secret_key"` // SecretKey
	Bucket    string `yaml:"bucket"`     // Bucket 名称
}

// WebDAVStorageConfig WebDAV 后端连接配置（storage.backend=webdav 时使用）
type WebDAVStorageConfig struct {
	URL      string `yaml:"url"`                // WebDAV 服务地址，如 https://example.com/dav
	Username string `yaml:"username,omitempty"` // 用户名
	Password string `yaml:"password,omitempty"` // 密码
}

// StorageBackendIsRemote 判断存储后端是否为远端存储（非本地磁盘）。
// 远端后端的根目录 Path 是虚拟前缀而非本地路径，目录存在性/建目录等磁盘操作需要降级跳过。
func StorageBackendIsRemote() bool {
	switch GlobalConfig.Storage.Backend {
	case StorageBackendS3, StorageBackendMinIO, StorageBackendOSS, StorageBackendWebDAV:
		return true
	default:
		return false
	}
}

// PublicStorageConfig 公开存储配置
type PublicStorageConfig struct {
	RootDirs []DirectoryConfig `yaml:"root_dirs"`
}

// UploadConfig 上传配置结构体
type UploadConfig struct {
	Enabled     bool                  `yaml:"enabled"`                 // 是否允许上传到公共目录（默认 true）
	ChunkSize   int64                 `yaml:"chunk_size,omitempty"`    // 分片大小，默认为10MB
	MaxFileSize int64                 `yaml:"max_file_size,omitempty"` // 最大文件大小限制，默认为0（无限制）
	URLUpload   URLUploadConfig       `yaml:"url_upload"`              // URL上传配置
	RateLimit   UploadRateLimitConfig `yaml:"rate_limit"`              // 上传频率限制配置
}

// UploadRateLimitConfig 上传频率限制配置（按客户端 IP 统计）
// 作用于上传控制面接口（创建会话/完成上传/URL 上传提交），分片数据面请求不受限。
// MaxRequests <= 0（或未配置）表示不限制；WindowMinutes 默认 1（分钟）。
type UploadRateLimitConfig struct {
	WindowMinutes int `yaml:"window_minutes,omitempty"` // 统计窗口（分钟），默认 1
	MaxRequests   int `yaml:"max_requests,omitempty"`   // 窗口内每个 IP 最大上传操作数；0/未配置 = 不限制
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
	// URL 上传总开关（默认 false）：关闭时前端隐藏入口且后端拒绝新建/探测任务；
	// 开启后同时启用 SSRF 安全限制（按 AllowedIPRanges 校验目标地址）
	Enabled bool `yaml:"enabled"`
	// 允许的 IP 网段列表（CIDR 格式，如 1.2.3.4/24），为空表示不限制（允许所有 IP）
	AllowedIPRanges []string `yaml:"allowed_ip_ranges"`
	// 下载 HTTPS/FTPS URL 时是否跳过 TLS 证书验证（默认 false，即验证证书）
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
	// 全局并行 URL 上传（下载）任务数上限（<=0 时使用默认值 2）
	MaxConcurrent int `yaml:"max_concurrent"`
	// URL 上传任务失败后的自动重试次数上限（<=0 时使用默认值 3）。
	// 仅在错误可重试（非 4xx 客户端错误）时自动重试，重试任务重新排到队尾。
	MaxRetries int `yaml:"max_retries"`
	// 服务器侧大小硬上限（字节），0 表示不限制。
	// 下载过程中实际累计写入的字节数超过该值时立即中断并清理已写内容，
	// 防止可伪造的 fileSize 与超大响应导致磁盘写满。
	MaxSize int64 `yaml:"max_size"`
}

// DefaultURLUploadMaxConcurrent URL 上传默认全局并行数
const DefaultURLUploadMaxConcurrent = 2

// DefaultURLUploadMaxRetries URL 上传默认自动重试次数
const DefaultURLUploadMaxRetries = 3

// DDosConfig 高频/DDoS 防护配置（默认开启，超阈值对该 IP 限流拦截并记安全事件）。
// 独立顶层节（不放 security 下），避免配置 UI 增量保存整节覆盖丢字段。
// Enabled=false 时检测器不启动；WindowSeconds/MaxRequests/CooldownMinutes <=0 时使用默认值。
type DDosConfig struct {
	Enabled         bool `yaml:"enabled,omitempty"`          // 是否启用高频检测（默认 true）
	WindowSeconds   int  `yaml:"window_seconds,omitempty"`   // 统计窗口（秒），默认 10
	MaxRequests     int  `yaml:"max_requests,omitempty"`     // 窗口内单 IP 请求数阈值，默认 200
	CooldownMinutes int  `yaml:"cooldown_minutes,omitempty"` // 拦截/告警冷却（分钟），默认 10
}

// WebhookEndpoint 单条 Webhook 回调地址记录。
// 每个地址可独立启用/命名/配置密钥/超时/重试/事件过滤与接收方鉴权（headers/query），
// 空或 0 的字段回退到 WebhookConfig 全局默认值。
type WebhookEndpoint struct {
	Name           string            `yaml:"name,omitempty"`            // 显示名称
	URL            string            `yaml:"url"`                       // 回调地址（HTTP POST 目标）
	Enabled        *bool             `yaml:"enabled,omitempty"`         // nil 视为启用
	Secret         string            `yaml:"secret,omitempty"`          // 空继承全局签名密钥
	TimeoutSeconds int               `yaml:"timeout_seconds,omitempty"` // 0 继承全局超时（秒）
	MaxRetries     int               `yaml:"max_retries,omitempty"`     // 0 继承全局重试次数
	Events         []string          `yaml:"events,omitempty"`          // 事件权限：空=不接收任何事件；支持精确/前缀通配/*
	Headers        map[string]string `yaml:"headers,omitempty"`         // 接收方鉴权等自定义请求头
	Query          map[string]string `yaml:"query,omitempty"`           // 追加到回调 URL 的查询参数
}

// IsEnabled 返回端点是否启用：Enabled == nil 表示未显式指定，视为启用。
func (e WebhookEndpoint) IsEnabled() bool {
	return e.Enabled == nil || *e.Enabled
}

// MatchesEvent 判断事件名是否命中该端点的事件权限规则（默认拒绝）。
// 空事件列表表示不接收任何事件；支持 `*`（全部）、精确（file.uploaded）与前缀通配（file.*）。
func (e WebhookEndpoint) MatchesEvent(name string) bool {
	if len(e.Events) == 0 {
		return false
	}
	for _, rule := range e.Events {
		if rule == "" || rule == "*" || rule == name {
			return true
		}
		if len(rule) > 2 && strings.HasSuffix(rule, ".*") && strings.HasPrefix(name, rule[:len(rule)-1]) {
			return true
		}
	}
	return false
}

// WebhookConfig Webhook 通知配置。
// 独立顶层节，启用后应用内关键事件（任务完成/失败、安全事件、文件与用户操作等）以
// HTTP POST 推送到 endpoints 中的回调地址（按端点事件权限过滤）。带 HMAC-SHA256 签名与指数退避重试。
// Enabled=false 时派发器不发送；TimeoutSeconds/MaxRetries/QueueSize <=0 时使用默认值。
// URLs 为旧版字段，仅用于兼容读取（写入时统一使用 endpoints），不再写入。
type WebhookConfig struct {
	Enabled        bool              `yaml:"enabled,omitempty"`         // 是否启用 Webhook 推送（默认 false）
	URLs           []string          `yaml:"urls,omitempty"`            // 旧版回调地址列表（仅兼容读取）
	Endpoints      []WebhookEndpoint `yaml:"endpoints,omitempty"`       // 回调地址记录列表（每地址独立配置）
	Secret         string            `yaml:"secret,omitempty"`          // 默认签名密钥，用于生成 X-Fuzhan-Signature 报头
	TimeoutSeconds int               `yaml:"timeout_seconds,omitempty"` // 默认单次请求超时（秒），默认 10
	MaxRetries     int               `yaml:"max_retries,omitempty"`     // 默认失败重试次数，默认 3（指数退避）
	QueueSize      int               `yaml:"queue_size,omitempty"`      // 进程内事件队列容量，默认 1024，满载丢弃并告警
}

// EffectiveEndpoints 返回参与派发的端点列表：
// endpoints 非空时返回其副本；否则把旧版 urls 转为启用端点（兼容旧配置）。
func (c WebhookConfig) EffectiveEndpoints() []WebhookEndpoint {
	if len(c.Endpoints) > 0 {
		out := make([]WebhookEndpoint, len(c.Endpoints))
		copy(out, c.Endpoints)
		return out
	}
	if len(c.URLs) > 0 {
		out := make([]WebhookEndpoint, 0, len(c.URLs))
		for _, u := range c.URLs {
			if u == "" {
				continue
			}
			// 旧版 urls 语义为「接收全部事件」，转换为端点时显式写入 `*` 保持行为一致
			out = append(out, WebhookEndpoint{URL: u, Events: []string{"*"}})
		}
		return out
	}
	return nil
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
	DDos     DDosConfig     `yaml:"ddos"`
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
	Webhook   WebhookConfig     `yaml:"webhook"`  // Webhook 通知配置
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
	IPAccessMode      string   `yaml:"ip_access_mode"`   // disable / whitelist / blacklist
	IPWhitelist       []string `yaml:"ip_whitelist"`     // CIDR 白名单
	IPBlacklist       []string `yaml:"ip_blacklist"`     // CIDR 黑名单
	DomainWhitelist   []string `yaml:"domain_whitelist"` // 域名白名单（白名单模式下生效）
	RateLimitEnabled  bool     `yaml:"rate_limit_enabled"`
	RequestsPerMinute int      `yaml:"requests_per_minute"`
	BurstSize         int      `yaml:"burst_size"`
	// AuthMode 鉴权方式：none（匿名，默认）/ api_key（系统签发的 API Key + scope）。
	// 与 Web 登录态完全隔离：本项只影响 Open API，不接受 Web 会话 token，也不会签发 Web 会话。
	// 注意：开启「强制登录」后本项不能为 none，否则 Open API 会被直接拒绝（强制登录优先级最高）。
	AuthMode string `yaml:"auth_mode"`
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
	// 未配置任何来源（nil 或空列表）时仅允许同源访问，
	// 不默认放行跨站（不再注入 "*"）。如需跨站需显式配置 allowed_origins。
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

// PreviewGroupConfig 单个预览分组配置：开关 + 可编辑的 MIME/扩展名列表。
// Enabled 使用指针区分「未配置」与显式开关：内置分组未配置视为开启，custom 组未配置视为关闭；
// Mimes/Exts 使用指针区分「未配置」（回退 utils 内置默认表）与显式留空（该组不命中任何文件）。
type PreviewGroupConfig struct {
	// Enabled 分组开关（内置分组 nil 视为开启，custom 组 nil 视为关闭）
	Enabled *bool `yaml:"enabled,omitempty"`
	// Mimes 该组匹配的 MIME 类型（逗号分隔，支持通配符，如 image/*）
	Mimes *string `yaml:"mimes,omitempty"`
	// Exts 该组匹配的文件扩展名（逗号分隔）
	Exts *string `yaml:"exts,omitempty"`
}

// PreviewTextGroupConfig 文本分组配置：在通用分组字段之外携带文本预览大小配置
type PreviewTextGroupConfig struct {
	Enabled *bool   `yaml:"enabled,omitempty"`
	Mimes   *string `yaml:"mimes,omitempty"`
	Exts    *string `yaml:"exts,omitempty"`
	// MaxInlineSize 文本直接返回内容的大小上限 (支持 1M, 2M 等格式)
	MaxInlineSize string `yaml:"max_inline_size,omitempty"`
	// TextChunkSize 文本文件分块大小 (支持 50K, 100K 等格式)
	TextChunkSize string `yaml:"text_chunk_size,omitempty"`
}

// PreviewGroupsConfig 预览类型分组配置（图片/文本/音频/视频/文档/自定义）
type PreviewGroupsConfig struct {
	Image    PreviewGroupConfig     `yaml:"image,omitempty"`
	Text     PreviewTextGroupConfig `yaml:"text,omitempty"`
	Audio    PreviewGroupConfig     `yaml:"audio,omitempty"`
	Video    PreviewGroupConfig     `yaml:"video,omitempty"`
	Document PreviewGroupConfig     `yaml:"document,omitempty"`
	Custom   PreviewGroupConfig     `yaml:"custom,omitempty"`
}

// PreviewConfig 预览配置
type PreviewConfig struct {
	// Enabled 预览全局开关（nil 视为开启）
	Enabled *bool `yaml:"enabled,omitempty"`
	// Groups 各类型分组配置
	Groups PreviewGroupsConfig `yaml:"groups,omitempty"`
}

// previewSwitchOn 开关缺省（nil）视为开启
func previewSwitchOn(p *bool) bool {
	return p == nil || *p
}

// ResolvePreviewList 列表缺省（nil）回退默认值；显式配置（含空串）按配置生效
func ResolvePreviewList(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// IsEnabled 预览全局开关是否开启（未配置视为开启）
func (c PreviewConfig) IsEnabled() bool {
	return previewSwitchOn(c.Enabled)
}

// IsGroupEnabled 指定预览分组是否开启。
// 内置分组未配置（nil）视为开启；custom 组为兜底白名单，未配置视为关闭、按需启用。
// 未知分组视为开启。
func (c PreviewConfig) IsGroupEnabled(name string) bool {
	switch name {
	case "image":
		return previewSwitchOn(c.Groups.Image.Enabled)
	case "text":
		return previewSwitchOn(c.Groups.Text.Enabled)
	case "audio":
		return previewSwitchOn(c.Groups.Audio.Enabled)
	case "video":
		return previewSwitchOn(c.Groups.Video.Enabled)
	case "document":
		return previewSwitchOn(c.Groups.Document.Enabled)
	case "custom":
		return c.Groups.Custom.Enabled != nil && *c.Groups.Custom.Enabled
	}
	return true
}

// GroupMimes 指定分组生效的 MIME 列表（未配置回退内置默认表）
func (c PreviewConfig) GroupMimes(name string) string {
	switch name {
	case "image":
		return ResolvePreviewList(c.Groups.Image.Mimes, utils.DefaultPreviewGroupMimes(name))
	case "text":
		return ResolvePreviewList(c.Groups.Text.Mimes, utils.DefaultPreviewGroupMimes(name))
	case "audio":
		return ResolvePreviewList(c.Groups.Audio.Mimes, utils.DefaultPreviewGroupMimes(name))
	case "video":
		return ResolvePreviewList(c.Groups.Video.Mimes, utils.DefaultPreviewGroupMimes(name))
	case "document":
		return ResolvePreviewList(c.Groups.Document.Mimes, utils.DefaultPreviewGroupMimes(name))
	case "custom":
		return ResolvePreviewList(c.Groups.Custom.Mimes, utils.DefaultPreviewGroupMimes(name))
	}
	return ""
}

// GroupExts 指定分组生效的扩展名列表（未配置回退内置默认表）
func (c PreviewConfig) GroupExts(name string) string {
	switch name {
	case "image":
		return ResolvePreviewList(c.Groups.Image.Exts, utils.DefaultPreviewGroupExts(name))
	case "text":
		return ResolvePreviewList(c.Groups.Text.Exts, utils.DefaultPreviewGroupExts(name))
	case "audio":
		return ResolvePreviewList(c.Groups.Audio.Exts, utils.DefaultPreviewGroupExts(name))
	case "video":
		return ResolvePreviewList(c.Groups.Video.Exts, utils.DefaultPreviewGroupExts(name))
	case "document":
		return ResolvePreviewList(c.Groups.Document.Exts, utils.DefaultPreviewGroupExts(name))
	case "custom":
		return ResolvePreviewList(c.Groups.Custom.Exts, utils.DefaultPreviewGroupExts(name))
	}
	return ""
}

// TextMaxInlineSize 文本组生效的最大内联大小（未配置默认 1M）
func (c PreviewConfig) TextMaxInlineSize() string {
	if c.Groups.Text.MaxInlineSize == "" {
		return "1M"
	}
	return c.Groups.Text.MaxInlineSize
}

// TextChunkSize 文本组生效的分块大小（未配置默认 100K）
func (c PreviewConfig) TextChunkSize() string {
	if c.Groups.Text.TextChunkSize == "" {
		return "100K"
	}
	return c.Groups.Text.TextChunkSize
}

// InitConfig 解析为 utils 预览判定所需的完整运行时配置
func (c PreviewConfig) InitConfig() utils.PreviewInitConfig {
	groups := make(map[string]utils.PreviewGroupState, 6)
	for _, name := range []string{"image", "text", "audio", "video", "document", "custom"} {
		groups[name] = utils.PreviewGroupState{
			Enabled: c.IsGroupEnabled(name),
			Mimes:   c.GroupMimes(name),
			Exts:    c.GroupExts(name),
		}
	}
	return utils.PreviewInitConfig{Enabled: c.IsEnabled(), Groups: groups}
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

// RootNameKeys 返回当前已配置的根目录名称（已排序），用于错误提示与诊断。
// 未配置任何根目录时返回空切片。
func RootNameKeys() []string {
	names := make([]string, 0, len(RootNames))
	for name := range RootNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

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

// GetDDoSConfig 返回高频/DDoS 检测配置（供检测器实时读取，支持配置热更新）。
func GetDDoSConfig() DDosConfig {
	configMu.RLock()
	dd := GlobalConfig.DDos
	configMu.RUnlock()
	return dd
}

// GetWebhookConfig 返回 Webhook 通知配置（供派发器实时读取，支持配置热更新）。
func GetWebhookConfig() WebhookConfig {
	configMu.RLock()
	wh := GlobalConfig.Webhook
	configMu.RUnlock()
	return wh
}

// GetUploadRateLimit 返回上传频率限制配置（供上传限流器每个请求实时读取，
// 支持配置热更新；MaxRequests <= 0 表示不限制）。
func GetUploadRateLimit() UploadRateLimitConfig {
	configMu.RLock()
	ul := GlobalConfig.Upload.RateLimit
	configMu.RUnlock()
	return ul
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
