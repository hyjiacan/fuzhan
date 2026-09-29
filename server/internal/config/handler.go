// Package config 处理系统配置
package config

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	appconfig "fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/middleware"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetOptions 处理 /api/v1/options 接口（公开配置信息）
func (h *Handler) GetOptions(c *gin.Context) {
	currentConfig := appconfig.GetConfig()
	resp := gin.H{
		"app": gin.H{
			"name": currentConfig.App.Name,
		},
		"privateStorage": gin.H{
			"enabled": currentConfig.Storage.Private.Enabled,
		},
		"tempFiles": gin.H{
			"enabled": currentConfig.Storage.Temp.Enabled,
		},
		"ftp": gin.H{
			"enabled": currentConfig.Server.FTP.Enabled,
			"port":    currentConfig.Server.FTP.Port,
		},
		"ftps": gin.H{
			"enabled": currentConfig.Server.FTPS.Enabled,
			"port":    currentConfig.Server.FTPS.Port,
		},
		"webdav": gin.H{
			"enabled":        currentConfig.Server.WebDAV.Enabled,
			"publicUsername": currentConfig.Account.Anonymous.Username,
		},
		"upload": gin.H{
			"chunkSize":   currentConfig.Upload.ChunkSize,
			"maxFileSize": currentConfig.Upload.MaxFileSize,
			"urlUpload": gin.H{
				"enabled":            currentConfig.Upload.URLUpload.Enabled,
				"allowedIPRanges":    currentConfig.Upload.URLUpload.AllowedIPRanges,
				"insecureSkipVerify": currentConfig.Upload.URLUpload.InsecureSkipVerify,
			},
		},
		"download": gin.H{
			"rateLimit": gin.H{
				"windowMinutes": currentConfig.Download.RateLimit.WindowMinutes,
				"maxRequests":   currentConfig.Download.RateLimit.MaxRequests,
				"lockAfter":     currentConfig.Download.RateLimit.LockAfter,
				"lockMinutes":   currentConfig.Download.RateLimit.LockMinutes,
			},
		},
		"preview": gin.H{
			"allowMimes": currentConfig.Preview.AllowMimes,
			"allowExts":  currentConfig.Preview.AllowExts,
		},
		// 用户自定义 MIME 表：供前端文件列表「是否可预览」判定与后端预览识别口径一致
		"mimeTable": currentConfig.MimeTable,
		"openApi": gin.H{
			"enabled": currentConfig.OpenAPI.Enabled,
		},
	}
	response.HandleSuccess(c, http.StatusOK, "", resp)
}

// Handler 系统配置处理器
type Handler struct{}

// NewHandler 创建配置处理器实例
func NewHandler() *Handler {
	return &Handler{}
}

// GetConfig 获取当前配置
func (h *Handler) GetConfig(c *gin.Context) {
	resp := appconfig.GlobalConfig.ToDTO()
	response.HandleSuccess(c, http.StatusOK, "", resp)
}

// SaveConfigRequest 保存配置请求
// 字段名与前端设置页提交内容严格一致，避免保存时覆盖未提交的配置项。
type SaveConfigRequest struct {
	App               appconfig.AppConfigDTO       `json:"app"`
	Account           appconfig.AccountResponse    `json:"account"`
	Server            appconfig.ServerResponse     `json:"server"`
	Database          appconfig.DatabaseConfigDTO  `json:"database"`
	RootDirs          []appconfig.RootDirConfig    `json:"rootDirs"`
	AllowedExtensions []string                     `json:"allowedExtensions"`
	PrivateFiles      appconfig.PrivateConfigDTO   `json:"privateFiles"`
	TempFiles         appconfig.TempFilesConfigDTO `json:"tempFiles"`
	Upload            appconfig.UploadConfigDTO    `json:"upload"`
	Download          appconfig.DownloadConfigDTO  `json:"download"`
	Preview           appconfig.PreviewConfigDTO   `json:"preview"`
	MimeTable         map[string]string            `json:"mimeTable"`
	OpenApi           *appconfig.OpenApiConfigDTO  `json:"openApi"`
	Index             appconfig.IndexConfigDTO     `json:"index"`
	Resource          appconfig.ResourceConfigDTO  `json:"resource"`
	Security          appconfig.SecurityConfigDTO  `json:"security"`
	Auth              appconfig.AuthConfigDTO      `json:"auth"`
}

