// Package config 处理系统配置
package config

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"

    "github.com/gin-gonic/gin"
    appconfig "fuzhan/internal/appconfig"
    "fuzhan/internal/middleware"
    "fuzhan/pkg/response"
    "fuzhan/internal/utils"
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
            "port":           currentConfig.Server.WebDAV.Port,
            "publicUsername": currentConfig.Account.Anonymous.Username,
        },
        "upload": gin.H{
            "chunkSize":   currentConfig.Upload.ChunkSize,
            "maxFileSize": currentConfig.Upload.MaxFileSize,
            "urlUpload": gin.H{
                "enabled": currentConfig.Upload.URLUpload.Enabled,
            },
        },
        "preview": gin.H{
            "allowMimes": currentConfig.Preview.AllowMimes,
            "allowExts":  currentConfig.Preview.AllowExts,
        },
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

// ConfigResponse 配置响应结构
type ConfigResponse struct {
    App               AppConfig         `json:"app"`
    Account           AccountResponse   `json:"account"`
    Server            ServerResponse    `json:"server"`
    Database          DatabaseConfig    `json:"database"`
    RootDirs          []RootDirConfig   `json:"rootDirs"`
    AllowedExtensions []string          `json:"allowedExtensions"`
    TempFiles         TempFilesConfig   `json:"tempFiles"`
    Upload            UploadConfig      `json:"upload"`
    Preview           PreviewConfig     `json:"preview"`
}

type AccountResponse struct {
    Anonymous         AnonymousResponse `json:"anonymous"`
    ReservedUsernames []string           `json:"reservedUsernames"`
}

type AnonymousResponse struct {
    Username string `json:"username"`
}

type AppConfig struct {
    Name        string `json:"name"`
    Initialized bool   `json:"initialized"`
}

type ServerResponse struct {
    Host   string           `json:"host"`
    HTTP   HTTPConfigResp   `json:"http"`
    HTTPS  HTTPConfigResp   `json:"https"`
    FTP    FTPConfigResp    `json:"ftp"`
    FTPS   FTPSConfigResp   `json:"ftps"`
    WebDAV WebDAVConfigResp `json:"webdav"`
}

type HTTPConfigResp struct {
    Enabled bool `json:"enabled"`
    Port    int  `json:"port"`
}

type FTPConfigResp struct {
    Enabled bool `json:"enabled"`
    Port    int  `json:"port"`
}

type FTPSConfigResp struct {
    Enabled bool `json:"enabled"`
    Port    int  `json:"port"`
}

type WebDAVConfigResp struct {
    Enabled bool `json:"enabled"`
    Port    int  `json:"port"`
}

type DatabaseConfig struct {
    Driver string `json:"driver"`
    DSN    string `json:"dsn"`
}

type RootDirConfig struct {
    Name     string `json:"name"`
    Path     string `json:"path"`
    FullPath string `json:"fullPath"`
}

type PrivateFilesConfig struct {
    Enabled      bool   `json:"enabled"`
    Path         string `json:"path"`
    QuotaGlobal  int64  `json:"quotaGlobal"`
    QuotaPerUser int64  `json:"quotaPerUser"`
}

type TempFilesConfig struct {
    Enabled           bool   `json:"enabled"`
    Path              string `json:"path"`
    QuotaGlobal       int64  `json:"quotaGlobal"`
    QuotaPerIP        int64  `json:"quotaPerIP"`
    DefaultExpireDays int    `json:"defaultExpireDays"`
    DeleteOnDownload  bool   `json:"deleteOnDownload"`
}

type UploadConfig struct {
    ChunkSize   int64            `json:"chunkSize"`
    MaxFileSize int64            `json:"maxFileSize"`
    URLUpload   URLUploadConfigResp `json:"urlUpload"`
}

type URLUploadConfigResp struct {
    Enabled            bool `json:"enabled"`
    InsecureSkipVerify bool `json:"insecureSkipVerify"`
}

type PreviewConfig struct {
    AllowMimes    string `json:"allowMimes"`
    AllowExts     string `json:"allowExts"`
    MaxInlineSize string `json:"maxInlineSize"`
    TextChunkSize string `json:"textChunkSize"`
}

// GetConfig 获取当前配置
func (h *Handler) GetConfig(c *gin.Context) {
    resp := appconfig.GlobalConfig.ToDTO()
    response.HandleSuccess(c, http.StatusOK, "", resp)
}

// SaveConfigRequest 保存配置请求
// 字段名与前端设置页提交内容严格一致，避免保存时覆盖未提交的配置项。
type SaveConfigRequest struct {
    App               AppConfig          `json:"app"`
    Account           AccountResponse    `json:"account"`
    Server            ServerResponse     `json:"server"`
    Database          DatabaseConfig     `json:"database"`
    RootDirs          []RootDirInput     `json:"rootDirs"`
    AllowedExtensions []string           `json:"allowedExtensions"`
    PrivateFiles      PrivateFilesConfig `json:"privateFiles"`
    TempFiles         TempFilesConfig    `json:"tempFiles"`
    Upload            UploadConfig       `json:"upload"`
    Preview           PreviewConfig      `json:"preview"`
    OpenApi           *OpenApiConfig     `json:"openApi"`
    Index             appconfig.IndexConfigDTO `json:"index"`
}

