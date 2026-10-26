package main

import (
	"net/http"
	"strings"

	"fuzhan/internal/admin"
	"fuzhan/internal/appconfig"
	configh "fuzhan/internal/appconfig/api"
	"fuzhan/internal/auth"
	"fuzhan/internal/behavioranalytics"
	"fuzhan/internal/cli"
	"fuzhan/internal/file"
	"fuzhan/internal/health"
	"fuzhan/internal/index"
	"fuzhan/internal/lite"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/monitor"
	"fuzhan/internal/notification"
	"fuzhan/internal/openapi"
	"fuzhan/internal/repositories"
	"fuzhan/internal/resource"
	"fuzhan/internal/search"
	"fuzhan/internal/security"
	"fuzhan/internal/services"
	"fuzhan/internal/setup"
	"fuzhan/internal/storage"
	temph "fuzhan/internal/temp"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)
// runCtx 汇总路由注册与 worker 运行所需的全部依赖，
// 避免在 main 中通过闭包传递上百个局部变量，也便于热重启时复用同一份依赖重新构建 Engine。
type runCtx struct {
	db              *gorm.DB
	cfg             *appconfig.Config
	validator       *appconfig.Validator
	recordRepo      repositories.AuditStore
	indexService    *index.Service
	fileService     *services.FileService
	downloadService *services.DownloadService
	searchService   *services.SearchService
	authService     *services.AuthService
	taskService     *services.TaskService

	baseHandler              *file.BaseHandler
	fileHandlers             *file.FileHandlers
	downloadHandlers         *file.DownloadHandlers
	searchHandlers           *file.SearchHandlers
	cliHandlers              *cli.CLIHandlers
	liteHandlers             *lite.Handlers
	bindingExampleHandler    *configh.BindingExampleHandler
	configHandler            *configh.Handler
	authHandler              *auth.Handler
	healthHandler            *health.HealthHandler
	setupHandler             *setup.SetupHandler
	uploadSessionHandler     *file.UploadSessionHandler
	privateUploadHandler     *file.PrivateUploadHandler
	privateStorageHandler    *file.PrivateStorageHandlers
	adminHandler             *admin.Handler
	securityHandler          *security.Handler
	notificationHandler      *notification.Handler
	adminURLDownloadHandler  *admin.URLDownloadHandler
	taskHandler              *admin.TaskHandler
	logHandler               *admin.LogHandler
	tempHandler              *temph.Handler
	indexHandler             *index.Handler
	depHandler               *index.DependencyHandler
	apiKeyHandler            *auth.ApiKeyHandler
	openAPIHandler           *openapi.Handler
	suggestHandler           *search.Handler
	recentHandler            *file.RecentHandler
	monitorHandler           *monitor.Handler
	resourceHandler          *resource.Handler
	previewHandler           *file.PreviewHandler
	behaviorAnalyticsHandler *behavioranalytics.Handler

	authMiddleware *middleware.AuthMiddleware
	rbacService    *auth.RBACService

	openAPICfg          *appconfig.OpenAPIConfig
	openAPIRateLimiter  *openapi.RateLimiter
	openAPIIPAccessCfg  *openapi.IPAccessConfig
	openAPICallStat     *openapi.APICallStat
	openAPIStatsHandler *openapi.StatsHandler

	idxSearch         *search.SearchIndex
	resourceCollector *resource.Collector
	ldapService       *auth.LDAPService

	// 公开多根目录的存储后端映射（rootName → Storage）。
	// 本地后端可不注入（rootNames 物理路径语义零变化）；远端后端（s3/minio/oss/webdav）
	// 注入后供 WebDAV/FTP 虚拟文件系统访问远端对象存储。
	publicStorages map[string]storage.Storage
}

// isCLIRequest 判断请求是否来自命令行工具（curl/wget）
func isCLIRequest(c *gin.Context) bool {
	ua := strings.ToLower(c.GetHeader("User-Agent"))
	return strings.HasPrefix(ua, "curl") || strings.HasPrefix(ua, "wget")
}

