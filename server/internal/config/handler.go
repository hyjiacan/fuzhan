// Package config 处理系统配置
package config

import (
    "fmt"
    "net/http"
    "os"
    "path/filepath"

    "github.com/gin-gonic/gin"
    appconfig "fuzhan/internal/appconfig"
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
    Temp              TempConfig        `json:"temp"`
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

type TempConfig struct {
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
type SaveConfigRequest struct {
    App               AppConfig            `json:"app"`
    Account           AccountResponse      `json:"account"`
    Server            ServerResponse       `json:"server"`
    Database          DatabaseConfig       `json:"database"`
    RootDirs          []RootDirInput       `json:"rootDirs"`
    AllowedExtensions []string             `json:"allowedExtensions"`
    Temp              TempConfig           `json:"temp"`
    TempFiles         TempFilesConfig      `json:"tempFiles"`
    Upload            UploadConfig         `json:"upload"`
    Preview           PreviewConfig        `json:"preview"`
    OpenApi           *OpenApiConfig       `json:"openApi"`
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
    FullPath string `json:"fullPath"`
}

func (h *Handler) SaveConfig(c *gin.Context) {
    var req SaveConfigRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
        return
    }

    // 加写锁保护配置并发修改
    appconfig.LockConfig()
    defer appconfig.UnlockConfig()

    appconfig.GlobalConfig.App.Name = req.App.Name
    appconfig.GlobalConfig.Server.Host = req.Server.Host
    appconfig.GlobalConfig.Server.HTTP.Enabled = req.Server.HTTP.Enabled
    appconfig.GlobalConfig.Server.HTTP.Port = req.Server.HTTP.Port
    appconfig.GlobalConfig.Server.HTTPS.Enabled = req.Server.HTTPS.Enabled
    appconfig.GlobalConfig.Server.HTTPS.Port = req.Server.HTTPS.Port
    appconfig.GlobalConfig.Server.FTP.Enabled = req.Server.FTP.Enabled
    appconfig.GlobalConfig.Server.FTP.Port = req.Server.FTP.Port
    appconfig.GlobalConfig.Server.FTPS.Enabled = req.Server.FTPS.Enabled
    appconfig.GlobalConfig.Server.FTPS.Port = req.Server.FTPS.Port
    appconfig.GlobalConfig.Server.WebDAV.Enabled = req.Server.WebDAV.Enabled
    appconfig.GlobalConfig.Server.WebDAV.Port = req.Server.WebDAV.Port
    appconfig.GlobalConfig.Account.Anonymous.Username = req.Account.Anonymous.Username
    appconfig.GlobalConfig.Account.ReservedUsernames = req.Account.ReservedUsernames
    appconfig.GlobalConfig.Database.Driver = req.Database.Driver
    appconfig.GlobalConfig.Database.DSN = req.Database.DSN

    if len(req.RootDirs) > 0 {
        dirs := make([]appconfig.DirectoryConfig, len(req.RootDirs))
        for i, dir := range req.RootDirs {
            dirs[i] = appconfig.DirectoryConfig{Path: dir.FullPath}
        }
        appconfig.GlobalConfig.Storage.Public.RootDirs = dirs
    }
    appconfig.GlobalConfig.Storage.AllowedExtensions = req.AllowedExtensions
    appconfig.GlobalConfig.Storage.Private.Enabled = req.Temp.Enabled
    appconfig.GlobalConfig.Storage.Private.Path = req.Temp.Path
    appconfig.GlobalConfig.Storage.Private.Quota.GlobalQuota = req.Temp.QuotaGlobal
    appconfig.GlobalConfig.Storage.Private.Quota.PerUserQuota = req.Temp.QuotaPerUser
    appconfig.GlobalConfig.Storage.Temp.Enabled = req.TempFiles.Enabled
    appconfig.GlobalConfig.Storage.Temp.Path = req.TempFiles.Path
    appconfig.GlobalConfig.Storage.Temp.Quota.Global = formatSizeToString(req.TempFiles.QuotaGlobal)
    appconfig.GlobalConfig.Storage.Temp.Quota.PerIP = formatSizeToString(req.TempFiles.QuotaPerIP)
    appconfig.GlobalConfig.Storage.Temp.DefaultExpireDays = req.TempFiles.DefaultExpireDays
    appconfig.GlobalConfig.Storage.Temp.DeleteOnDownload = req.TempFiles.DeleteOnDownload
    appconfig.GlobalConfig.Upload.ChunkSize = req.Upload.ChunkSize
    appconfig.GlobalConfig.Upload.MaxFileSize = req.Upload.MaxFileSize
    appconfig.GlobalConfig.Upload.URLUpload.Enabled = req.Upload.URLUpload.Enabled
    appconfig.GlobalConfig.Upload.URLUpload.InsecureSkipVerify = req.Upload.URLUpload.InsecureSkipVerify
    appconfig.GlobalConfig.Preview.AllowMimes = req.Preview.AllowMimes
    appconfig.GlobalConfig.Preview.AllowExts = req.Preview.AllowExts
    appconfig.GlobalConfig.Preview.MaxInlineSize = req.Preview.MaxInlineSize
    appconfig.GlobalConfig.Preview.TextChunkSize = req.Preview.TextChunkSize

    if req.OpenApi != nil {
        appconfig.GlobalConfig.OpenAPI.Enabled = req.OpenApi.Enabled
        appconfig.GlobalConfig.OpenAPI.IPAccessMode = req.OpenApi.IpAccessMode
        appconfig.GlobalConfig.OpenAPI.IPWhitelist = req.OpenApi.IpWhitelist
        appconfig.GlobalConfig.OpenAPI.IPBlacklist = req.OpenApi.IpBlacklist
        appconfig.GlobalConfig.OpenAPI.RateLimitEnabled = req.OpenApi.RateLimitEnabled
        if req.OpenApi.RequestsPerMinute > 0 {
            appconfig.GlobalConfig.OpenAPI.RequestsPerMinute = req.OpenApi.RequestsPerMinute
        }
    }

    // 索引配置
	appconfig.GlobalConfig.Index.ScanCronExpression = req.Index.ScanCronExpression

    if len(req.RootDirs) > 0 {
        newRootNames := map[string]string{}
        for _, dir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
            dirName := filepath.Base(dir.Path)
            if dirName != "" {
                newRootNames[dirName] = dir.Path
            }
        }
        appconfig.RootNames = newRootNames
    }

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

    configPathFile := "fuzhan.yaml"
    updates := appconfig.NewConfigUpdates()
    if req.App.Name != "" {
        updates.Add("app.name", req.App.Name)
    }
    updates.Add("server.host", req.Server.Host)
    updates.Add("server.http.enabled", req.Server.HTTP.Enabled)
    updates.Add("server.http.port", req.Server.HTTP.Port)
    updates.Add("server.https.enabled", req.Server.HTTPS.Enabled)
    updates.Add("server.https.port", req.Server.HTTPS.Port)
    updates.Add("server.ftp.enabled", req.Server.FTP.Enabled)
    updates.Add("server.ftp.port", req.Server.FTP.Port)
    updates.Add("server.ftps.enabled", req.Server.FTPS.Enabled)
    updates.Add("server.ftps.port", req.Server.FTPS.Port)
    updates.Add("server.webdav.enabled", req.Server.WebDAV.Enabled)
    updates.Add("server.webdav.port", req.Server.WebDAV.Port)
    updates.Add("account.anonymous.username", req.Account.Anonymous.Username)
    reservedList := make([]interface{}, len(req.Account.ReservedUsernames))
    for i, u := range req.Account.ReservedUsernames {
        reservedList[i] = u
    }
    updates.Add("account.reserved_usernames", reservedList)
    updates.Add("database.driver", req.Database.Driver)
    updates.Add("database.dsn", req.Database.DSN)

    if len(req.RootDirs) > 0 {
        dirs := make([]interface{}, len(req.RootDirs))
        for i, dir := range req.RootDirs {
            dirs[i] = map[string]interface{}{"path": dir.FullPath, "quota": "0"}
        }
        updates.Add("storage.public.root_dirs", dirs)
    }
    if len(req.AllowedExtensions) > 0 {
        extList := make([]interface{}, len(req.AllowedExtensions))
        for i, ext := range req.AllowedExtensions {
            extList[i] = ext
        }
        updates.Add("storage.allowed_extensions", extList)
    }
    updates.Add("storage.private.enabled", req.Temp.Enabled)
    updates.Add("storage.private.path", req.Temp.Path)
    updates.Add("storage.private.quota.global", formatSizeToString(req.Temp.QuotaGlobal))
    updates.Add("storage.private.quota.per_user", formatSizeToString(req.Temp.QuotaPerUser))
    updates.Add("storage.temp.enabled", req.TempFiles.Enabled)
    updates.Add("storage.temp.path", req.TempFiles.Path)
    updates.Add("storage.temp.quota.global", formatSizeToString(req.TempFiles.QuotaGlobal))
    updates.Add("storage.temp.quota.per_ip", formatSizeToString(req.TempFiles.QuotaPerIP))
    updates.Add("storage.temp.default_expire_days", req.TempFiles.DefaultExpireDays)
    updates.Add("storage.temp.delete_on_download", req.TempFiles.DeleteOnDownload)
    updates.Add("upload.chunk_size", formatSizeToString(req.Upload.ChunkSize))
    updates.Add("upload.max_file_size", formatSizeToString(req.Upload.MaxFileSize))
    updates.Add("upload.url_upload.enabled", req.Upload.URLUpload.Enabled)
    updates.Add("upload.url_upload.insecure_skip_verify", req.Upload.URLUpload.InsecureSkipVerify)
    updates.Add("preview.allow_mimes", req.Preview.AllowMimes)
    updates.Add("preview.allow_exts", req.Preview.AllowExts)
    updates.Add("preview.max_inline_size", req.Preview.MaxInlineSize)
    updates.Add("preview.text_chunk_size", req.Preview.TextChunkSize)

    if req.OpenApi != nil {
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
            updates.Add("open_api.requests_per_minute", req.OpenApi.RequestsPerMinute)
        }
    }

    // 索引配置更新
	updates.Add("index.scan_cron_expression", req.Index.ScanCronExpression)

    if err := updates.Apply(configPathFile); err != nil {
        response.HandleErrorCompat(c, http.StatusInternalServerError, "保存配置失败: "+err.Error(), nil)
        return
    }

    utils.InitPreviewConfig(appconfig.GlobalConfig.Preview.AllowMimes, appconfig.GlobalConfig.Preview.AllowExts)
    appconfig.RunOnSaveHooks()
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