type OpenApiConfig struct {
    Enabled            bool     `json:"enabled"`
    IpAccessMode       string   `json:"ipAccessMode"`
    IpWhitelist        []string `json:"ipWhitelist"`
    IpBlacklist        []string `json:"ipBlacklist"`
    RateLimitEnabled   bool     `json:"rateLimitEnabled"`
    RequestsPerMinute  int      `json:"requestsPerMinute"`
}

type RootDirInput struct {
    Name     string `json:"name"`
    Path     string `json:"path"`
    FullPath string `json:"fullPath"`
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
        // WebDAV 端口未被前端管理，绝不在未提供时覆盖既有值(0 表示与 HTTP/HTTPS 同端口)
        if req.Server.WebDAV.Enabled || req.Server.WebDAV.Port != 0 {
            appconfig.GlobalConfig.Server.WebDAV.Enabled = req.Server.WebDAV.Enabled
            updates.Add("server.webdav.enabled", req.Server.WebDAV.Enabled)
        }
        if req.Server.WebDAV.Port > 0 {
            appconfig.GlobalConfig.Server.WebDAV.Port = req.Server.WebDAV.Port
            updates.Add("server.webdav.port", req.Server.WebDAV.Port)
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
            dirs[i] = appconfig.DirectoryConfig{Path: p, Quota: quota}
            fileDirs[i] = map[string]interface{}{"path": p, "quota": quota}
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
        updates.Add("storage.private.quota.global", formatSizeToString(req.PrivateFiles.QuotaGlobal))
        updates.Add("storage.private.quota.per_user", formatSizeToString(req.PrivateFiles.QuotaPerUser))
    }

    // —— 临时文件（前端字段名 tempFiles）——
    if present("tempFiles") {
        appconfig.GlobalConfig.Storage.Temp.Enabled = req.TempFiles.Enabled
        appconfig.GlobalConfig.Storage.Temp.Path = req.TempFiles.Path
        appconfig.GlobalConfig.Storage.Temp.Quota.Global = formatSizeToString(req.TempFiles.QuotaGlobal)
        appconfig.GlobalConfig.Storage.Temp.Quota.PerIP = formatSizeToString(req.TempFiles.QuotaPerIP)
        appconfig.GlobalConfig.Storage.Temp.DefaultExpireDays = req.TempFiles.DefaultExpireDays
        appconfig.GlobalConfig.Storage.Temp.DeleteOnDownload = req.TempFiles.DeleteOnDownload
        updates.Add("storage.temp.enabled", req.TempFiles.Enabled)
        updates.Add("storage.temp.path", req.TempFiles.Path)
        updates.Add("storage.temp.quota.global", formatSizeToString(req.TempFiles.QuotaGlobal))
        updates.Add("storage.temp.quota.per_ip", formatSizeToString(req.TempFiles.QuotaPerIP))
        updates.Add("storage.temp.default_expire_days", req.TempFiles.DefaultExpireDays)
        updates.Add("storage.temp.delete_on_download", req.TempFiles.DeleteOnDownload)
    }

    // —— 上传 ——
    if present("upload") {
        if req.Upload.ChunkSize > 0 {
            appconfig.GlobalConfig.Upload.ChunkSize = req.Upload.ChunkSize
            updates.Add("upload.chunk_size", formatSizeToString(req.Upload.ChunkSize))
        }
        appconfig.GlobalConfig.Upload.MaxFileSize = req.Upload.MaxFileSize
        updates.Add("upload.max_file_size", formatSizeToString(req.Upload.MaxFileSize))
        appconfig.GlobalConfig.Upload.URLUpload.Enabled = req.Upload.URLUpload.Enabled
        appconfig.GlobalConfig.Upload.URLUpload.InsecureSkipVerify = req.Upload.URLUpload.InsecureSkipVerify
        updates.Add("upload.url_upload.enabled", req.Upload.URLUpload.Enabled)
        updates.Add("upload.url_upload.insecure_skip_verify", req.Upload.URLUpload.InsecureSkipVerify)
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
    appconfig.RunOnSaveHooks()

    // 记录配置修改操作，便于审计追踪（关键操作需留痕）
    middleware.LogOperation(c, "config.update", "global_config", nil)
    response.HandleSuccess(c, http.StatusOK, "配置已保存并生效", nil)
}

func parseOrDefault(s string, defaultVal int64) int64 {
    if s == "" {
        return defaultVal
    }
    val, err := appconfig.ParseQuotaString(s)
    if err != nil {
        return defaultVal
    }
    return val
}

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