// isIEBrowser 判断请求是否来自 IE/Trident 内核浏览器（排除 Edge/Edg）。
func isIEBrowser(r *http.Request) bool {
	low := strings.ToLower(r.Header.Get("User-Agent"))
	if low == "" {
		return false
	}
	// 排除 Microsoft Edge（Chromium 内核 UA 含 edg/，旧版含 edge/）
	if strings.Contains(low, "edg/") || strings.Contains(low, "edge/") {
		return false
	}
	return strings.Contains(low, "msie") || strings.Contains(low, "trident")
}

// cliScriptWhitelsList 判断请求是否为 CLI 安装/升级脚本（/cli/install.sh、/cli/fuzhan.sh）。
// 这两个脚本在任何情况下保持匿名可下载，否则开启强制登录后新用户无法安装 CLI 工具。
func cliScriptWhitelist() func(*gin.Context) bool {
	return func(c *gin.Context) bool {
		p := c.Request.URL.Path
		return strings.HasSuffix(p, "/install.sh") || strings.HasSuffix(p, "/fuzhan.sh")
	}
}

// loginGateWhiteList 返回「强制登录」闸门的匿名例外汇总规则。
// 命中规则（返回 true）的请求即使开启强制登录也保持匿名可访问：
//   - 认证入口：登录/注册/OAuth2（否则无人能登录）
//   - 初始化引导：/setup/**（全新安装时的配置向导）
//   - 公开系统信息：/options、/health、扫描状态展示
//   - 临时文件分享：/temp/**（匿名分享链路，保持匿名例外）
//   - 记录与搜索联想：匿名页仍然使用到的少量轻量只读接口
func loginGateWhiteList() []func(*gin.Context) bool {
	prefixOf := func(prefix string) func(*gin.Context) bool {
		return func(c *gin.Context) bool {
			return strings.HasPrefix(c.Request.URL.Path, prefix)
		}
	}
	return []func(*gin.Context) bool{
		prefixOf("/api/v1/web/auth/"),
		prefixOf("/api/v1/web/temp/"),
		prefixOf("/api/v1/web/setup/"),
		prefixOf("/api/v1/web/health"),
		prefixOf("/api/v1/web/options"),
		prefixOf("/api/v1/web/monitor/keywords"),
		prefixOf("/api/v1/web/monitor/recent"),
		prefixOf("/api/v1/web/monitor/rankings"),
		prefixOf("/api/v1/web/monitor/hot-downloads"),
		prefixOf("/api/v1/web/admin/index/scan/"),
		prefixOf("/api/v1/web/files/scan/status"),
		prefixOf("/api/v1/web/search-suggest"),
		prefixOf("/api/v1/web/search-spellcheck"),
		prefixOf("/api/v1/web/search-related"),
		prefixOf("/api/v1/web/files/records/"),
	}
}

// redirectIEToLite 当请求来自 IE 浏览器时 302 跳转到 /lite（老旧浏览器改用轻量版页面）。
// 仅在 SPA 页面导航处（GET / 与 NoRoute fallback）调用；/lite、/assets、/download、API 等
// 独立路由不会走到这里，因此不会造成循环重定向。返回是否已跳转。
func redirectIEToLite(c *gin.Context) bool {
	if !isIEBrowser(c.Request) {
		return false
	}
	c.Redirect(http.StatusFound, "/lite")
	c.Abort()
	return true
}

// webdavRecordingRepo 将 repositories.AuditStore 适配为 webdav 的 RecordingRepository：
// 优先走全局有界记录器（与 FTP 一致），失败/未初始化时回退直写 AuditStore。
type webdavRecordingRepo struct {
	store repositories.AuditStore
}

func (r *webdavRecordingRepo) Create(rec *models.OperationRecord) error {
	if services.SubmitRecord(rec) {
		return nil
	}
	if r.store == nil {
		return nil
	}
	return r.store.Create(rec)
}