func (h *Handler) SaveConfig(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		utils.HandleBadRequest(c, "读取请求数据失败: "+err.Error(), nil)
		return
	}

	var req SaveConfigRequest
	if err := json.Unmarshal(body, &req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}

	// 解析原始 JSON 以检测哪些配置节被显式提交。
	// 前端只提交设置页展示的子集，未提交的配置项必须保持原值，绝不能重置为空。
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(body, &raw)
	present := func(key string) bool {
		_, ok := raw[key]
		return ok
	}

	// 加写锁保护配置并发修改
	appconfig.LockConfig()
	defer appconfig.UnlockConfig()

	// 记录现有根目录配额，避免保存时（前端不传递配额）把配额重置为 0
	existingQuotas := map[string]int64{}
	for _, d := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		existingQuotas[d.Path] = d.Quota
	}

	updates := appconfig.NewConfigUpdates()

	// —— 应用信息 ——
	if present("app") && req.App.Name != "" {
		appconfig.GlobalConfig.App.Name = req.App.Name
		updates.Add("app.name", req.App.Name)
	}

	// —— 账户配置 ——
	if present("account") {
		if req.Account.Anonymous.Username != "" {
			appconfig.GlobalConfig.Account.Anonymous.Username = req.Account.Anonymous.Username
			updates.Add("account.anonymous.username", req.Account.Anonymous.Username)
		}
		if req.Account.ReservedUsernames != nil {
			appconfig.GlobalConfig.Account.ReservedUsernames = req.Account.ReservedUsernames
			reservedList := make([]interface{}, len(req.Account.ReservedUsernames))
			for i, u := range req.Account.ReservedUsernames {
				reservedList[i] = u
			}
			updates.Add("account.reserved_usernames", reservedList)
		}
	}

	// —— 服务器配置 ——
	if present("server") {
		if req.Server.Host != "" {
			appconfig.GlobalConfig.Server.Host = req.Server.Host
			updates.Add("server.host", req.Server.Host)
		}
		if req.Server.HTTP.Enabled || req.Server.HTTP.Port > 0 {
			appconfig.GlobalConfig.Server.HTTP.Enabled = req.Server.HTTP.Enabled
			updates.Add("server.http.enabled", req.Server.HTTP.Enabled)
		}
		if req.Server.HTTP.Port > 0 {
			appconfig.GlobalConfig.Server.HTTP.Port = req.Server.HTTP.Port
			updates.Add("server.http.port", req.Server.HTTP.Port)
		}
		if req.Server.HTTPS.Enabled || req.Server.HTTPS.Port > 0 {
			appconfig.GlobalConfig.Server.HTTPS.Enabled = req.Server.HTTPS.Enabled
			updates.Add("server.https.enabled", req.Server.HTTPS.Enabled)
		}
		if req.Server.HTTPS.Port > 0 {
			appconfig.GlobalConfig.Server.HTTPS.Port = req.Server.HTTPS.Port
			updates.Add("server.https.port", req.Server.HTTPS.Port)
		}
		if req.Server.FTP.Enabled || req.Server.FTP.Port > 0 {
			appconfig.GlobalConfig.Server.FTP.Enabled = req.Server.FTP.Enabled
			updates.Add("server.ftp.enabled", req.Server.FTP.Enabled)
		}
		if req.Server.FTP.Port > 0 {
			appconfig.GlobalConfig.Server.FTP.Port = req.Server.FTP.Port
			updates.Add("server.ftp.port", req.Server.FTP.Port)
		}
		if req.Server.FTPS.Enabled || req.Server.FTPS.Port > 0 {
			appconfig.GlobalConfig.Server.FTPS.Enabled = req.Server.FTPS.Enabled
			updates.Add("server.ftps.enabled", req.Server.FTPS.Enabled)
		}
		if req.Server.FTPS.Port > 0 {
			appconfig.GlobalConfig.Server.FTPS.Port = req.Server.FTPS.Port
			updates.Add("server.ftps.port", req.Server.FTPS.Port)
		}
		// WebDAV 端口未被前端管理，仅保存启用状态
		if req.Server.WebDAV.Enabled {
			appconfig.GlobalConfig.Server.WebDAV.Enabled = true
			updates.Add("server.webdav.enabled", true)
		} else {
			appconfig.GlobalConfig.Server.WebDAV.Enabled = false
			updates.Add("server.webdav.enabled", false)
		}
	}

	// —— 数据库 ——
	if present("database") {
		if req.Database.Driver != "" {
			appconfig.GlobalConfig.Database.Driver = req.Database.Driver
			updates.Add("database.driver", req.Database.Driver)
		}
		if req.Database.DSN != "" {
			appconfig.GlobalConfig.Database.DSN = req.Database.DSN
			updates.Add("database.dsn", req.Database.DSN)
		}
	}

	// —— 共享目录（保留既有配额）——
	if present("rootDirs") && len(req.RootDirs) > 0 {
		dirs := make([]appconfig.DirectoryConfig, len(req.RootDirs))
		fileDirs := make([]interface{}, len(req.RootDirs))
		newRootNames := map[string]string{}
		for i, dir := range req.RootDirs {
			p := dir.Path
			if p == "" {
				p = dir.FullPath
			}
			quota := existingQuotas[p]
			dirs[i] = appconfig.DirectoryConfig{
				Path:        p,
				Quota:       quota,
				AllowUpload: appconfig.BoolPtrFalseWhenDisabled(dir.UploadEnabled),
			}
			fileDirs[i] = map[string]interface{}{
				"path":         p,
				"quota":        quota,
				"allow_upload": dir.UploadEnabled,
			}
			if name := filepath.Base(p); name != "" && name != "." && name != string(filepath.Separator) {
				newRootNames[name] = p
			}
		}
		appconfig.GlobalConfig.Storage.Public.RootDirs = dirs
		if len(newRootNames) > 0 {
			appconfig.RootNames = newRootNames
		}
		updates.Add("storage.public.root_dirs", fileDirs)
	}

	// —— 允许的扩展名 ——
	if present("allowedExtensions") {
		appconfig.GlobalConfig.Storage.AllowedExtensions = req.AllowedExtensions
		// 前端可能清空列表（表示允许所有扩展名），此时也必须写回空序列
		extList := make([]interface{}, len(req.AllowedExtensions))
		for i, ext := range req.AllowedExtensions {
			extList[i] = ext
		}
		updates.Add("storage.allowed_extensions", extList)
	}

	// —— 私有文件存储（前端字段名 privateFiles）——
	if present("privateFiles") {
		appconfig.GlobalConfig.Storage.Private.Enabled = req.PrivateFiles.Enabled
		appconfig.GlobalConfig.Storage.Private.Path = req.PrivateFiles.Path
		appconfig.GlobalConfig.Storage.Private.Quota.GlobalQuota = req.PrivateFiles.QuotaGlobal
		appconfig.GlobalConfig.Storage.Private.Quota.PerUserQuota = req.PrivateFiles.QuotaPerUser
		updates.Add("storage.private.enabled", req.PrivateFiles.Enabled)
		updates.Add("storage.private.path", req.PrivateFiles.Path)
		updates.Add("storage.private.quota.global", appconfig.FormatSizeString(req.PrivateFiles.QuotaGlobal))
		updates.Add("storage.private.quota.per_user", appconfig.FormatSizeString(req.PrivateFiles.QuotaPerUser))
	}

	// —— 临时文件（前端字段名 tempFiles）——
	if present("tempFiles") {
		appconfig.GlobalConfig.Storage.Temp.Enabled = req.TempFiles.Enabled
		appconfig.GlobalConfig.Storage.Temp.Path = req.TempFiles.Path
		appconfig.GlobalConfig.Storage.Temp.Quota.Global = appconfig.FormatSizeString(req.TempFiles.QuotaGlobal)
		appconfig.GlobalConfig.Storage.Temp.Quota.PerIP = appconfig.FormatSizeString(req.TempFiles.QuotaPerIP)
		appconfig.GlobalConfig.Storage.Temp.DefaultExpireDays = req.TempFiles.DefaultExpireDays
		appconfig.GlobalConfig.Storage.Temp.DeleteOnDownload = req.TempFiles.DeleteOnDownload
		updates.Add("storage.temp.enabled", req.TempFiles.Enabled)
		updates.Add("storage.temp.path", req.TempFiles.Path)
		updates.Add("storage.temp.quota.global", appconfig.FormatSizeString(req.TempFiles.QuotaGlobal))
		updates.Add("storage.temp.quota.per_ip", appconfig.FormatSizeString(req.TempFiles.QuotaPerIP))
		updates.Add("storage.temp.default_expire_days", req.TempFiles.DefaultExpireDays)
		updates.Add("storage.temp.delete_on_download", req.TempFiles.DeleteOnDownload)
	}

	// —— 上传 ——
	if present("upload") {
		if req.Upload.ChunkSize > 0 {
			appconfig.GlobalConfig.Upload.ChunkSize = req.Upload.ChunkSize
			updates.Add("upload.chunk_size", appconfig.FormatSizeString(req.Upload.ChunkSize))
		}
		appconfig.GlobalConfig.Upload.MaxFileSize = req.Upload.MaxFileSize
		updates.Add("upload.max_file_size", appconfig.FormatSizeString(req.Upload.MaxFileSize))
		appconfig.GlobalConfig.Upload.URLUpload.Enabled = req.Upload.URLUpload.Enabled
		appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges = req.Upload.URLUpload.AllowedIPRanges
		appconfig.GlobalConfig.Upload.URLUpload.InsecureSkipVerify = req.Upload.URLUpload.InsecureSkipVerify
		updates.Add("upload.url_upload.enabled", req.Upload.URLUpload.Enabled)
		allowedIPRanges := make([]interface{}, len(req.Upload.URLUpload.AllowedIPRanges))
		for i, ip := range req.Upload.URLUpload.AllowedIPRanges {
			allowedIPRanges[i] = ip
		}
		updates.Add("upload.url_upload.allowed_ip_ranges", allowedIPRanges)
		updates.Add("upload.url_upload.insecure_skip_verify", req.Upload.URLUpload.InsecureSkipVerify)
	}

	// —— 下载限流（max_requests=0 表示不限制）——
	if present("download") {
		appconfig.GlobalConfig.Download.RateLimit.WindowMinutes = req.Download.RateLimit.WindowMinutes
		appconfig.GlobalConfig.Download.RateLimit.MaxRequests = req.Download.RateLimit.MaxRequests
		appconfig.GlobalConfig.Download.RateLimit.LockAfter = req.Download.RateLimit.LockAfter
		appconfig.GlobalConfig.Download.RateLimit.LockMinutes = req.Download.RateLimit.LockMinutes
		updates.Add("download.rate_limit.window_minutes", req.Download.RateLimit.WindowMinutes)
		updates.Add("download.rate_limit.max_requests", req.Download.RateLimit.MaxRequests)
		updates.Add("download.rate_limit.lock_after", req.Download.RateLimit.LockAfter)
		updates.Add("download.rate_limit.lock_minutes", req.Download.RateLimit.LockMinutes)
	}

	// —— 预览 ——
	if present("preview") {
		appconfig.GlobalConfig.Preview.AllowMimes = req.Preview.AllowMimes
		appconfig.GlobalConfig.Preview.AllowExts = req.Preview.AllowExts
		appconfig.GlobalConfig.Preview.MaxInlineSize = req.Preview.MaxInlineSize
		appconfig.GlobalConfig.Preview.TextChunkSize = req.Preview.TextChunkSize
		updates.Add("preview.allow_mimes", req.Preview.AllowMimes)
		updates.Add("preview.allow_exts", req.Preview.AllowExts)
		updates.Add("preview.max_inline_size", req.Preview.MaxInlineSize)
		updates.Add("preview.text_chunk_size", req.Preview.TextChunkSize)
	}

	// —— 用户自定义 MIME 表（扩展名 -> MIME 映射）——
	if present("mimeTable") {
		appconfig.GlobalConfig.MimeTable = req.MimeTable
		updates.Add("mime_table", req.MimeTable)
	}

	// —— OpenAPI ——
	if req.OpenApi != nil && present("openApi") {
		appconfig.GlobalConfig.OpenAPI.Enabled = req.OpenApi.Enabled
		appconfig.GlobalConfig.OpenAPI.IPAccessMode = req.OpenApi.IpAccessMode
		appconfig.GlobalConfig.OpenAPI.IPWhitelist = req.OpenApi.IpWhitelist
		appconfig.GlobalConfig.OpenAPI.IPBlacklist = req.OpenApi.IpBlacklist
		appconfig.GlobalConfig.OpenAPI.RateLimitEnabled = req.OpenApi.RateLimitEnabled
		updates.Add("open_api.enabled", req.OpenApi.Enabled)
		updates.Add("open_api.ip_access_mode", req.OpenApi.IpAccessMode)
		ipWhitelist := make([]interface{}, len(req.OpenApi.IpWhitelist))
		for i, ip := range req.OpenApi.IpWhitelist {
			ipWhitelist[i] = ip
		}
		updates.Add("open_api.ip_whitelist", ipWhitelist)
		ipBlacklist := make([]interface{}, len(req.OpenApi.IpBlacklist))
		for i, ip := range req.OpenApi.IpBlacklist {
			ipBlacklist[i] = ip
		}
		updates.Add("open_api.ip_blacklist", ipBlacklist)
		updates.Add("open_api.rate_limit_enabled", req.OpenApi.RateLimitEnabled)
		if req.OpenApi.RequestsPerMinute > 0 {
			appconfig.GlobalConfig.OpenAPI.RequestsPerMinute = req.OpenApi.RequestsPerMinute
			updates.Add("open_api.requests_per_minute", req.OpenApi.RequestsPerMinute)
		}
	}

	// —— 文件索引（定时扫描）——
	if present("index") {
		appconfig.GlobalConfig.Index.ScanCronExpression = req.Index.ScanCronExpression
		updates.Add("index.scan_cron_expression", req.Index.ScanCronExpression)

		// —— 检索索引对齐任务 cron ——
		appconfig.GlobalConfig.Index.SearchReconcileCronExpression = req.Index.SearchReconcileCronExpression
		updates.Add("index.search_reconcile_cron_expression", req.Index.SearchReconcileCronExpression)

		// —— 启动扫描延迟 ——
		if req.Index.ScanStartDelaySeconds > 0 {
			appconfig.GlobalConfig.Index.ScanStartDelaySeconds = req.Index.ScanStartDelaySeconds
			updates.Add("index.scan_start_delay_seconds", req.Index.ScanStartDelaySeconds)
		}
	}

	// —— 服务器资源监控 ——
	if present("resource") {
		appconfig.GlobalConfig.Resource.Enabled = req.Resource.Enabled
		updates.Add("resource.enabled", req.Resource.Enabled)
		// 未配置（<=0）时保持默认值，不写入 0/负值
		if req.Resource.SamplingInterval > 0 {
			appconfig.GlobalConfig.Resource.SamplingInterval = req.Resource.SamplingInterval
			updates.Add("resource.sampling_interval", req.Resource.SamplingInterval)
		}
		if req.Resource.CollectInterval > 0 {
			appconfig.GlobalConfig.Resource.CollectInterval = req.Resource.CollectInterval
			updates.Add("resource.collect_interval", req.Resource.CollectInterval)
		}
		if req.Resource.RetentionDays > 0 {
			appconfig.GlobalConfig.Resource.RetentionDays = req.Resource.RetentionDays
			updates.Add("resource.retention_days", req.Resource.RetentionDays)
		}
	}

	// —— 安全配置（trust_proxy / CORS）——
	if present("security") {
		appconfig.GlobalConfig.Security.TrustProxy = req.Security.TrustProxy
		updates.Add("security.trust_proxy", req.Security.TrustProxy)

		appconfig.GlobalConfig.Security.AllowedOrigins = req.Security.AllowedOrigins
		origins := make([]interface{}, len(req.Security.AllowedOrigins))
		for i, o := range req.Security.AllowedOrigins {
			origins[i] = o
		}
		updates.Add("security.allowed_origins", origins)
	}

	// —— LDAP 认证配置 ——
	// 前端只提交发生变化的顶级节，auth 一旦提交则整个 LDAP 节点原样写回
	if present("auth") {
		ld := req.Auth.LDAP
		appconfig.GlobalConfig.Auth.LDAP.Enabled = ld.Enabled
		appconfig.GlobalConfig.Auth.LDAP.Host = ld.Host
		appconfig.GlobalConfig.Auth.LDAP.Port = ld.Port
		appconfig.GlobalConfig.Auth.LDAP.UseSSL = ld.UseSSL
		appconfig.GlobalConfig.Auth.LDAP.BaseDN = ld.BaseDN
		appconfig.GlobalConfig.Auth.LDAP.BindDN = ld.BindDN
		appconfig.GlobalConfig.Auth.LDAP.BindPassword = ld.BindPassword
		appconfig.GlobalConfig.Auth.LDAP.UserFilter = ld.UserFilter
		appconfig.GlobalConfig.Auth.LDAP.SyncInterval = ld.SyncInterval
		appconfig.GlobalConfig.Auth.LDAP.AutoCreateUser = ld.AutoCreateUser
		appconfig.GlobalConfig.Auth.LDAP.InsecureSkipVerify = ld.InsecureSkipVerify
		updates.Add("auth.ldap.enabled", ld.Enabled)
		updates.Add("auth.ldap.host", ld.Host)
		updates.Add("auth.ldap.port", ld.Port)
		updates.Add("auth.ldap.use_ssl", ld.UseSSL)
		updates.Add("auth.ldap.base_dn", ld.BaseDN)
		updates.Add("auth.ldap.bind_dn", ld.BindDN)
		updates.Add("auth.ldap.bind_password", ld.BindPassword)
		updates.Add("auth.ldap.user_filter", ld.UserFilter)
		updates.Add("auth.ldap.sync_interval", ld.SyncInterval)
		updates.Add("auth.ldap.auto_create_user", ld.AutoCreateUser)
		updates.Add("auth.ldap.insecure_skip_verify", ld.InsecureSkipVerify)

		// —— OAuth2/OIDC 认证配置 ——
		oauth := req.Auth.OAuth2
		appconfig.GlobalConfig.Auth.OAuth2.Enabled = oauth.Enabled
		appconfig.GlobalConfig.Auth.OAuth2.ProviderName = oauth.ProviderName
		appconfig.GlobalConfig.Auth.OAuth2.ClientID = oauth.ClientID
		appconfig.GlobalConfig.Auth.OAuth2.ClientSecret = oauth.ClientSecret
		appconfig.GlobalConfig.Auth.OAuth2.AuthURL = oauth.AuthURL
		appconfig.GlobalConfig.Auth.OAuth2.TokenURL = oauth.TokenURL
		appconfig.GlobalConfig.Auth.OAuth2.UserInfoURL = oauth.UserInfoURL
		appconfig.GlobalConfig.Auth.OAuth2.Issuer = oauth.Issuer
		appconfig.GlobalConfig.Auth.OAuth2.Scopes = oauth.Scopes
		appconfig.GlobalConfig.Auth.OAuth2.RedirectURL = oauth.RedirectURL
		appconfig.GlobalConfig.Auth.OAuth2.UsernameClaim = oauth.UsernameClaim
		appconfig.GlobalConfig.Auth.OAuth2.DisplayNameClaim = oauth.DisplayNameClaim
		appconfig.GlobalConfig.Auth.OAuth2.EmailClaim = oauth.EmailClaim
		appconfig.GlobalConfig.Auth.OAuth2.AutoCreateUser = oauth.AutoCreateUser
		appconfig.GlobalConfig.Auth.OAuth2.InsecureSkipVerify = oauth.InsecureSkipVerify
		updates.Add("auth.oauth2.enabled", oauth.Enabled)
		updates.Add("auth.oauth2.provider_name", oauth.ProviderName)
		updates.Add("auth.oauth2.client_id", oauth.ClientID)
		updates.Add("auth.oauth2.client_secret", oauth.ClientSecret)
		updates.Add("auth.oauth2.auth_url", oauth.AuthURL)
		updates.Add("auth.oauth2.token_url", oauth.TokenURL)
		updates.Add("auth.oauth2.user_info_url", oauth.UserInfoURL)
		updates.Add("auth.oauth2.issuer", oauth.Issuer)
		scopeList := make([]interface{}, len(oauth.Scopes))
		for i, sc := range oauth.Scopes {
			scopeList[i] = sc
		}
		updates.Add("auth.oauth2.scopes", scopeList)
		updates.Add("auth.oauth2.redirect_url", oauth.RedirectURL)
		updates.Add("auth.oauth2.username_claim", oauth.UsernameClaim)
		updates.Add("auth.oauth2.display_name_claim", oauth.DisplayNameClaim)
		updates.Add("auth.oauth2.email_claim", oauth.EmailClaim)
		updates.Add("auth.oauth2.auto_create_user", oauth.AutoCreateUser)
		updates.Add("auth.oauth2.insecure_skip_verify", oauth.InsecureSkipVerify)
	}

	// 私有/临时文件存储目录创建
	if appconfig.GlobalConfig.Storage.Private.Enabled && appconfig.GlobalConfig.Storage.Private.Path != "" {
		for _, dir := range []string{
			appconfig.GlobalConfig.Storage.Private.Path,
			filepath.Join(appconfig.GlobalConfig.Storage.Private.Path, "files"),
			filepath.Join(appconfig.GlobalConfig.Storage.Private.Path, "users"),
			filepath.Join(appconfig.GlobalConfig.Storage.Private.Path, "metadata"),
		} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				utils.Warn("创建私有存储目录失败", utils.String("dir", dir), utils.Err(err))
			}
		}
	}
	if appconfig.GlobalConfig.Storage.Temp.Enabled && appconfig.GlobalConfig.Storage.Temp.Path != "" {
		if err := os.MkdirAll(appconfig.GlobalConfig.Storage.Temp.Path, 0755); err != nil {
			utils.Warn("创建临时文件目录失败", utils.String("dir", appconfig.GlobalConfig.Storage.Temp.Path), utils.Err(err))
		}
	}

	// 写入配置文件（使用启动时解析的真实路径，避免工作目录不同导致写入错误的文件）
	if err := updates.Apply(appconfig.ConfigPath); err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, "保存配置失败: "+err.Error(), nil)
		return
	}

	utils.InitPreviewConfig(appconfig.GlobalConfig.Preview.AllowMimes, appconfig.GlobalConfig.Preview.AllowExts)
	utils.InitMimeTable(appconfig.GlobalConfig.MimeTable)
	appconfig.RunOnSaveHooks()

	// 记录配置修改操作，便于审计追踪（关键操作需留痕）
	middleware.LogOperation(c, "config.update", "global_config", nil)
	response.HandleSuccess(c, http.StatusOK, "配置已保存并生效", nil)
}

