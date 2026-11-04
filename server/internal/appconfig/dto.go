package appconfig

import (
	"fmt"
	"strings"
)

// BoolPtrFalseWhenDisabled 将上传开关 bool 转为首选的 *bool：
// 禁止上传（enabled=false）时返回指向 false 的指针；允许上传时返回 nil（默认允许，写配置更简洁）。
func BoolPtrFalseWhenDisabled(enabled bool) *bool {
	if !enabled {
		v := false
		return &v
	}
	return nil
}

// ConfigDTO 配置 API 响应/请求 DTO，与前端字段保持一致（JSON camelCase）
type ConfigDTO struct {
	App               AppConfigDTO       `json:"app"`
	Account           AccountResponse    `json:"account"`
	Server            ServerResponse     `json:"server"`
	Database          DatabaseConfigDTO  `json:"database"`
	RootDirs          []RootDirConfig    `json:"rootDirs"`
	AllowedExtensions []string           `json:"allowedExtensions"`
	StorageBackend    *StorageBackendDTO `json:"storageBackend"`
	PrivateFiles      PrivateConfigDTO   `json:"privateFiles"`
	TempFiles         TempFilesConfigDTO `json:"tempFiles"`
	Upload            UploadConfigDTO    `json:"upload"`
	Download          DownloadConfigDTO  `json:"download"`
	Preview           PreviewConfigDTO   `json:"preview"`
	MimeTable         map[string]string  `json:"mimeTable"`
	OpenApi           *OpenApiConfigDTO  `json:"openApi"`
	Index             IndexConfigDTO     `json:"index"`
	Resource          ResourceConfigDTO  `json:"resource"`
	Security          SecurityConfigDTO  `json:"security"`
	Auth              AuthConfigDTO      `json:"auth"`
	Webhook           *WebhookConfigDTO  `json:"webhook"`
}

// AuthConfigDTO 认证配置 DTO
type AuthConfigDTO struct {
	LDAP   LDAPConfigDTO   `json:"ldap"`
	OAuth2 OAuth2ConfigDTO `json:"oauth2"`
}

// OAuth2ConfigDTO 外部 OAuth2/OIDC 配置 DTO（对应 yaml auth.oauth2.*）
type OAuth2ConfigDTO struct {
	Enabled            bool     `json:"enabled"`
	ProviderName       string   `json:"providerName"`
	ClientID           string   `json:"clientId"`
	ClientSecret       string   `json:"clientSecret"`
	AuthURL            string   `json:"authUrl"`
	TokenURL           string   `json:"tokenUrl"`
	UserInfoURL        string   `json:"userInfoUrl"`
	Issuer             string   `json:"issuer"`
	Scopes             []string `json:"scopes"`
	RedirectURL        string   `json:"redirectUrl"`
	UsernameClaim      string   `json:"usernameClaim"`
	DisplayNameClaim   string   `json:"displayNameClaim"`
	EmailClaim         string   `json:"emailClaim"`
	AutoCreateUser     bool     `json:"autoCreateUser"`
	InsecureSkipVerify bool     `json:"insecureSkipVerify"`
}

// LDAPConfigDTO LDAP 配置 DTO（前端 camelCase 字段，对应 yaml auth.ldap.*）
type LDAPConfigDTO struct {
	Enabled            bool   `json:"enabled"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	UseSSL             bool   `json:"useSSL"`
	BaseDN             string `json:"baseDN"`
	BindDN             string `json:"bindDN"`
	BindPassword       string `json:"bindPassword"`
	UserFilter         string `json:"userFilter"`
	SyncInterval       int    `json:"syncInterval"`
	AutoCreateUser     bool   `json:"autoCreateUser"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
}

// SecurityConfigDTO 安全配置 DTO（trust_proxy / CORS）
type SecurityConfigDTO struct {
	TrustProxy     bool     `json:"trustProxy"`
	AllowedOrigins []string `json:"allowedOrigins"`
}

type OpenApiConfigDTO struct {
	Enabled           bool     `json:"enabled"`
	IpAccessMode      string   `json:"ipAccessMode"`
	IpWhitelist       []string `json:"ipWhitelist"`
	IpBlacklist       []string `json:"ipBlacklist"`
	DomainWhitelist   []string `json:"domainWhitelist"`
	RateLimitEnabled  bool     `json:"rateLimitEnabled"`
	RequestsPerMinute int      `json:"requestsPerMinute"`
	// AuthMode 鉴权方式：none（匿名）/ api_key（系统签发的 API Key + scope）
	AuthMode string `json:"authMode"`
}

