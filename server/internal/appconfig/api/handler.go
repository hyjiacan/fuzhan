// Package api 系统配置管理 HTTP 接口（配置读取/保存/LDAP、OAuth2 连通性测试）。
// 隶属 appconfig 领域：配置结构与读写核心在 appconfig 包，本包仅做接口层，
// 因依赖 auth/resource（二者反向依赖 appconfig）不能并入 appconfig 包本身。
package api

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
	"fuzhan/internal/resource"
	"fuzhan/internal/utils"
	"fuzhan/internal/webhook"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// ListPartitions 返回本机磁盘分区列表（含总容量/剩余容量），供设置页共享目录选择存储分区。
func (h *Handler) ListPartitions(c *gin.Context) {
	response.HandleSuccess(c, http.StatusOK, "", resource.ListPartitions())
}

// GetOptions 处理 /api/v1/web/options 接口（公开配置信息）
func (h *Handler) GetOptions(c *gin.Context) {
	currentConfig := appconfig.GetConfig()
	resp := gin.H{
		"app": gin.H{
			"name": currentConfig.App.Name,
		},
		"account": gin.H{
			// 注册开关：登录/注册入口据此决定是否展示「立即注册」
			"allowRegistration": currentConfig.Account.AllowRegistration,
			// 强制登录开关：前端据此决定首次加载前移登录页、路由守卫与下载链接是否附带 token
			"requireLogin": currentConfig.Account.RequireLogin,
			// 登录/注册图形验证码：mode/triggerAfter 为配置；required 按当前请求 IP 的失败次数动态计算
			"captcha": gin.H{
				"mode":         currentConfig.Account.Captcha.Mode,
				"triggerAfter": currentConfig.Account.Captcha.TriggerAfter,
				"required":     auth.CaptchaRequired(utils.GetClientIP(c)),
			},
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
				"insecureSkipVerify": currentConfig.Upload.URLUpload.InsecureSkipVerify,
				"maxConcurrent":      currentConfig.Upload.URLUpload.MaxConcurrent,
				"maxRetries":         currentConfig.Upload.URLUpload.MaxRetries,
				"maxSize":            currentConfig.Upload.URLUpload.MaxSize,
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
		"preview": buildPreviewOptions(currentConfig.Preview),
		// 用户自定义 MIME 表：供前端文件列表「是否可预览」判定与后端预览识别口径一致
		"mimeTable": currentConfig.MimeTable,
		"openApi": gin.H{
			"enabled": currentConfig.OpenAPI.Enabled,
		},
	}
	response.HandleSuccess(c, http.StatusOK, "", resp)
}

// buildPreviewOptions 构造公开 options 的预览节点：全局开关 + 各分组生效配置（已解析默认值）
// + 内置默认表 defaults（供前端设置页「恢复默认」使用），与前端 config/preview.js 的消费结构保持一致。
// 后端是预览类型默认值的唯一来源，前端不内置默认表。
func buildPreviewOptions(p appconfig.PreviewConfig) gin.H {
	groups := gin.H{}
	defaults := gin.H{}
	for _, name := range []string{"image", "text", "audio", "video", "document", "custom"} {
		groups[name] = gin.H{
			"enabled": p.IsGroupEnabled(name),
			"mimes":   p.GroupMimes(name),
			"exts":    p.GroupExts(name),
		}
		// custom 组默认关闭，其余内置分组默认开启
		defaults[name] = gin.H{
			"enabled": name != "custom",
			"mimes":   utils.DefaultPreviewGroupMimes(name),
			"exts":    utils.DefaultPreviewGroupExts(name),
		}
	}
	return gin.H{"enabled": p.IsEnabled(), "groups": groups, "defaults": defaults}
}

// applyPreviewGroup 将提交的预览分组 DTO 合入全局配置：指针字段未提交（nil）沿用当前生效值，
// 显式提交（含空串）按提交值生效。
func applyPreviewGroup(dst *appconfig.PreviewGroupConfig, src appconfig.PreviewGroupDTO, key string, updates appconfig.ConfigUpdates) {
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
		updates.Add(key+".enabled", *src.Enabled)
	}
	if src.Mimes != nil {
		dst.Mimes = src.Mimes
		updates.Add(key+".mimes", *src.Mimes)
	}
	if src.Exts != nil {
		dst.Exts = src.Exts
		updates.Add(key+".exts", *src.Exts)
	}
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
	StorageBackend    *appconfig.StorageBackendDTO `json:"storageBackend"`
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
	Webhook           *appconfig.WebhookConfigDTO  `json:"webhook"`
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

	// 存储后端连接参数校验：切换后端后需重启服务，若参数不完整将导致下次启动失败，
	// 因此在保存前拦截，避免把不可用的配置写入文件。
	if present("storageBackend") && req.StorageBackend != nil {
		if msg := validateStorageBackend(req.StorageBackend); msg != "" {
			utils.HandleBadRequest(c, msg, nil)
			return
		}
	}

	// LDAP 连接参数校验：启用时缺地址或 Base DN 会导致认证不可用，保存前拦截
	if present("auth") {
		if msg := validateLDAPConfig(req.Auth.LDAP); msg != "" {
			utils.HandleBadRequest(c, msg, nil)
			return
		}
	}

	// 验证码模式白名单校验：非法值会导致安全功能静默失效，保存前拦截并归一化。
	// 必须在加写锁前 return，避免锁未释放。
	if present("account") && req.Account.Captcha.Mode != "" {
		mode := strings.ToLower(strings.TrimSpace(req.Account.Captcha.Mode))
		switch mode {
		case appconfig.CaptchaModeDisabled, appconfig.CaptchaModeAlways:
		case appconfig.CaptchaModeAfterN, "aftern":
			mode = appconfig.CaptchaModeAfterN
		default:
			utils.HandleBadRequest(c, "验证码模式无效，仅支持 disabled / always / after_n", nil)
			return
		}
		req.Account.Captcha.Mode = mode
	}

	// 加写锁保护配置并发修改。
	// 注意：RunOnSaveHooks 内部的钩子会通过 GetConfig 申请读锁，
	// 绝不能持写锁跨钩子执行，否则同 goroutine RWMutex 自死锁。
	appconfig.LockConfig()

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
		// 注册开关：account 节提交即写入（bool 无"未设置"语义，关闭注册是合法目标状态）
		appconfig.GlobalConfig.Account.AllowRegistration = req.Account.AllowRegistration
		updates.Add("account.allow_registration", req.Account.AllowRegistration)
		// 强制登录开关：开启后 Web 界面仅登录用户可交互
		appconfig.GlobalConfig.Account.RequireLogin = req.Account.RequireLogin
		updates.Add("account.require_login", req.Account.RequireLogin)
		// 图形验证码：mode 非空才写入，避免前端未提交 captcha 时误清空已有配置；
		// trigger_after 仅接受正值，避免把阈值重置为 0/负数（<=0 运行时按默认值处理）
		if req.Account.Captcha.Mode != "" {
			appconfig.GlobalConfig.Account.Captcha.Mode = req.Account.Captcha.Mode
			updates.Add("account.captcha.mode", req.Account.Captcha.Mode)
		}
		if req.Account.Captcha.TriggerAfter > 0 {
			appconfig.GlobalConfig.Account.Captcha.TriggerAfter = req.Account.Captcha.TriggerAfter
			updates.Add("account.captcha.trigger_after", req.Account.Captcha.TriggerAfter)
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

	// —— 存储后端（服务启动时初始化，保存后需重启服务生效）——
	if present("storageBackend") && req.StorageBackend != nil {
		sb := req.StorageBackend
		backend := strings.TrimSpace(sb.Backend)
		if backend == "" {
			backend = appconfig.StorageBackendLocal
		}
		appconfig.GlobalConfig.Storage.Backend = backend
		updates.Add("storage.backend", backend)

		appconfig.GlobalConfig.Storage.S3 = appconfig.S3StorageConfig{
			Endpoint:  strings.TrimSpace(sb.S3.Endpoint),
			AccessKey: strings.TrimSpace(sb.S3.AccessKey),
			SecretKey: sb.S3.SecretKey,
			Bucket:    strings.TrimSpace(sb.S3.Bucket),
			Region:    strings.TrimSpace(sb.S3.Region),
			UseSSL:    sb.S3.UseSSL,
		}
		updates.Add("storage.s3.endpoint", appconfig.GlobalConfig.Storage.S3.Endpoint)
		updates.Add("storage.s3.access_key", appconfig.GlobalConfig.Storage.S3.AccessKey)
		updates.Add("storage.s3.secret_key", appconfig.GlobalConfig.Storage.S3.SecretKey)
		updates.Add("storage.s3.bucket", appconfig.GlobalConfig.Storage.S3.Bucket)
		updates.Add("storage.s3.region", appconfig.GlobalConfig.Storage.S3.Region)
		updates.Add("storage.s3.use_ssl", appconfig.GlobalConfig.Storage.S3.UseSSL)

		appconfig.GlobalConfig.Storage.OSS = appconfig.OSSStorageConfig{
			Endpoint:  strings.TrimSpace(sb.OSS.Endpoint),
			AccessKey: strings.TrimSpace(sb.OSS.AccessKey),
			SecretKey: sb.OSS.SecretKey,
			Bucket:    strings.TrimSpace(sb.OSS.Bucket),
		}
		updates.Add("storage.oss.endpoint", appconfig.GlobalConfig.Storage.OSS.Endpoint)
		updates.Add("storage.oss.access_key", appconfig.GlobalConfig.Storage.OSS.AccessKey)
		updates.Add("storage.oss.secret_key", appconfig.GlobalConfig.Storage.OSS.SecretKey)
		updates.Add("storage.oss.bucket", appconfig.GlobalConfig.Storage.OSS.Bucket)

		appconfig.GlobalConfig.Storage.WebDAV = appconfig.WebDAVStorageConfig{
			URL:      strings.TrimSpace(sb.WebDAV.URL),
			Username: sb.WebDAV.Username,
			Password: sb.WebDAV.Password,
		}
		updates.Add("storage.webdav.url", appconfig.GlobalConfig.Storage.WebDAV.URL)
		updates.Add("storage.webdav.username", appconfig.GlobalConfig.Storage.WebDAV.Username)
		updates.Add("storage.webdav.password", appconfig.GlobalConfig.Storage.WebDAV.Password)

		// —— 存储告警（内存配置即时生效，无需重启）——
		// 仅在请求显式包含 storageBackend.alert 时更新，避免旧客户端只提交
		// 存储后端字段时把告警配置重置为默认值。
		if rawSB, ok := raw["storageBackend"]; ok {
			var sbFields map[string]json.RawMessage
			if json.Unmarshal(rawSB, &sbFields) == nil {
				if _, hasAlert := sbFields["alert"]; hasAlert {
					alert := appconfig.NormalizeStorageAlertConfig(appconfig.StorageAlertConfig{
						Enabled:              sb.Alert.Enabled,
						ThresholdPercent:     sb.Alert.ThresholdPercent,
						CheckIntervalMinutes: sb.Alert.CheckIntervalMinutes,
						CooldownMinutes:      sb.Alert.CooldownMinutes,
						WebEnabled:           sb.Alert.WebEnabled,
						WebhookEnabled:       sb.Alert.WebhookEnabled,
						EmailEnabled:         sb.Alert.EmailEnabled,
						EmailRecipients:      sb.Alert.EmailRecipients,
						SMTP: appconfig.SMTPConfig{
							Host:     sb.Alert.SMTP.Host,
							Port:     sb.Alert.SMTP.Port,
							Username: sb.Alert.SMTP.Username,
							Password: sb.Alert.SMTP.Password,
							From:     sb.Alert.SMTP.From,
							TLSMode:  sb.Alert.SMTP.TLSMode,
						},
					})
					appconfig.GlobalConfig.Storage.Alert = alert
					updates.Add("storage.alert.enabled", alert.Enabled)
					updates.Add("storage.alert.threshold_percent", alert.ThresholdPercent)
					updates.Add("storage.alert.check_interval_minutes", alert.CheckIntervalMinutes)
					updates.Add("storage.alert.cooldown_minutes", alert.CooldownMinutes)
					updates.Add("storage.alert.web_enabled", alert.WebEnabled)
					updates.Add("storage.alert.webhook_enabled", alert.WebhookEnabled)
					updates.Add("storage.alert.email_enabled", alert.EmailEnabled)
					updates.Add("storage.alert.email_recipients", alert.EmailRecipients)
					updates.Add("storage.alert.smtp.host", alert.SMTP.Host)
					updates.Add("storage.alert.smtp.port", alert.SMTP.Port)
					updates.Add("storage.alert.smtp.username", alert.SMTP.Username)
					updates.Add("storage.alert.smtp.password", alert.SMTP.Password)
					updates.Add("storage.alert.smtp.from", alert.SMTP.From)
					updates.Add("storage.alert.smtp.tls_mode", alert.SMTP.TLSMode)
				}
			}
		}
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
		maxConcurrent := req.Upload.URLUpload.MaxConcurrent
		if maxConcurrent <= 0 {
			maxConcurrent = appconfig.DefaultURLUploadMaxConcurrent
		}
		appconfig.GlobalConfig.Upload.URLUpload.MaxConcurrent = maxConcurrent
		updates.Add("upload.url_upload.max_concurrent", maxConcurrent)
		maxRetries := req.Upload.URLUpload.MaxRetries
		if maxRetries <= 0 {
			maxRetries = appconfig.DefaultURLUploadMaxRetries
		}
		appconfig.GlobalConfig.Upload.URLUpload.MaxRetries = maxRetries
		updates.Add("upload.url_upload.max_retries", maxRetries)
		// 服务器侧大小硬上限（字节），0 表示不限制；负数按 0 处理
		maxSize := req.Upload.URLUpload.MaxSize
		if maxSize < 0 {
			maxSize = 0
		}
		appconfig.GlobalConfig.Upload.URLUpload.MaxSize = maxSize
		updates.Add("upload.url_upload.max_size", maxSize)
		// 上传频率限制（max_requests=0 表示不限制）
		appconfig.GlobalConfig.Upload.RateLimit.WindowMinutes = req.Upload.RateLimit.WindowMinutes
		appconfig.GlobalConfig.Upload.RateLimit.MaxRequests = req.Upload.RateLimit.MaxRequests
		updates.Add("upload.rate_limit.window_minutes", req.Upload.RateLimit.WindowMinutes)
		updates.Add("upload.rate_limit.max_requests", req.Upload.RateLimit.MaxRequests)
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

	// —— 预览（分组配置：未提交的指针字段沿用当前生效值，显式提交按提交值生效）——
	if present("preview") {
		pv := &appconfig.GlobalConfig.Preview
		if req.Preview.Enabled != nil {
			pv.Enabled = req.Preview.Enabled
			updates.Add("preview.enabled", *req.Preview.Enabled)
		}
		applyPreviewGroup(&pv.Groups.Image, req.Preview.Groups.Image, "preview.groups.image", updates)
		applyPreviewGroup(&pv.Groups.Audio, req.Preview.Groups.Audio, "preview.groups.audio", updates)
		applyPreviewGroup(&pv.Groups.Video, req.Preview.Groups.Video, "preview.groups.video", updates)
		applyPreviewGroup(&pv.Groups.Document, req.Preview.Groups.Document, "preview.groups.document", updates)
		applyPreviewGroup(&pv.Groups.Custom, req.Preview.Groups.Custom, "preview.groups.custom", updates)
		// 文本分组额外携带文本预览大小配置
		td := req.Preview.Groups.Text
		tv := &pv.Groups.Text
		if td.Enabled != nil {
			tv.Enabled = td.Enabled
			updates.Add("preview.groups.text.enabled", *td.Enabled)
		}
		if td.Mimes != nil {
			tv.Mimes = td.Mimes
			updates.Add("preview.groups.text.mimes", *td.Mimes)
		}
		if td.Exts != nil {
			tv.Exts = td.Exts
			updates.Add("preview.groups.text.exts", *td.Exts)
		}
		if td.MaxInlineSize != "" {
			tv.MaxInlineSize = td.MaxInlineSize
			updates.Add("preview.groups.text.max_inline_size", td.MaxInlineSize)
		}
		if td.TextChunkSize != "" {
			tv.TextChunkSize = td.TextChunkSize
			updates.Add("preview.groups.text.text_chunk_size", td.TextChunkSize)
		}
	}

	// —— 用户自定义 MIME 表（扩展名 -> MIME 映射）——
	if present("mimeTable") {
		appconfig.GlobalConfig.MimeTable = req.MimeTable
		updates.Add("mime_table", req.MimeTable)
	}

	// —— OpenAPI ——
	if req.OpenApi != nil && present("openApi") {
		// 鉴权方式白名单校验：非法值会让 Open API 中间件直接返回 500，保存前拦截。
		// 未提交（空字符串）时沿用原值，避免设置页的局部保存把已配置的方式重置。
		if mode := strings.TrimSpace(req.OpenApi.AuthMode); mode != "" {
			if mode != "none" && mode != "api_key" {
				utils.HandleBadRequest(c, "Open API 鉴权方式无效，仅支持 none / api_key", nil)
				return
			}
			appconfig.GlobalConfig.OpenAPI.AuthMode = mode
			updates.Add("open_api.auth_mode", mode)
		}
		appconfig.GlobalConfig.OpenAPI.Enabled = req.OpenApi.Enabled
		appconfig.GlobalConfig.OpenAPI.IPAccessMode = req.OpenApi.IpAccessMode
		appconfig.GlobalConfig.OpenAPI.IPWhitelist = req.OpenApi.IpWhitelist
		appconfig.GlobalConfig.OpenAPI.IPBlacklist = req.OpenApi.IpBlacklist
		appconfig.GlobalConfig.OpenAPI.DomainWhitelist = req.OpenApi.DomainWhitelist
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
		domainWhitelist := make([]interface{}, len(req.OpenApi.DomainWhitelist))
		for i, d := range req.OpenApi.DomainWhitelist {
			domainWhitelist[i] = d
		}
		updates.Add("open_api.domain_whitelist", domainWhitelist)
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

	// —— Webhook 推送配置 ——
	if req.Webhook != nil && present("webhook") {
		appconfig.GlobalConfig.Webhook.Enabled = req.Webhook.Enabled
		appconfig.GlobalConfig.Webhook.Secret = req.Webhook.Secret
		appconfig.GlobalConfig.Webhook.TimeoutSeconds = req.Webhook.TimeoutSeconds
		appconfig.GlobalConfig.Webhook.MaxRetries = req.Webhook.MaxRetries
		appconfig.GlobalConfig.Webhook.QueueSize = req.Webhook.QueueSize
		updates.Add("webhook.enabled", req.Webhook.Enabled)
		updates.Add("webhook.secret", req.Webhook.Secret)
		updates.Add("webhook.timeout_seconds", req.Webhook.TimeoutSeconds)
		updates.Add("webhook.max_retries", req.Webhook.MaxRetries)
		updates.Add("webhook.queue_size", req.Webhook.QueueSize)

		// 端点：写入 NewEndpoints（nil 表示未改变，不产生更新）；非 nil 时完成旧 urls → endpoints 迁移
		newEndpoints := appconfig.WebhookConfig{Endpoints: endpointsFromDTO(req.Webhook.Endpoints)}.Endpoints
		appconfig.GlobalConfig.Webhook.Endpoints = newEndpoints
		if req.Webhook.Endpoints != nil {
			appconfig.GlobalConfig.Webhook.URLs = nil
			updates.Add("webhook.urls", []interface{}{})
			epList := make([]interface{}, len(newEndpoints))
			for i, ep := range newEndpoints {
				m := map[string]interface{}{
					"name":            ep.Name,
					"url":             ep.URL,
					"secret":          ep.Secret,
					"timeout_seconds": ep.TimeoutSeconds,
					"max_retries":     ep.MaxRetries,
					"events":          stringListToInterface(ep.Events),
					"headers":         stringMapToInterface(ep.Headers),
					"query":           stringMapToInterface(ep.Query),
				}
				// 仅显式设置时才写入 enabled（nil 视为启用，写入 null 无意义）
				if ep.Enabled != nil {
					m["enabled"] = *ep.Enabled
				}
				epList[i] = m
			}
			updates.Add("webhook.endpoints", epList)
		}
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
		appconfig.UnlockConfig()
		response.HandleErrorCompat(c, http.StatusInternalServerError, "保存配置失败: "+err.Error(), nil)
		return
	}
	// 配置变更已全部落定，先释放写锁再执行热更新钩子
	appconfig.UnlockConfig()

	utils.InitPreviewConfig(appconfig.GlobalConfig.Preview.InitConfig())
	utils.InitMimeTable(appconfig.GlobalConfig.MimeTable)
	appconfig.RunOnSaveHooks()

	// 记录配置修改操作，便于审计追踪（关键操作需留痕）
	middleware.LogOperation(c, "config.update", "global_config", nil)
	response.HandleSuccess(c, http.StatusOK, "配置已保存并生效", nil)
}

// validateLDAPConfig 校验 LDAP 配置：启用时必须提供服务器地址与 Base DN，端口需在合法范围。
// 端口为 0 表示使用协议默认值（LDAP 389 / LDAPS 636）。
func validateLDAPConfig(dto appconfig.LDAPConfigDTO) string {
	if dto.Port < 0 || dto.Port > 65535 {
		return "LDAP 端口必须在 1-65535 之间"
	}
	if !dto.Enabled {
		return ""
	}
	if strings.TrimSpace(dto.Host) == "" {
		return "启用 LDAP 时必须填写服务器地址"
	}
	if strings.TrimSpace(dto.BaseDN) == "" {
		return "启用 LDAP 时必须填写 Base DN"
	}
	return ""
}

// validateStorageBackend 校验存储后端类型与必填连接参数。
// 返回空字符串表示通过；否则返回给用户的中文错误信息。
func validateStorageBackend(sb *appconfig.StorageBackendDTO) string {
	backend := strings.TrimSpace(sb.Backend)
	if backend == "" {
		backend = appconfig.StorageBackendLocal
	}
	switch backend {
	case appconfig.StorageBackendLocal:
		return ""
	case appconfig.StorageBackendS3, appconfig.StorageBackendMinIO:
		if strings.TrimSpace(sb.S3.Endpoint) == "" || strings.TrimSpace(sb.S3.Bucket) == "" ||
			strings.TrimSpace(sb.S3.AccessKey) == "" || sb.S3.SecretKey == "" {
			return "S3/MinIO 存储需填写端点、Bucket、AccessKey 与 SecretKey"
		}
	case appconfig.StorageBackendOSS:
		if strings.TrimSpace(sb.OSS.Endpoint) == "" || strings.TrimSpace(sb.OSS.Bucket) == "" ||
			strings.TrimSpace(sb.OSS.AccessKey) == "" || sb.OSS.SecretKey == "" {
			return "OSS 存储需填写端点、Bucket、AccessKey 与 SecretKey"
		}
	case appconfig.StorageBackendWebDAV:
		if strings.TrimSpace(sb.WebDAV.URL) == "" {
			return "WebDAV 存储需填写服务地址"
		}
	default:
		return "不支持的存储后端类型: " + backend
	}
	return ""
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

// TestWebhookConfig 测试 Webhook 回调连通性：向当前（或 body 中临时）端点逐条发送测试事件。
// 可选 JSON body（WebhookConfigDTO）用于测试未保存的表单值；body 为空时使用当前已保存的配置。
// body 内 endpoints 仅含单条时，则只测试该条（前端「单条测试」）。
func (h *Handler) TestWebhookConfig(c *gin.Context) {
	cfg := appconfig.WebhookConfig{
		Enabled:        appconfig.GetConfig().Webhook.Enabled,
		Endpoints:      appconfig.GetConfig().Webhook.Endpoints,
		URLs:           appconfig.GetConfig().Webhook.URLs,
		Secret:         appconfig.GetConfig().Webhook.Secret,
		TimeoutSeconds: appconfig.GetConfig().Webhook.TimeoutSeconds,
		MaxRetries:     appconfig.GetConfig().Webhook.MaxRetries,
		QueueSize:      appconfig.GetConfig().Webhook.QueueSize,
	}
	body, _ := io.ReadAll(c.Request.Body)
	if len(strings.TrimSpace(string(body))) > 0 {
		var dto appconfig.WebhookConfigDTO
		if err := json.Unmarshal(body, &dto); err != nil {
			utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
			return
		}
		cfg = webhookConfigFromDTO(dto)
	}
	results := webhook.TestPush(cfg)
	response.HandleSuccess(c, http.StatusOK, "Webhook 测试完成", results)
}

// ListWebhookEvents 返回 Webhook 事件目录（前端动态渲染事件选项，避免与后端列表漂移）。
func (h *Handler) ListWebhookEvents(c *gin.Context) {
	response.HandleSuccess(c, http.StatusOK, "", webhook.EventCatalog())
}

// webhookConfigFromDTO 将 Webhook 配置 DTO 转为内部配置（测试未保存表单值时使用）。
// 未显式提供的全局字段使用当前已保存值补齐，保证单条端点测试也能正常工作。
func webhookConfigFromDTO(dto appconfig.WebhookConfigDTO) appconfig.WebhookConfig {
	saved := appconfig.GetConfig().Webhook
	cfg := appconfig.WebhookConfig{
		Enabled:        dto.Enabled,
		Endpoints:      endpointsFromDTO(dto.Endpoints),
		Secret:         dto.Secret,
		TimeoutSeconds: dto.TimeoutSeconds,
		MaxRetries:     dto.MaxRetries,
		QueueSize:      dto.QueueSize,
	}
	if cfg.Secret == "" {
		cfg.Secret = saved.Secret
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = saved.TimeoutSeconds
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = saved.MaxRetries
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = saved.QueueSize
	}
	return cfg
}

// endpointsFromDTO 将 DTO 端点列表转为内部端点列表（nil 保持 nil，空列表保持空）。
func endpointsFromDTO(dtos []appconfig.WebhookEndpointDTO) []appconfig.WebhookEndpoint {
	if dtos == nil {
		return nil
	}
	out := make([]appconfig.WebhookEndpoint, len(dtos))
	for i, d := range dtos {
		out[i] = appconfig.EndpointFromDTO(d)
	}
	return out
}

// stringListToInterface 将 []string 转为便于 YAML 序列化的 []interface{}。
func stringListToInterface(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// stringMapToInterface 将 map[string]string 转为便于 YAML 序列化的 map[string]interface{}。
func stringMapToInterface(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