// TestLDAPConfig 测试 LDAP 连接。可选 JSON body（LDAPConfigDTO）用于测试未保存的表单值；
// body 为空时使用当前已保存的配置。仅做连接与服务账号绑定探测，不验证具体用户。
func (h *Handler) TestLDAPConfig(c *gin.Context) {
	var dto appconfig.LDAPConfigDTO
	body, _ := io.ReadAll(c.Request.Body)
	useBody := len(strings.TrimSpace(string(body))) > 0
	if useBody {
		if err := json.Unmarshal(body, &dto); err != nil {
			utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
			return
		}
	} else {
		saved := appconfig.GetConfig().Auth.LDAP
		dto = appconfig.LDAPConfigDTO{
			Enabled:            saved.Enabled,
			Host:               saved.Host,
			Port:               saved.Port,
			UseSSL:             saved.UseSSL,
			BaseDN:             saved.BaseDN,
			BindDN:             saved.BindDN,
			BindPassword:       saved.BindPassword,
			UserFilter:         saved.UserFilter,
			SyncInterval:       saved.SyncInterval,
			AutoCreateUser:     saved.AutoCreateUser,
			InsecureSkipVerify: saved.InsecureSkipVerify,
		}
	}

	if dto.Host == "" {
		utils.HandleBadRequest(c, "LDAP 服务器地址未配置", nil)
		return
	}

	svc := auth.NewLDAPService(nil, &auth.LDAPConfig{
		Enabled:            dto.Enabled,
		Host:               dto.Host,
		Port:               dto.Port,
		UseSSL:             dto.UseSSL,
		BaseDN:             dto.BaseDN,
		BindDN:             dto.BindDN,
		BindPassword:       dto.BindPassword,
		UserFilter:         dto.UserFilter,
		SyncInterval:       dto.SyncInterval,
		AutoCreateUser:     dto.AutoCreateUser,
		InsecureSkipVerify: dto.InsecureSkipVerify,
	})
	if err := svc.TestConnection(); err != nil {
		utils.HandleBadRequest(c, "LDAP 连接测试失败: "+err.Error(), nil)
		return
	}
	response.HandleSuccess(c, http.StatusOK, "LDAP 连接测试成功", nil)
}