// WebhookEndpointDTO 单条 Webhook 回调地址 DTO（对应 yaml webhook.endpoints[]）
type WebhookEndpointDTO struct {
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	Enabled        *bool             `json:"enabled"`
	Secret         string            `json:"secret"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	MaxRetries     int               `json:"maxRetries"`
	Events         []string          `json:"events"`
	Headers        map[string]string `json:"headers"`
	Query          map[string]string `json:"query"`
}

// WebhookConfigDTO Webhook 推送配置 DTO（对应 yaml webhook.*）
type WebhookConfigDTO struct {
	Enabled        bool                 `json:"enabled"`
	URLs           []string             `json:"urls,omitempty"` // 旧版，仅兼容读取
	Endpoints      []WebhookEndpointDTO `json:"endpoints"`
	Secret         string               `json:"secret"`
	TimeoutSeconds int                  `json:"timeoutSeconds"`
	MaxRetries     int                  `json:"maxRetries"`
	QueueSize      int                  `json:"queueSize"`
}

// EndpointToDTO 将内部端点结构转为 DTO。
func EndpointToDTO(e WebhookEndpoint) WebhookEndpointDTO {
	return WebhookEndpointDTO{
		Name:           e.Name,
		URL:            e.URL,
		Enabled:        e.Enabled,
		Secret:         e.Secret,
		TimeoutSeconds: e.TimeoutSeconds,
		MaxRetries:     e.MaxRetries,
		Events:         e.Events,
		Headers:        e.Headers,
		Query:          e.Query,
	}
}

// EndpointFromDTO 将 DTO 转为内部端点结构（默认 nil 视为启用，由保存方决定）。
func EndpointFromDTO(d WebhookEndpointDTO) WebhookEndpoint {
	return WebhookEndpoint{
		Name:           d.Name,
		URL:            d.URL,
		Enabled:        d.Enabled,
		Secret:         d.Secret,
		TimeoutSeconds: d.TimeoutSeconds,
		MaxRetries:     d.MaxRetries,
		Events:         d.Events,
		Headers:        d.Headers,
		Query:          d.Query,
	}
}

// AppConfigDTO 应用配置 DTO
type AppConfigDTO struct {
	Name        string `json:"name"`
	Initialized bool   `json:"initialized"`
}

// DatabaseConfigDTO 数据库配置 DTO
type DatabaseConfigDTO struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}

// RootDirConfig 根目录 DTO
type RootDirConfig struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	FullPath      string `json:"fullPath"`
	UploadEnabled bool   `json:"uploadEnabled"`
}

// StorageBackendDTO 存储后端配置 DTO（对应 yaml storage.backend/s3/oss/webdav）。
// 存储后端在服务启动时初始化，修改后需重启服务生效。
type StorageBackendDTO struct {
	Backend string                 `json:"backend"`
	S3      S3StorageConfigDTO     `json:"s3"`
	OSS     OSSStorageConfigDTO    `json:"oss"`
	WebDAV  WebDAVStorageConfigDTO `json:"webdav"`
	Alert   StorageAlertConfigDTO  `json:"alert"`
}

// StorageAlertConfigDTO 存储使用率告警配置 DTO。
type StorageAlertConfigDTO struct {
	Enabled              bool          `json:"enabled"`
	ThresholdPercent     float64       `json:"thresholdPercent"`
	CheckIntervalMinutes int           `json:"checkIntervalMinutes"`
	CooldownMinutes      int           `json:"cooldownMinutes"`
	WebEnabled           bool          `json:"webEnabled"`
	WebhookEnabled       bool          `json:"webhookEnabled"`
	EmailEnabled         bool          `json:"emailEnabled"`
	EmailRecipients      []string      `json:"emailRecipients"`
	SMTP                 SMTPConfigDTO `json:"smtp"`
}

// SMTPConfigDTO 告警邮件 SMTP 配置 DTO。
type SMTPConfigDTO struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	TLSMode  string `json:"tlsMode"`
}

// S3StorageConfigDTO S3/MinIO 连接配置 DTO
type S3StorageConfigDTO struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	UseSSL    bool   `json:"useSSL"`
}

// OSSStorageConfigDTO 阿里云 OSS 连接配置 DTO
type OSSStorageConfigDTO struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Bucket    string `json:"bucket"`
}

// WebDAVStorageConfigDTO WebDAV 连接配置 DTO
type WebDAVStorageConfigDTO struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// PrivateConfigDTO 私有存储 DTO（API 中字段名为 "temp"，兼容前端）
type PrivateConfigDTO struct {
	Enabled      bool   `json:"enabled"`
	Path         string `json:"path"`
	QuotaGlobal  int64  `json:"quotaGlobal"`
	QuotaPerUser int64  `json:"quotaPerUser"`
}

// TempFilesConfigDTO 临时文件配置 DTO
type TempFilesConfigDTO struct {
	Enabled           bool   `json:"enabled"`
	Path              string `json:"path"`
	QuotaGlobal       int64  `json:"quotaGlobal"`
	QuotaPerIP        int64  `json:"quotaPerIP"`
	DefaultExpireDays int    `json:"defaultExpireDays"`
	DeleteOnDownload  bool   `json:"deleteOnDownload"`
}

// UploadConfigDTO 上传配置 DTO
type UploadConfigDTO struct {
	ChunkSize   int64              `json:"chunkSize"`
	MaxFileSize int64              `json:"maxFileSize"`
	URLUpload   URLUploadDTO       `json:"urlUpload"`
	RateLimit   UploadRateLimitDTO `json:"rateLimit"`
}

// UploadRateLimitDTO 上传频率限制 DTO（按客户端 IP 统计，作用于上传控制面接口）
type UploadRateLimitDTO struct {
	WindowMinutes int `json:"windowMinutes"`
	MaxRequests   int `json:"maxRequests"` // 0 = 不限制
}

// URLUploadDTO URL 上传配置 DTO
type URLUploadDTO struct {
	Enabled            bool     `json:"enabled"`
	AllowedIPRanges    []string `json:"allowedIPRanges"`
	InsecureSkipVerify bool     `json:"insecureSkipVerify"`
	MaxConcurrent      int      `json:"maxConcurrent"`
	MaxRetries         int      `json:"maxRetries"`
	MaxSize            int64    `json:"maxSize"` // 服务器侧大小硬上限（字节），0=不限
}

// DownloadConfigDTO 下载配置 DTO
type DownloadConfigDTO struct {
	RateLimit DownloadRateLimitDTO `json:"rateLimit"`
}

// DownloadRateLimitDTO 下载频率限制 DTO
type DownloadRateLimitDTO struct {
	WindowMinutes int `json:"windowMinutes"`
	MaxRequests   int `json:"maxRequests"`
	LockAfter     int `json:"lockAfter"`
	LockMinutes   int `json:"lockMinutes"`
}

// PreviewGroupDTO 单个预览分组 DTO。
// 指针以区分「未提交」与「显式留空」：未提交沿用当前生效值，显式留空按空列表生效。
type PreviewGroupDTO struct {
	Enabled *bool   `json:"enabled"`
	Mimes   *string `json:"mimes"`
	Exts    *string `json:"exts"`
}

// PreviewTextGroupDTO 文本分组 DTO（额外携带文本预览大小配置）
type PreviewTextGroupDTO struct {
	Enabled       *bool   `json:"enabled"`
	Mimes         *string `json:"mimes"`
	Exts          *string `json:"exts"`
	MaxInlineSize string  `json:"maxInlineSize"`
	TextChunkSize string  `json:"textChunkSize"`
}

// PreviewGroupsDTO 预览类型分组配置 DTO
type PreviewGroupsDTO struct {
	Image    PreviewGroupDTO     `json:"image"`
	Text     PreviewTextGroupDTO `json:"text"`
	Audio    PreviewGroupDTO     `json:"audio"`
	Video    PreviewGroupDTO     `json:"video"`
	Document PreviewGroupDTO     `json:"document"`
	Custom   PreviewGroupDTO     `json:"custom"`
}

// PreviewConfigDTO 预览配置 DTO
type PreviewConfigDTO struct {
	Enabled *bool            `json:"enabled"`
	Groups  PreviewGroupsDTO `json:"groups"`
}

// boolPtr 返回布尔值的指针，用于 DTO 中区分零值与未设置
func boolPtr(v bool) *bool { return &v }

// strPtr 返回字符串值的指针
func strPtr(v string) *string { return &v }

// ResolvePreviewSwitch 解析开关指针：未设置（nil）视为开启
func ResolvePreviewSwitch(p *bool) bool {
	return p == nil || *p
}

// AccountResponse 账户配置 DTO
type AccountResponse struct {
	Anonymous         AnonymousResponse `json:"anonymous"`
	ReservedUsernames []string          `json:"reservedUsernames"`
	AllowRegistration bool              `json:"allowRegistration"` // 是否允许用户自行注册
	RequireLogin      bool              `json:"requireLogin"`      // 是否强制登录：开启后 Web 界面仅登录用户可交互
	Captcha           CaptchaResponse   `json:"captcha"`           // 登录/注册图形验证码
}

// CaptchaResponse 验证码配置响应
type CaptchaResponse struct {
	Mode         string `json:"mode"`
	TriggerAfter int    `json:"triggerAfter"`
}

// AnonymousResponse 匿名用户 DTO
type AnonymousResponse struct {
	Username string `json:"username"`
}

// ServerResponse 服务器配置 DTO
type ServerResponse struct {
	Host   string           `json:"host"`
	HTTP   HTTPConfigResp   `json:"http"`
	HTTPS  HTTPConfigResp   `json:"https"`
	FTP    FTPConfigResp    `json:"ftp"`
	FTPS   FTPSConfigResp   `json:"ftps"`
	WebDAV WebDAVConfigResp `json:"webdav"`
}

// HTTPConfigResp HTTP 配置响应
type HTTPConfigResp struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

// FTPConfigResp FTP 配置响应
type FTPConfigResp struct {
	Enabled          bool `json:"enabled"`
	Port             int  `json:"port"`
	PassivePortStart int  `json:"passivePortStart"`
	PassivePortEnd   int  `json:"passivePortEnd"`
}

// FTPSConfigResp FTPS 配置响应
type FTPSConfigResp struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

// WebDAVConfigResp WebDAV 配置响应
type WebDAVConfigResp struct {
	Enabled bool `json:"enabled"`
}

// IndexConfigDTO 文件索引配置 DTO
type IndexConfigDTO struct {
	ScanStartDelaySeconds         int    `json:"scanStartDelaySeconds"`
	ScanCronExpression            string `json:"scanCronExpression"`
	SearchReconcileCronExpression string `json:"searchReconcileCronExpression"`
}

// ResourceConfigDTO 服务器资源监控配置 DTO
type ResourceConfigDTO struct {
	Enabled          bool `json:"enabled"`
	SamplingInterval int  `json:"samplingInterval"`
	CollectInterval  int  `json:"collectInterval"`
	RetentionDays    int  `json:"retentionDays"`
}

// ParseOrDefault 解析配额字符串，失败时返回默认值
func ParseOrDefault(s string, defaultVal int64) int64 {
	if s == "" {
		return defaultVal
	}
	val, err := ParseQuotaString(s)
	if err != nil {
		return defaultVal
	}
	return val
}

// ToDTO 将内部 Config 转换为 API DTO
func (c *Config) ToDTO() ConfigDTO {
	// 构建根目录列表
	rootDirs := make([]RootDirConfig, len(c.Storage.Public.RootDirs))
	for i, dir := range c.Storage.Public.RootDirs {
		// 从路径提取最后一段作为显示名称
		name := dir.Path
		if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
			name = name[idx+1:]
		}
		rootDirs[i] = RootDirConfig{
			Name:          name,
			Path:          name,
			FullPath:      dir.Path,
			UploadEnabled: dir.UploadAllowed(),
		}
	}

	return ConfigDTO{
		App: AppConfigDTO{
			Name:        c.App.Name,
			Initialized: c.App.Initialized,
		},
		Account: AccountResponse{
			Anonymous: AnonymousResponse{
				Username: c.Account.Anonymous.Username,
			},
			ReservedUsernames: c.Account.ReservedUsernames,
			AllowRegistration: c.Account.AllowRegistration,
			RequireLogin:      c.Account.RequireLogin,
			Captcha: CaptchaResponse{
				Mode:         c.Account.Captcha.Mode,
				TriggerAfter: c.Account.Captcha.TriggerAfter,
			},
		},
		Server: ServerResponse{
			Host: c.Server.Host,
			HTTP: HTTPConfigResp{
				Enabled: c.Server.HTTP.Enabled,
				Port:    c.Server.HTTP.Port,
			},
			HTTPS: HTTPConfigResp{
				Enabled: c.Server.HTTPS.Enabled,
				Port:    c.Server.HTTPS.Port,
			},
			FTP: FTPConfigResp{
				Enabled:          c.Server.FTP.Enabled,
				Port:             c.Server.FTP.Port,
				PassivePortStart: c.Server.FTP.PassivePortStart,
				PassivePortEnd:   c.Server.FTP.PassivePortEnd,
			},
			FTPS: FTPSConfigResp{
				Enabled: c.Server.FTPS.Enabled,
				Port:    c.Server.FTPS.Port,
			},
			WebDAV: WebDAVConfigResp{
				Enabled: c.Server.WebDAV.Enabled,
			},
		},
		Database: DatabaseConfigDTO{
			Driver: c.Database.Driver,
			DSN:    c.Database.DSN,
		},
		RootDirs:          rootDirs,
		AllowedExtensions: c.Storage.AllowedExtensions,
		StorageBackend: &StorageBackendDTO{
			Backend: c.Storage.Backend,
			S3: S3StorageConfigDTO{
				Endpoint:  c.Storage.S3.Endpoint,
				AccessKey: c.Storage.S3.AccessKey,
				SecretKey: c.Storage.S3.SecretKey,
				Bucket:    c.Storage.S3.Bucket,
				Region:    c.Storage.S3.Region,
				UseSSL:    c.Storage.S3.UseSSL,
			},
			OSS: OSSStorageConfigDTO{
				Endpoint:  c.Storage.OSS.Endpoint,
				AccessKey: c.Storage.OSS.AccessKey,
				SecretKey: c.Storage.OSS.SecretKey,
				Bucket:    c.Storage.OSS.Bucket,
			},
			WebDAV: WebDAVStorageConfigDTO{
				URL:      c.Storage.WebDAV.URL,
				Username: c.Storage.WebDAV.Username,
				Password: c.Storage.WebDAV.Password,
			},
			Alert: StorageAlertConfigDTO{
				Enabled:              c.Storage.Alert.Enabled,
				ThresholdPercent:     c.Storage.Alert.ThresholdPercent,
				CheckIntervalMinutes: c.Storage.Alert.CheckIntervalMinutes,
				CooldownMinutes:      c.Storage.Alert.CooldownMinutes,
				WebEnabled:           c.Storage.Alert.WebEnabled,
				WebhookEnabled:       c.Storage.Alert.WebhookEnabled,
				EmailEnabled:         c.Storage.Alert.EmailEnabled,
				EmailRecipients:      c.Storage.Alert.EmailRecipients,
				SMTP: SMTPConfigDTO{
					Host:     c.Storage.Alert.SMTP.Host,
					Port:     c.Storage.Alert.SMTP.Port,
					Username: c.Storage.Alert.SMTP.Username,
					Password: c.Storage.Alert.SMTP.Password,
					From:     c.Storage.Alert.SMTP.From,
					TLSMode:  c.Storage.Alert.SMTP.TLSMode,
				},
			},
		},
		TempFiles: TempFilesConfigDTO{
			Enabled:           c.Storage.Temp.Enabled,
			Path:              c.Storage.Temp.Path,
			QuotaGlobal:       ParseOrDefault(c.Storage.Temp.Quota.Global, 0),
			QuotaPerIP:        ParseOrDefault(c.Storage.Temp.Quota.PerIP, 0),
			DefaultExpireDays: c.Storage.Temp.DefaultExpireDays,
			DeleteOnDownload:  c.Storage.Temp.DeleteOnDownload,
		},
		PrivateFiles: PrivateConfigDTO{
			Enabled:      c.Storage.Private.Enabled,
			Path:         c.Storage.Private.Path,
			QuotaGlobal:  c.Storage.Private.Quota.GlobalQuota,
			QuotaPerUser: c.Storage.Private.Quota.PerUserQuota,
		},
		Upload: UploadConfigDTO{
			ChunkSize:   c.Upload.ChunkSize,
			MaxFileSize: c.Upload.MaxFileSize,
			URLUpload: URLUploadDTO{
				Enabled:            c.Upload.URLUpload.Enabled,
				AllowedIPRanges:    c.Upload.URLUpload.AllowedIPRanges,
				InsecureSkipVerify: c.Upload.URLUpload.InsecureSkipVerify,
				MaxConcurrent:      c.Upload.URLUpload.MaxConcurrent,
				MaxRetries:         c.Upload.URLUpload.MaxRetries,
				MaxSize:            c.Upload.URLUpload.MaxSize,
			},
			RateLimit: UploadRateLimitDTO{
				WindowMinutes: c.Upload.RateLimit.WindowMinutes,
				MaxRequests:   c.Upload.RateLimit.MaxRequests,
			},
		},
		Download: DownloadConfigDTO{
			RateLimit: DownloadRateLimitDTO{
				WindowMinutes: c.Download.RateLimit.WindowMinutes,
				MaxRequests:   c.Download.RateLimit.MaxRequests,
				LockAfter:     c.Download.RateLimit.LockAfter,
				LockMinutes:   c.Download.RateLimit.LockMinutes,
			},
		},
		Preview: PreviewConfigDTO{
			Enabled: boolPtr(c.Preview.IsEnabled()),
			Groups: PreviewGroupsDTO{
				Image: PreviewGroupDTO{
					Enabled: boolPtr(c.Preview.IsGroupEnabled("image")),
					Mimes:   strPtr(c.Preview.GroupMimes("image")),
					Exts:    strPtr(c.Preview.GroupExts("image")),
				},
				Text: PreviewTextGroupDTO{
					Enabled:       boolPtr(c.Preview.IsGroupEnabled("text")),
					Mimes:         strPtr(c.Preview.GroupMimes("text")),
					Exts:          strPtr(c.Preview.GroupExts("text")),
					MaxInlineSize: c.Preview.TextMaxInlineSize(),
					TextChunkSize: c.Preview.TextChunkSize(),
				},
				Audio: PreviewGroupDTO{
					Enabled: boolPtr(c.Preview.IsGroupEnabled("audio")),
					Mimes:   strPtr(c.Preview.GroupMimes("audio")),
					Exts:    strPtr(c.Preview.GroupExts("audio")),
				},
				Video: PreviewGroupDTO{
					Enabled: boolPtr(c.Preview.IsGroupEnabled("video")),
					Mimes:   strPtr(c.Preview.GroupMimes("video")),
					Exts:    strPtr(c.Preview.GroupExts("video")),
				},
				Document: PreviewGroupDTO{
					Enabled: boolPtr(c.Preview.IsGroupEnabled("document")),
					Mimes:   strPtr(c.Preview.GroupMimes("document")),
					Exts:    strPtr(c.Preview.GroupExts("document")),
				},
				Custom: PreviewGroupDTO{
					Enabled: boolPtr(c.Preview.IsGroupEnabled("custom")),
					Mimes:   strPtr(c.Preview.GroupMimes("custom")),
					Exts:    strPtr(c.Preview.GroupExts("custom")),
				},
			},
		},
		MimeTable: c.MimeTable,
		OpenApi: &OpenApiConfigDTO{
			Enabled:           c.OpenAPI.Enabled,
			IpAccessMode:      c.OpenAPI.IPAccessMode,
			IpWhitelist:       c.OpenAPI.IPWhitelist,
			IpBlacklist:       c.OpenAPI.IPBlacklist,
			DomainWhitelist:   c.OpenAPI.DomainWhitelist,
			RateLimitEnabled:  c.OpenAPI.RateLimitEnabled,
			RequestsPerMinute: c.OpenAPI.RequestsPerMinute,
			AuthMode:          c.OpenAPI.AuthMode,
		},
		Index: IndexConfigDTO{
			ScanStartDelaySeconds:         c.Index.ScanStartDelaySeconds,
			ScanCronExpression:            c.Index.ScanCronExpression,
			SearchReconcileCronExpression: c.Index.SearchReconcileCronExpression,
		},
		Resource: ResourceConfigDTO{
			Enabled:          c.Resource.Enabled,
			SamplingInterval: c.Resource.SamplingInterval,
			CollectInterval:  c.Resource.CollectInterval,
			RetentionDays:    c.Resource.RetentionDays,
		},
		Security: SecurityConfigDTO{
			TrustProxy:     c.Security.TrustProxy,
			AllowedOrigins: c.Security.AllowedOrigins,
		},
		Webhook: &WebhookConfigDTO{
			Enabled:        c.Webhook.Enabled,
			Secret:         c.Webhook.Secret,
			TimeoutSeconds: c.Webhook.TimeoutSeconds,
			MaxRetries:     c.Webhook.MaxRetries,
			QueueSize:      c.Webhook.QueueSize,
			Endpoints:      endpointsToDTO(c.Webhook.EffectiveEndpoints()),
		},
		Auth: AuthConfigDTO{
			LDAP: LDAPConfigDTO{
				Enabled:            c.Auth.LDAP.Enabled,
				Host:               c.Auth.LDAP.Host,
				Port:               c.Auth.LDAP.Port,
				UseSSL:             c.Auth.LDAP.UseSSL,
				BaseDN:             c.Auth.LDAP.BaseDN,
				BindDN:             c.Auth.LDAP.BindDN,
				BindPassword:       c.Auth.LDAP.BindPassword,
				UserFilter:         c.Auth.LDAP.UserFilter,
				SyncInterval:       c.Auth.LDAP.SyncInterval,
				AutoCreateUser:     c.Auth.LDAP.AutoCreateUser,
				InsecureSkipVerify: c.Auth.LDAP.InsecureSkipVerify,
			},
			OAuth2: OAuth2ConfigDTO{
				Enabled:            c.Auth.OAuth2.Enabled,
				ProviderName:       c.Auth.OAuth2.ProviderName,
				ClientID:           c.Auth.OAuth2.ClientID,
				ClientSecret:       c.Auth.OAuth2.ClientSecret,
				AuthURL:            c.Auth.OAuth2.AuthURL,
				TokenURL:           c.Auth.OAuth2.TokenURL,
				UserInfoURL:        c.Auth.OAuth2.UserInfoURL,
				Issuer:             c.Auth.OAuth2.Issuer,
				Scopes:             c.Auth.OAuth2.Scopes,
				RedirectURL:        c.Auth.OAuth2.RedirectURL,
				UsernameClaim:      c.Auth.OAuth2.UsernameClaim,
				DisplayNameClaim:   c.Auth.OAuth2.DisplayNameClaim,
				EmailClaim:         c.Auth.OAuth2.EmailClaim,
				AutoCreateUser:     c.Auth.OAuth2.AutoCreateUser,
				InsecureSkipVerify: c.Auth.OAuth2.InsecureSkipVerify,
			},
		},
	}
}

// endpointsToDTO 将内部端点列表转为 DTO 列表。
func endpointsToDTO(eps []WebhookEndpoint) []WebhookEndpointDTO {
	if len(eps) == 0 {
		return []WebhookEndpointDTO{}
	}
	out := make([]WebhookEndpointDTO, len(eps))
	for i, e := range eps {
		out[i] = EndpointToDTO(e)
	}
	return out
}

// ConfigFromDTO 从 API DTO 构建内部 Config
func ConfigFromDTO(dto ConfigDTO) Config {
	cfg := Config{}

	cfg.App.Name = dto.App.Name
	cfg.App.Initialized = dto.App.Initialized

	cfg.Server.Host = dto.Server.Host
	cfg.Server.HTTP.Enabled = dto.Server.HTTP.Enabled
	cfg.Server.HTTP.Port = dto.Server.HTTP.Port
	cfg.Server.HTTPS.Enabled = dto.Server.HTTPS.Enabled
	cfg.Server.HTTPS.Port = dto.Server.HTTPS.Port
	cfg.Server.FTP.Enabled = dto.Server.FTP.Enabled
	cfg.Server.FTP.Port = dto.Server.FTP.Port
	cfg.Server.FTP.PassivePortStart = dto.Server.FTP.PassivePortStart
	cfg.Server.FTP.PassivePortEnd = dto.Server.FTP.PassivePortEnd
	cfg.Server.FTPS.Enabled = dto.Server.FTPS.Enabled
	cfg.Server.FTPS.Port = dto.Server.FTPS.Port
	cfg.Server.WebDAV.Enabled = dto.Server.WebDAV.Enabled

	cfg.Account.Anonymous.Username = dto.Account.Anonymous.Username
	cfg.Account.ReservedUsernames = dto.Account.ReservedUsernames
	cfg.Account.AllowRegistration = dto.Account.AllowRegistration
	cfg.Account.RequireLogin = dto.Account.RequireLogin
	cfg.Account.Captcha.Mode = dto.Account.Captcha.Mode
	cfg.Account.Captcha.TriggerAfter = dto.Account.Captcha.TriggerAfter

	cfg.Database.Driver = dto.Database.Driver
	cfg.Database.DSN = dto.Database.DSN

	// 根目录
	if len(dto.RootDirs) > 0 {
		cfg.Storage.Public.RootDirs = make([]DirectoryConfig, len(dto.RootDirs))
		for i, dir := range dto.RootDirs {
			path := dir.FullPath
			if path == "" {
				path = dir.Path
			}
			cfg.Storage.Public.RootDirs[i] = DirectoryConfig{
				Path: path,
				// 仅当明确禁止上传时写入 false；允许时保持 nil，回写 YAML 更简洁且与默认一致
				AllowUpload: BoolPtrFalseWhenDisabled(dir.UploadEnabled),
			}
		}
	}

	cfg.Storage.AllowedExtensions = dto.AllowedExtensions

	// 存储后端连接配置
	if dto.StorageBackend != nil {
		cfg.Storage.Backend = dto.StorageBackend.Backend
		cfg.Storage.S3 = S3StorageConfig{
			Endpoint:  dto.StorageBackend.S3.Endpoint,
			AccessKey: dto.StorageBackend.S3.AccessKey,
			SecretKey: dto.StorageBackend.S3.SecretKey,
			Bucket:    dto.StorageBackend.S3.Bucket,
			Region:    dto.StorageBackend.S3.Region,
			UseSSL:    dto.StorageBackend.S3.UseSSL,
		}
		cfg.Storage.OSS = OSSStorageConfig{
			Endpoint:  dto.StorageBackend.OSS.Endpoint,
			AccessKey: dto.StorageBackend.OSS.AccessKey,
			SecretKey: dto.StorageBackend.OSS.SecretKey,
			Bucket:    dto.StorageBackend.OSS.Bucket,
		}
		cfg.Storage.WebDAV = WebDAVStorageConfig{
			URL:      dto.StorageBackend.WebDAV.URL,
			Username: dto.StorageBackend.WebDAV.Username,
			Password: dto.StorageBackend.WebDAV.Password,
		}
		cfg.Storage.Alert = NormalizeStorageAlertConfig(StorageAlertConfig{
			Enabled:              dto.StorageBackend.Alert.Enabled,
			ThresholdPercent:     dto.StorageBackend.Alert.ThresholdPercent,
			CheckIntervalMinutes: dto.StorageBackend.Alert.CheckIntervalMinutes,
			CooldownMinutes:      dto.StorageBackend.Alert.CooldownMinutes,
			WebEnabled:           dto.StorageBackend.Alert.WebEnabled,
			WebhookEnabled:       dto.StorageBackend.Alert.WebhookEnabled,
			EmailEnabled:         dto.StorageBackend.Alert.EmailEnabled,
			EmailRecipients:      dto.StorageBackend.Alert.EmailRecipients,
			SMTP: SMTPConfig{
				Host:     dto.StorageBackend.Alert.SMTP.Host,
				Port:     dto.StorageBackend.Alert.SMTP.Port,
				Username: dto.StorageBackend.Alert.SMTP.Username,
				Password: dto.StorageBackend.Alert.SMTP.Password,
				From:     dto.StorageBackend.Alert.SMTP.From,
				TLSMode:  dto.StorageBackend.Alert.SMTP.TLSMode,
			},
		})
	}

	// 私有存储（前端字段名 privateFiles）
	cfg.Storage.Private.Enabled = dto.PrivateFiles.Enabled
	cfg.Storage.Private.Path = dto.PrivateFiles.Path
	cfg.Storage.Private.Quota.GlobalQuota = dto.PrivateFiles.QuotaGlobal
	cfg.Storage.Private.Quota.PerUserQuota = dto.PrivateFiles.QuotaPerUser

	// 临时文件
	cfg.Storage.Temp.Enabled = dto.TempFiles.Enabled
	cfg.Storage.Temp.Path = dto.TempFiles.Path
	cfg.Storage.Temp.Quota.Global = FormatSizeString(dto.TempFiles.QuotaGlobal)
	cfg.Storage.Temp.Quota.PerIP = FormatSizeString(dto.TempFiles.QuotaPerIP)
	cfg.Storage.Temp.DefaultExpireDays = dto.TempFiles.DefaultExpireDays
	cfg.Storage.Temp.DeleteOnDownload = dto.TempFiles.DeleteOnDownload

	// 上传
	cfg.Upload.ChunkSize = dto.Upload.ChunkSize
	cfg.Upload.MaxFileSize = dto.Upload.MaxFileSize
	cfg.Upload.URLUpload.Enabled = dto.Upload.URLUpload.Enabled
	cfg.Upload.URLUpload.AllowedIPRanges = dto.Upload.URLUpload.AllowedIPRanges
	cfg.Upload.URLUpload.InsecureSkipVerify = dto.Upload.URLUpload.InsecureSkipVerify
	cfg.Upload.URLUpload.MaxConcurrent = dto.Upload.URLUpload.MaxConcurrent
	cfg.Upload.URLUpload.MaxRetries = dto.Upload.URLUpload.MaxRetries
	cfg.Upload.URLUpload.MaxSize = dto.Upload.URLUpload.MaxSize
	cfg.Upload.RateLimit.WindowMinutes = dto.Upload.RateLimit.WindowMinutes
	cfg.Upload.RateLimit.MaxRequests = dto.Upload.RateLimit.MaxRequests

	// 下载限流
	cfg.Download.RateLimit.WindowMinutes = dto.Download.RateLimit.WindowMinutes
	cfg.Download.RateLimit.MaxRequests = dto.Download.RateLimit.MaxRequests
	cfg.Download.RateLimit.LockAfter = dto.Download.RateLimit.LockAfter
	cfg.Download.RateLimit.LockMinutes = dto.Download.RateLimit.LockMinutes

	// 预览（指针字段直接透传，nil 表示沿用缺省语义）
	cfg.Preview.Enabled = dto.Preview.Enabled
	cfg.Preview.Groups.Image = PreviewGroupConfig{Enabled: dto.Preview.Groups.Image.Enabled, Mimes: dto.Preview.Groups.Image.Mimes, Exts: dto.Preview.Groups.Image.Exts}
	cfg.Preview.Groups.Text = PreviewTextGroupConfig{
		Enabled: dto.Preview.Groups.Text.Enabled, Mimes: dto.Preview.Groups.Text.Mimes, Exts: dto.Preview.Groups.Text.Exts,
		MaxInlineSize: dto.Preview.Groups.Text.MaxInlineSize, TextChunkSize: dto.Preview.Groups.Text.TextChunkSize,
	}
	cfg.Preview.Groups.Audio = PreviewGroupConfig{Enabled: dto.Preview.Groups.Audio.Enabled, Mimes: dto.Preview.Groups.Audio.Mimes, Exts: dto.Preview.Groups.Audio.Exts}
	cfg.Preview.Groups.Video = PreviewGroupConfig{Enabled: dto.Preview.Groups.Video.Enabled, Mimes: dto.Preview.Groups.Video.Mimes, Exts: dto.Preview.Groups.Video.Exts}
	cfg.Preview.Groups.Document = PreviewGroupConfig{Enabled: dto.Preview.Groups.Document.Enabled, Mimes: dto.Preview.Groups.Document.Mimes, Exts: dto.Preview.Groups.Document.Exts}
	cfg.Preview.Groups.Custom = PreviewGroupConfig{Enabled: dto.Preview.Groups.Custom.Enabled, Mimes: dto.Preview.Groups.Custom.Mimes, Exts: dto.Preview.Groups.Custom.Exts}

	cfg.MimeTable = dto.MimeTable

	if dto.OpenApi != nil {
		cfg.OpenAPI.Enabled = dto.OpenApi.Enabled
		cfg.OpenAPI.IPAccessMode = dto.OpenApi.IpAccessMode
		cfg.OpenAPI.IPWhitelist = dto.OpenApi.IpWhitelist
		cfg.OpenAPI.IPBlacklist = dto.OpenApi.IpBlacklist
		cfg.OpenAPI.DomainWhitelist = dto.OpenApi.DomainWhitelist
		cfg.OpenAPI.RateLimitEnabled = dto.OpenApi.RateLimitEnabled
		cfg.OpenAPI.RequestsPerMinute = dto.OpenApi.RequestsPerMinute
		cfg.OpenAPI.AuthMode = strings.TrimSpace(dto.OpenApi.AuthMode)
	}

	if dto.Webhook != nil {
		cfg.Webhook.Enabled = dto.Webhook.Enabled
		cfg.Webhook.Secret = dto.Webhook.Secret
		cfg.Webhook.TimeoutSeconds = dto.Webhook.TimeoutSeconds
		cfg.Webhook.MaxRetries = dto.Webhook.MaxRetries
		cfg.Webhook.QueueSize = dto.Webhook.QueueSize
		if dto.Webhook.Endpoints != nil {
			cfg.Webhook.Endpoints = make([]WebhookEndpoint, len(dto.Webhook.Endpoints))
			for i, e := range dto.Webhook.Endpoints {
				cfg.Webhook.Endpoints[i] = EndpointFromDTO(e)
			}
		}
	}

	// 索引配置
	cfg.Index.ScanStartDelaySeconds = dto.Index.ScanStartDelaySeconds
	cfg.Index.ScanCronExpression = dto.Index.ScanCronExpression
	cfg.Index.SearchReconcileCronExpression = dto.Index.SearchReconcileCronExpression

	// 资源监控配置
	cfg.Resource.Enabled = dto.Resource.Enabled
	cfg.Resource.SamplingInterval = dto.Resource.SamplingInterval
	cfg.Resource.CollectInterval = dto.Resource.CollectInterval
	cfg.Resource.RetentionDays = dto.Resource.RetentionDays

	// 安全配置
	cfg.Security.TrustProxy = dto.Security.TrustProxy
	cfg.Security.AllowedOrigins = dto.Security.AllowedOrigins

	// LDAP 认证配置
	cfg.Auth.LDAP.Enabled = dto.Auth.LDAP.Enabled
	cfg.Auth.LDAP.Host = dto.Auth.LDAP.Host
	cfg.Auth.LDAP.Port = dto.Auth.LDAP.Port
	cfg.Auth.LDAP.UseSSL = dto.Auth.LDAP.UseSSL
	cfg.Auth.LDAP.BaseDN = dto.Auth.LDAP.BaseDN
	cfg.Auth.LDAP.BindDN = dto.Auth.LDAP.BindDN
	cfg.Auth.LDAP.BindPassword = dto.Auth.LDAP.BindPassword
	cfg.Auth.LDAP.UserFilter = dto.Auth.LDAP.UserFilter
	cfg.Auth.LDAP.SyncInterval = dto.Auth.LDAP.SyncInterval
	cfg.Auth.LDAP.AutoCreateUser = dto.Auth.LDAP.AutoCreateUser
	cfg.Auth.LDAP.InsecureSkipVerify = dto.Auth.LDAP.InsecureSkipVerify

	// OAuth2/OIDC 认证配置
	cfg.Auth.OAuth2.Enabled = dto.Auth.OAuth2.Enabled
	cfg.Auth.OAuth2.ProviderName = dto.Auth.OAuth2.ProviderName
	cfg.Auth.OAuth2.ClientID = dto.Auth.OAuth2.ClientID
	cfg.Auth.OAuth2.ClientSecret = dto.Auth.OAuth2.ClientSecret
	cfg.Auth.OAuth2.AuthURL = dto.Auth.OAuth2.AuthURL
	cfg.Auth.OAuth2.TokenURL = dto.Auth.OAuth2.TokenURL
	cfg.Auth.OAuth2.UserInfoURL = dto.Auth.OAuth2.UserInfoURL
	cfg.Auth.OAuth2.Issuer = dto.Auth.OAuth2.Issuer
	cfg.Auth.OAuth2.Scopes = dto.Auth.OAuth2.Scopes
	cfg.Auth.OAuth2.RedirectURL = dto.Auth.OAuth2.RedirectURL
	cfg.Auth.OAuth2.UsernameClaim = dto.Auth.OAuth2.UsernameClaim
	cfg.Auth.OAuth2.DisplayNameClaim = dto.Auth.OAuth2.DisplayNameClaim
	cfg.Auth.OAuth2.EmailClaim = dto.Auth.OAuth2.EmailClaim
	cfg.Auth.OAuth2.AutoCreateUser = dto.Auth.OAuth2.AutoCreateUser
	cfg.Auth.OAuth2.InsecureSkipVerify = dto.Auth.OAuth2.InsecureSkipVerify

	return cfg
}

// FormatSizeString 将 int64 格式化为配额字符串
func FormatSizeString(val int64) string {
	if val >= 1024*1024*1024*1024 {
		return formatFloat(float64(val)/(1024*1024*1024*1024), 1) + "T"
	}
	if val >= 1024*1024*1024 {
		return formatFloat(float64(val)/(1024*1024*1024), 1) + "G"
	}
	if val >= 1024*1024 {
		return formatFloat(float64(val)/(1024*1024), 1) + "M"
	}
	if val >= 1024 {
		return formatFloat(float64(val)/1024, 1) + "K"
	}
	return formatFloat(float64(val), 0) + "B"
}

func formatFloat(val float64, decimals int) string {
	return fmt.Sprintf("%.*f", decimals, val)
}
