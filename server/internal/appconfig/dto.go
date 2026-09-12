package appconfig

import (
	"fmt"
	"strings"
)

// ConfigDTO 配置 API 响应/请求 DTO，与前端字段保持一致（JSON camelCase）
type ConfigDTO struct {
	App               AppConfigDTO       `json:"app"`
	Account           AccountResponse    `json:"account"`
	Server            ServerResponse     `json:"server"`
	Database          DatabaseConfigDTO  `json:"database"`
	RootDirs          []RootDirConfig    `json:"rootDirs"`
	AllowedExtensions []string           `json:"allowedExtensions"`
	PrivateFiles      PrivateConfigDTO   `json:"privateFiles"`
	TempFiles         TempFilesConfigDTO `json:"tempFiles"`
	Upload            UploadConfigDTO    `json:"upload"`
	Download          DownloadConfigDTO  `json:"download"`
	Preview           PreviewConfigDTO   `json:"preview"`
	OpenApi           *OpenApiConfigDTO  `json:"openApi"`
	Index             IndexConfigDTO     `json:"index"`
	Resource          ResourceConfigDTO  `json:"resource"`
	Security          SecurityConfigDTO  `json:"security"`
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
	RateLimitEnabled  bool     `json:"rateLimitEnabled"`
	RequestsPerMinute int      `json:"requestsPerMinute"`
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
	Name     string `json:"name"`
	Path     string `json:"path"`
	FullPath string `json:"fullPath"`
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
	ChunkSize   int64        `json:"chunkSize"`
	MaxFileSize int64        `json:"maxFileSize"`
	URLUpload   URLUploadDTO `json:"urlUpload"`
}

// URLUploadDTO URL 上传配置 DTO
type URLUploadDTO struct {
	Enabled            bool     `json:"enabled"`
	AllowedIPRanges    []string `json:"allowedIPRanges"`
	InsecureSkipVerify bool     `json:"insecureSkipVerify"`
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

// PreviewConfigDTO 预览配置 DTO
type PreviewConfigDTO struct {
	AllowMimes    string `json:"allowMimes"`
	AllowExts     string `json:"allowExts"`
	MaxInlineSize string `json:"maxInlineSize"`
	TextChunkSize string `json:"textChunkSize"`
}

// AccountResponse 账户配置 DTO
type AccountResponse struct {
	Anonymous         AnonymousResponse `json:"anonymous"`
	ReservedUsernames []string          `json:"reservedUsernames"`
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
func parseOrDefault(s string, defaultVal int64) int64 {
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
			Name:     name,
			Path:     name,
			FullPath: dir.Path,
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
		TempFiles: TempFilesConfigDTO{
			Enabled:           c.Storage.Temp.Enabled,
			Path:              c.Storage.Temp.Path,
			QuotaGlobal:       parseOrDefault(c.Storage.Temp.Quota.Global, 0),
			QuotaPerIP:        parseOrDefault(c.Storage.Temp.Quota.PerIP, 0),
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
			AllowMimes:    c.Preview.AllowMimes,
			AllowExts:     c.Preview.AllowExts,
			MaxInlineSize: c.Preview.MaxInlineSize,
			TextChunkSize: c.Preview.TextChunkSize,
		},
		OpenApi: &OpenApiConfigDTO{
			Enabled:           c.OpenAPI.Enabled,
			IpAccessMode:      c.OpenAPI.IPAccessMode,
			IpWhitelist:       c.OpenAPI.IPWhitelist,
			IpBlacklist:       c.OpenAPI.IPBlacklist,
			RateLimitEnabled:  c.OpenAPI.RateLimitEnabled,
			RequestsPerMinute: c.OpenAPI.RequestsPerMinute,
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
	}
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
			}
		}
	}

	cfg.Storage.AllowedExtensions = dto.AllowedExtensions

	// 私有存储（前端字段名 privateFiles）
	cfg.Storage.Private.Enabled = dto.PrivateFiles.Enabled
	cfg.Storage.Private.Path = dto.PrivateFiles.Path
	cfg.Storage.Private.Quota.GlobalQuota = dto.PrivateFiles.QuotaGlobal
	cfg.Storage.Private.Quota.PerUserQuota = dto.PrivateFiles.QuotaPerUser

	// 临时文件
	cfg.Storage.Temp.Enabled = dto.TempFiles.Enabled
	cfg.Storage.Temp.Path = dto.TempFiles.Path
	cfg.Storage.Temp.Quota.Global = formatSizeToString(dto.TempFiles.QuotaGlobal)
	cfg.Storage.Temp.Quota.PerIP = formatSizeToString(dto.TempFiles.QuotaPerIP)
	cfg.Storage.Temp.DefaultExpireDays = dto.TempFiles.DefaultExpireDays
	cfg.Storage.Temp.DeleteOnDownload = dto.TempFiles.DeleteOnDownload

	// 上传
	cfg.Upload.ChunkSize = dto.Upload.ChunkSize
	cfg.Upload.MaxFileSize = dto.Upload.MaxFileSize
	cfg.Upload.URLUpload.Enabled = dto.Upload.URLUpload.Enabled
	cfg.Upload.URLUpload.AllowedIPRanges = dto.Upload.URLUpload.AllowedIPRanges
	cfg.Upload.URLUpload.InsecureSkipVerify = dto.Upload.URLUpload.InsecureSkipVerify

	// 下载限流
	cfg.Download.RateLimit.WindowMinutes = dto.Download.RateLimit.WindowMinutes
	cfg.Download.RateLimit.MaxRequests = dto.Download.RateLimit.MaxRequests
	cfg.Download.RateLimit.LockAfter = dto.Download.RateLimit.LockAfter
	cfg.Download.RateLimit.LockMinutes = dto.Download.RateLimit.LockMinutes

	// 预览
	cfg.Preview.AllowMimes = dto.Preview.AllowMimes
	cfg.Preview.AllowExts = dto.Preview.AllowExts
	cfg.Preview.MaxInlineSize = dto.Preview.MaxInlineSize
	cfg.Preview.TextChunkSize = dto.Preview.TextChunkSize

	if dto.OpenApi != nil {
		cfg.OpenAPI.Enabled = dto.OpenApi.Enabled
		cfg.OpenAPI.IPAccessMode = dto.OpenApi.IpAccessMode
		cfg.OpenAPI.IPWhitelist = dto.OpenApi.IpWhitelist
		cfg.OpenAPI.IPBlacklist = dto.OpenApi.IpBlacklist
		cfg.OpenAPI.RateLimitEnabled = dto.OpenApi.RateLimitEnabled
		cfg.OpenAPI.RequestsPerMinute = dto.OpenApi.RequestsPerMinute
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

	return cfg
}

// formatSizeToString 将 int64 格式化为配额字符串
func formatSizeToString(val int64) string {
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