// TestOAuth2Config 测试 OAuth2/OIDC 配置：校验凭据并解析端点（必要时执行 OIDC 发现）。
// 可选 JSON body（OAuth2ConfigDTO）用于测试未保存的表单值；body 为空时使用当前已保存的配置。
func (h *Handler) TestOAuth2Config(c *gin.Context) {
	var dto appconfig.OAuth2ConfigDTO
	body, _ := io.ReadAll(c.Request.Body)
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &dto); err != nil {
			utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
			return
		}
	} else {
		dto = appconfig.GlobalConfig.ToDTO().Auth.OAuth2
	}

	svc := auth.NewOAuth2Service(nil, &auth.OAuth2Config{
		Enabled:            dto.Enabled,
		ProviderName:       dto.ProviderName,
		ClientID:           dto.ClientID,
		ClientSecret:       dto.ClientSecret,
		AuthURL:            dto.AuthURL,
		TokenURL:           dto.TokenURL,
		UserInfoURL:        dto.UserInfoURL,
		Issuer:             dto.Issuer,
		Scopes:             dto.Scopes,
		RedirectURL:        dto.RedirectURL,
		UsernameClaim:      dto.UsernameClaim,
		DisplayNameClaim:   dto.DisplayNameClaim,
		EmailClaim:         dto.EmailClaim,
		AutoCreateUser:     dto.AutoCreateUser,
		InsecureSkipVerify: dto.InsecureSkipVerify,
	})
	if err := svc.TestConnection(); err != nil {
		utils.HandleBadRequest(c, "OAuth2 配置测试失败: "+err.Error(), nil)
		return
	}
	response.HandleSuccess(c, http.StatusOK, "OAuth2 配置测试成功（端点可达）", nil)
}
