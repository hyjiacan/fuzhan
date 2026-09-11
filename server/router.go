package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/access/webdav"
	"fuzhan/internal/admin"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/cli"
	configh "fuzhan/internal/config"
	"fuzhan/internal/file"
	"fuzhan/internal/ftp"
	"fuzhan/internal/health"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/monitor"
	"fuzhan/internal/notification"
	"fuzhan/internal/openapi"
	"fuzhan/internal/repositories"
	"fuzhan/internal/resource"
	"fuzhan/internal/search"
	"fuzhan/internal/services"
	"fuzhan/internal/setup"
	temph "fuzhan/internal/temp"
	"fuzhan/internal/utils"

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

	baseHandler             *file.BaseHandler
	fileHandlers            *file.FileHandlers
	downloadHandlers        *file.DownloadHandlers
	searchHandlers          *file.SearchHandlers
	cliHandlers             *cli.CLIHandlers
	bindingExampleHandler   *configh.BindingExampleHandler
	configHandler           *configh.Handler
	authHandler             *auth.Handler
	healthHandler           *health.HealthHandler
	setupHandler            *setup.SetupHandler
	uploadSessionHandler    *file.UploadSessionHandler
	privateUploadHandler    *file.PrivateUploadHandler
	privateStorageHandler   *file.PrivateStorageHandlers
	adminHandler            *admin.Handler
	notificationHandler     *notification.Handler
	adminURLDownloadHandler *admin.URLDownloadHandler
	taskHandler             *admin.TaskHandler
	tempHandler             *temph.Handler
	indexHandler            *index.Handler
	depHandler              *index.DependencyHandler
	apiKeyHandler           *auth.ApiKeyHandler
	openAPIHandler          *openapi.Handler
	suggestHandler          *search.Handler
	recentHandler           *file.RecentHandler
	monitorHandler          *monitor.Handler
	resourceHandler         *resource.Handler
	previewHandler          *file.PreviewHandler

	authMiddleware *middleware.AuthMiddleware
	rbacService    *auth.RBACService

	openAPICfg          *appconfig.OpenAPIConfig
	openAPIRateLimiter  *openapi.RateLimiter
	openAPIIPAccessCfg  *openapi.IPAccessConfig
	openAPICallStat     *openapi.APICallStat
	openAPIStatsHandler *openapi.StatsHandler

	idxSearch         *search.SearchIndex
	resourceCollector *resource.Collector
}

// isCLIRequest 判断请求是否来自命令行工具（curl/wget）
func isCLIRequest(c *gin.Context) bool {
	ua := strings.ToLower(c.GetHeader("User-Agent"))
	return strings.HasPrefix(ua, "curl") || strings.HasPrefix(ua, "wget")
}

// newRouter 创建 Gin Engine 与 FTP Handler（热重启时重新创建）
func (rt *runCtx) newRouter() (*gin.Engine, *ftp.FTPHandler) {
	// 创建 FTP 处理器（每次热重启时重建）
	ftpHandler := ftp.NewFTPHandler()
	ftpHandler.SetAuthUser(ftp.AuthUserFunc(func(username, password string) (string, error) {
		user, err := rt.authService.LoginByPassword(username, password)
		if err != nil {
			return "", err
		}
		return user.UUID, nil
	}))
	// 设置 FTP 操作录制回调（走全局有界记录器，避免每次操作起新协程）
	ftpHandler.SetRecordFunc(func(action, filePath, fileName, rootName, fileTypeTag, clientIP, userUUID string, fileSize int64) {
		rec := &models.OperationRecord{
			FileName: fileName,
			FilePath: filePath,
			// FullPath = /RootName + FilePath
			FullPath:   "/" + rootName + filePath,
			RootName:   rootName,
			FileSize:   fileSize,
			ClientIP:   clientIP,
			UserID:     userUUID,
			Action:     action,
			UploadTime: utils.Now(),
		}
		if !services.SubmitRecord(rec) {
			if err := rt.recordRepo.Create(rec); err != nil {
				utils.Debug("FTP操作记录失败",
					utils.String("action", action),
					utils.String("file", filePath),
					utils.Err(err))
			}
		}
	})

	// 创建 Gin 路由
	r := gin.New()

	// 添加监控中间件
	r.Use(middleware.MonitoringMiddleware())

	// 添加安全头中间件
	r.Use(middleware.SecurityHeadersMiddleware())

	// 添加跨域中间件
	r.Use(middleware.CORSMiddleware())

	// 添加请求ID中间件
	r.Use(middleware.RequestIDMiddleware())

	// 添加审计日志中间件（在 Auth 之前，捕获请求基础信息）
	r.Use(middleware.AuditMiddleware())

	// 添加自定义恢复中间件（使用结构化日志记录 panic 信息）
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		utils.Error("请求处理异常",
			utils.String("path", c.Request.URL.Path),
			utils.String("method", c.Request.Method),
			utils.Any("panic", recovered),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))

	// 获取前端构建目录
	webDir := appconfig.GetWebDir()

	// 检查是否存在磁盘上的前端构建
	useDiskAssets := false
	if _, err := os.Stat(webDir); err == nil {
		useDiskAssets = true
		utils.Info("使用磁盘上的前端资源", utils.String("dir", webDir))
	}

	// SPA 首页路由
	if useDiskAssets {
		// 磁盘资源
		assetsDir := filepath.Join(webDir, "assets")
		r.GET("/", func(c *gin.Context) {
			if isCLIRequest(c) {
				rt.cliHandlers.HandleCli(c)
				return
			}
			c.File(filepath.Join(webDir, "index.html"))
		})
		r.Static("/assets", assetsDir)
		// 独立页面
		r.GET("/scalar.html", func(c *gin.Context) {
			c.File(filepath.Join(webDir, "scalar.html"))
		})
	} else {
		// 嵌入资源
		r.GET("/", func(c *gin.Context) {
			if isCLIRequest(c) {
				rt.cliHandlers.HandleCli(c)
				return
			}
			data, err := webAssets.ReadFile("web/index.html")
			if err != nil {
				utils.HandleNotFound(c, "index.html not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
		r.StaticFS("/assets", http.FS(assetsFS))
		// 独立页面
		r.GET("/scalar.html", func(c *gin.Context) {
			data, err := webAssets.ReadFile("web/scalar.html")
			if err != nil {
				utils.HandleNotFound(c, "scalar.html not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
	}

	// SPA fallback: 支持 Vue Router 的 /* 路径
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// favicon 处理
		if path == "/favicon.ico" {
			c.Status(http.StatusNotFound)
			return
		}
		// API 路径返回 404
		if strings.HasPrefix(path, "/api/") {
			utils.HandleNotFound(c, "API 路由未找到")
			return
		}
		// 其他路径返回 index.html（SPA fallback）
		c.File(filepath.Join(webDir, "index.html"))
	})

	// API 路由组
	api := r.Group("/api/v1")
	rt.registerApiRoutes(r, api)

	// 顶层快捷入口与 Open API 路由组
	rt.registerTopLevelRoutes(r)

	return r, ftpHandler
}

// registerApiRoutes 注册 /api/v1 下的全部业务路由
func (rt *runCtx) registerApiRoutes(r *gin.Engine, api *gin.RouterGroup) {
	authMiddleware := rt.authMiddleware
	configHandler := rt.configHandler
	authHandler := rt.authHandler
	healthHandler := rt.healthHandler
	monitorHandler := rt.monitorHandler
	searchHandlers := rt.searchHandlers
	downloadHandlers := rt.downloadHandlers
	indexHandler := rt.indexHandler
	depHandler := rt.depHandler
	setupHandler := rt.setupHandler
	notificationHandler := rt.notificationHandler
	tempHandler := rt.tempHandler
	uploadSessionHandler := rt.uploadSessionHandler
	privateStorageHandler := rt.privateStorageHandler
	privateUploadHandler := rt.privateUploadHandler
	fileHandlers := rt.fileHandlers
	recentHandler := rt.recentHandler
	previewHandler := rt.previewHandler
	suggestHandler := rt.suggestHandler
	bindingExampleHandler := rt.bindingExampleHandler
	validator := rt.validator

	// 健康检查路由（无需认证）
	api.GET("/health", healthHandler.Health)
	api.GET("/ready", healthHandler.Ready)
	// 监控指标路由（需要认证，防止信息泄露）
	api.GET("/metrics", authMiddleware.AuthRequired(), middleware.PrometheusHandler())

	// 系统监测路由（无需认证）
	api.GET("/monitor/storage", monitorHandler.Storage)
	api.GET("/monitor/access", monitorHandler.Access)
	api.GET("/monitor/keywords", monitorHandler.Keywords)
	api.GET("/monitor/recent", monitorHandler.RecentKeywords)
	api.GET("/monitor/rankings", monitorHandler.Rankings)
	api.GET("/monitor/hot-downloads", monitorHandler.HotDownloads)

	// 系统选项路由（无需认证，供前端加载时使用）
	api.GET("/options", configHandler.GetOptions)

	// 认证路由（无需认证）
	auth := api.Group("/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
	}

	// 分享下载路由（无需认证）
	r.GET("/share/:code", privateStorageHandler.Download)
	r.HEAD("/share/:code", privateStorageHandler.Download)

	// 私有存储路由（需要认证）
	private := api.Group("/private")
	private.Use(authMiddleware.AuthRequired(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	{
		private.GET("/files", privateStorageHandler.List)
		private.GET("/quota", privateUploadHandler.QuotaHandler)
		private.POST("/upload", privateStorageHandler.Upload) // 简单上传
		private.DELETE("/files/:code", privateStorageHandler.Delete)

		// 私有存储分片上传路由
		privateUploads := private.Group("/uploads")
		{
			privateUploads.POST("/session", privateUploadHandler.CreateSession)
			privateUploads.GET("/session/:id", privateUploadHandler.GetSession)
			privateUploads.POST("/session/:id/resume", privateUploadHandler.ResumeSession)
			privateUploads.DELETE("/session/:id", privateUploadHandler.CancelSession)
			privateUploads.POST("/chunk", privateUploadHandler.UploadChunk)
			privateUploads.POST("/finalize", privateUploadHandler.FinalizeSession)
		}
	}

	// 管理页面搜索和下载（需管理员鉴权；下载返回文件流不套 ResponseWrapper）
	api.GET("/admin/search/*query", authMiddleware.AuthRequired(), middleware.RequireAdmin(), searchHandlers.AdminSearchFiles)
	api.GET("/admin/download/*path", authMiddleware.AuthRequired(), middleware.RequireAdmin(), downloadHandlers.AdminDownloadFile)
	api.HEAD("/admin/download/*path", authMiddleware.AuthRequired(), middleware.RequireAdmin(), downloadHandlers.AdminDownloadFile)

	// 文件记录搜索（用于 autocomplete，无需 admin）
	api.GET("/files/search-records", authMiddleware.AuthOptional(), middleware.ResponseWrapperMiddleware(), indexHandler.SearchFileRecords)

	// 扫描状态路由（无需认证，footer 与页面用于展示扫描状态）
	scanRoutes := api.Group("/admin/index")
	{
		scanRoutes.GET("/scan/progress", indexHandler.GetScanProgress)
		scanRoutes.GET("/scan/status", indexHandler.GetScanStatus)
	}

	// 管理员路由（需要认证和管理员权限）
	adminGroup := api.Group("/admin")
	adminGroup.Use(authMiddleware.AuthRequired(), middleware.RequireAdmin(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	rt.registerAdminRoutes(adminGroup)

	// 配置引导路由（无需认证；初始化后除 status 外的所有 setup 路由返回 404）
	setup := api.Group("/setup")
	setup.GET("/status", setupHandler.IsInitialized)
	setupInitOnly := api.Group("/setup")
	setupInitOnly.Use(func(c *gin.Context) {
		if appconfig.GlobalConfig.App.Initialized {
			utils.HandleNotFound(c, "API 路由未找到")
			c.Abort()
			return
		}
		c.Next()
	})
	setupInitOnly.POST("/save", setupHandler.SaveConfig)
	setupInitOnly.POST("/validate-dir", setupHandler.ValidateDirectory)
	setupInitOnly.GET("/network/interfaces", setupHandler.GetNetworkInterfaces)
	setupInitOnly.GET("/default-config", setupHandler.GetDefaultConfig)

	// 通知路由（可选认证，同时支持已登录和匿名用户）
	notifications := api.Group("/notifications")
	notifications.Use(authMiddleware.AuthOptional())
	{
		notifications.GET("", notificationHandler.GetNotifications)
		notifications.POST("/read", notificationHandler.MarkRead)
		notifications.POST("/merge", notificationHandler.MergeAnonymous)
	}

	// 系统配置路由（需要管理员认证）
	configGroup := api.Group("/config")
	configGroup.Use(authMiddleware.AuthRequired(), middleware.RequireAdmin(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	configGroup.GET("", configHandler.GetConfig)
	configGroup.POST("", configHandler.SaveConfig)

	// 临时文件路由（基于IP，无需认证）
	temp := api.Group("/temp")
	temp.GET("/list", tempHandler.ListHandler)
	temp.GET("/quota", tempHandler.QuotaHandler)
	temp.GET("/client-ip", tempHandler.ClientIPHandler)
	temp.GET("/:code", tempHandler.InfoHandler)
	temp.GET("/:code/download", tempHandler.DownloadHandler)
	temp.HEAD("/:code/download", tempHandler.DownloadHandler)
	temp.DELETE("/:code", tempHandler.DeleteHandler)

	// 临时文件分片上传路由
	tempUploads := api.Group("/temp/upload")
	{
		tempUploads.POST("/session", tempHandler.CreateTempSessionHandler)
		tempUploads.GET("/session/:uploadId", tempHandler.GetTempSessionHandler)
		tempUploads.POST("/session/:uploadId/resume", tempHandler.ResumeTempSessionHandler)
		tempUploads.DELETE("/session/:uploadId", tempHandler.CancelTempSessionHandler)
		tempUploads.POST("/chunk", tempHandler.UploadTempChunkHandler)
		tempUploads.POST("/finalize", tempHandler.FinalizeTempUploadHandler)
	}

	// 上传会话路由（无需认证，支持断点续传）
	uploads := api.Group("/uploads")
	{
		uploads.POST("/session", authMiddleware.AuthOptional(), uploadSessionHandler.CreateSession)
		uploads.GET("/session/:id", uploadSessionHandler.GetSession)
		uploads.POST("/session/:id/resume", uploadSessionHandler.ResumeSession)
		uploads.DELETE("/session/:id", uploadSessionHandler.CancelSession)
		uploads.POST("/chunk", uploadSessionHandler.UploadChunk)
		uploads.POST("/finalize", authMiddleware.AuthOptional(), uploadSessionHandler.FinalizeSession)
		uploads.POST("/url", uploadSessionHandler.UploadFromURL)          // 从URL上传
		uploads.GET("/url-task/:taskId", uploadSessionHandler.GetURLTask) // URL任务进度查询
		uploads.POST("/url_info", func(c *gin.Context) {
			file.GetFileInfoFromURL(c.Writer, c.Request)
		})
		uploads.GET("/sessions", authMiddleware.AuthOptional(), uploadSessionHandler.ListUploadSessions)
		uploads.GET("/url-tasks", authMiddleware.AuthOptional(), uploadSessionHandler.ListURLTasks)
		uploads.POST("/url-tasks/:taskId/cancel", authMiddleware.AuthOptional(), uploadSessionHandler.CancelURLTask)
		uploads.POST("/url-tasks/:taskId/retry", authMiddleware.AuthOptional(), uploadSessionHandler.RetryURLTask)
		uploads.DELETE("/url-tasks/:taskId", authMiddleware.AuthOptional(), uploadSessionHandler.DeleteURLTask)
	}

	// 无需认证的文件路由
	api.GET("/files/list", fileHandlers.ListDirectories)
	api.GET("/files/depends/:id", depHandler.GetDependencyTree)

	// 下载路由（无需认证）
	api.GET("/download/*path", downloadHandlers.DownloadFile)
	api.HEAD("/download/*path", downloadHandlers.DownloadFile)

	// 最近上传路由（无需认证）
	files := api.Group("/files")
	{
		files.GET("/recent", recentHandler.GetRecent)
		files.GET("/recent/carousel", recentHandler.GetRecentCarousel)
		files.GET("/preview/*path", previewHandler.PreviewFile)    // 文件预览
		files.GET("/preview-chunk/*path", previewHandler.GetChunk) // 读取文件分块
		files.PUT("/records/:id/notes", indexHandler.PublicUpdateNotes)
		files.GET("/records/:id/notes", indexHandler.PublicGetNotes)
		files.POST("/dependencies", depHandler.CreateDependencyPublic)
		files.DELETE("/dependencies/:id", depHandler.DeleteDependencyPublic)
		files.GET("/record", indexHandler.FindFileRecord)
		files.GET("/scan/status", indexHandler.GetScanStatus) // Footer 扫描状态

		// 搜索路由（无需认证）
		api.GET("/search/*query", searchHandlers.SearchFiles)

		// 文件名检索：自动补全（搜索框实时联想）与拼写纠错
		api.GET("/search-suggest", suggestHandler.Autocomplete)
		api.GET("/search-spellcheck", suggestHandler.SpellCheck)
	}

	// 需要认证的路由组
	protected := api.Group("/")
	protected.Use(authMiddleware.AuthRequired(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	{
		// 认证相关路由
		protected.POST("/auth/refresh", authHandler.Login) // 复用登录逻辑
		protected.GET("/auth/user", authHandler.GetCurrentUser)
		protected.PUT("/auth/password", authHandler.ChangePassword)

		// 文件管理路由已全部迁移至 admin 组（仅管理员可操作公开文件）
		protected.GET("/get_file_info", func(c *gin.Context) {
			file.GetFileInfoFromURL(c.Writer, c.Request)
		})
		// 数据绑定示例路由
		examples := protected.Group("/examples")
		{
			examples.POST("/upload", bindingExampleHandler.UploadWithBinding)
			examples.POST("/rename", bindingExampleHandler.RenameFileWithBinding)

			// 验证中间件示例路由
			examples.POST("/validation/upload", func(c *gin.Context) {
				var req models.UploadRequest
				if err := c.ShouldBind(&req); err != nil {
					validationErrors := validator.TranslateError(err)
					if len(validationErrors) > 0 {
						utils.HandleBadRequest(c, "请求数据验证失败", validationErrors)
						return
					}

					utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
					return
				}

				utils.HandleSuccess(c, http.StatusOK, "验证通过", req)
			})
		}
	}

	// WebDAV 统一端点（在 server.webdav.enabled 时启用）
	// 可选 BasicAuth 认证：匿名用户只访问 public/，认证用户访问 public/ + private/
	if rt.cfg.Server.WebDAV.Enabled {
		authenticate := webdav.UserAuthenticator(func(username, password string) (string, bool, error) {
			user, err := rt.authService.LoginByPassword(username, password)
			if err != nil {
				return "", false, err
			}
			return user.UUID, user.Disabled, nil
		})

		publicFS := webdav.NewPublicFileSystem(appconfig.RootNames)
		var unifiedFS *webdav.UnifiedFileSystem
		if rt.cfg.Storage.Private.Enabled {
			privateFS := webdav.NewPrivateFileSystem(rt.cfg.Storage.Private.Path)
			unifiedFS = webdav.NewUnifiedFileSystem(publicFS, privateFS)
		} else {
			unifiedFS = webdav.NewUnifiedFileSystem(publicFS, nil)
		}

		davHandler := webdav.NewHandler("/api/v1/webdav", unifiedFS)
		// 挂到 /api/v1/webdav 子组，避免 /*path 通配符与 /api/v1 下同级静态路由冲突
		webdav.SetupUnifiedRouter(api.Group("/webdav"), davHandler, authenticate, rt.cfg.Upload.MaxFileSize)
		utils.Info("WebDAV 统一端点已启用", utils.String("path", "/api/v1/webdav"))
	} else {
		utils.Info("WebDAV 服务未启用")
	}

	// CLI 路由（无需认证）
	cli := api.Group("/cli")
	{
		cli.GET("/", rt.cliHandlers.HandleCli)
		cli.GET("/search/*query", rt.cliHandlers.CliSearch)
		cli.GET("/list/*path", rt.cliHandlers.CliList)
		cli.GET("/install.sh", rt.cliHandlers.InstallScript)
		cli.GET("/fuzhan.sh", rt.cliHandlers.FuzhanScript)
	}
}

// registerAdminRoutes 注册 /api/v1/admin 下需要管理员权限的路由
func (rt *runCtx) registerAdminRoutes(admin *gin.RouterGroup) {
	adminHandler := rt.adminHandler
	fileHandlers := rt.fileHandlers
	adminURLDownloadHandler := rt.adminURLDownloadHandler
	taskHandler := rt.taskHandler
	apiKeyHandler := rt.apiKeyHandler
	indexHandler := rt.indexHandler
	depHandler := rt.depHandler

	// 全量/触发扫描（需管理员鉴权，防止匿名资源耗尽 DoS）
	admin.POST("/index/scan", indexHandler.TriggerScan)
	admin.POST("/index/scan/trigger", indexHandler.TriggerFullScan)
	admin.GET("/users", adminHandler.UsersHandler)
	admin.PUT("/users/:uuid/reset-password", adminHandler.ResetPasswordHandler)
	admin.PUT("/users/:uuid/disabled", adminHandler.SetUserDisabledHandler)
	admin.DELETE("/users/:uuid", adminHandler.DeleteUserHandler)
	admin.GET("/sessions", adminHandler.SessionsHandler)
	admin.POST("/sessions/cleanup", adminHandler.CleanupSessionsHandler)

	// 在线 IP 统计路由（与登录无关，依据最近请求判定在线）
	admin.GET("/online-ips", adminHandler.OnlineIPs)

	// 操作记录清空/删除路由
	admin.POST("/records/clear", adminHandler.ClearRecordsHandler)
	admin.POST("/records/delete", adminHandler.DeleteRecordHandler)

	// TLS 证书上传路由
	admin.POST("/upload-cert", adminHandler.UploadCertHandler)
	admin.POST("/upload-key", adminHandler.UploadKeyHandler)

	// 文件管理路由
	admin.GET("/files/list", fileHandlers.ListDirectories)
	admin.POST("/files/move", fileHandlers.MoveFileHandler)
	admin.DELETE("/files", fileHandlers.DeleteFileHandler)

	// URL 下载任务管理路由
	admin.GET("/url-tasks", adminURLDownloadHandler.ListURLTasks)
	admin.POST("/url-tasks/:id/retry", adminURLDownloadHandler.RetryURLTask)
	admin.DELETE("/url-tasks/:id", adminURLDownloadHandler.DeleteURLTask)

	// 任务管理路由
	admin.GET("/tasks", taskHandler.ListTasks)
	admin.GET("/tasks/history", taskHandler.GetTaskHistory)
	admin.GET("/tasks/:id", taskHandler.GetTask)
	admin.POST("/tasks/:id/cancel", taskHandler.CancelTask)

	// API Key 管理路由（v3 Phase 2）
	apiKeyRoutes := admin.Group("/api-keys")
	{
		apiKeyRoutes.GET("", apiKeyHandler.ListApiKeys)
		apiKeyRoutes.POST("", apiKeyHandler.CreateApiKey)
		apiKeyRoutes.GET("/:id", apiKeyHandler.GetApiKey)
		apiKeyRoutes.PUT("/:id/status", apiKeyHandler.UpdateApiKeyStatus)
		apiKeyRoutes.DELETE("/:id", apiKeyHandler.DeleteApiKey)
	}

	// 文件索引管理路由（v3 Phase 0，仅保留管理端专用路由）
	indexRoutes := admin.Group("/index")
	{
		indexRoutes.GET("/records", indexHandler.ListRecords)
		indexRoutes.GET("/stats", indexHandler.GetStats)
		indexRoutes.POST("/check", indexHandler.TriggerConsistencyCheck)
		indexRoutes.DELETE("/records/:id", indexHandler.DeleteRecord)

		// v3 Phase 1 — 衍生功能
		indexRoutes.GET("/duplicates", indexHandler.ListDuplicates)
		indexRoutes.POST("/duplicates/:id/keep", indexHandler.KeepDuplicate)
		indexRoutes.PUT("/records/:id/notes", indexHandler.UpdateNotes)
		indexRoutes.GET("/dependencies", depHandler.ListDependencies)
		indexRoutes.POST("/dependencies", depHandler.CreateDependency)
		indexRoutes.GET("/records/:id/dependencies", depHandler.GetDependenciesByRecord)
		indexRoutes.DELETE("/dependencies/:id", depHandler.DeleteDependency)
	}

	// Open API 调用统计（v3 Phase 3）
	admin.GET("/open-api/stats", rt.openAPIStatsHandler.GetStats)

	// 服务器资源监控
	resourceRoutes := admin.Group("/resource")
	{
		resourceRoutes.GET("/snapshot", rt.resourceHandler.Snapshot)
		resourceRoutes.GET("/history", rt.resourceHandler.History)
	}
}

// registerTopLevelRoutes 注册顶层快捷入口（/cli、/download）与 Open API 路由组（/api/open/v1）
func (rt *runCtx) registerTopLevelRoutes(r *gin.Engine) {
	// CLI 路由别名（无需认证，/cli 作为 /api/v1/cli 的快捷入口）
	cliAlias := r.Group("/cli")
	{
		cliAlias.GET("/", rt.cliHandlers.HandleCli)
		cliAlias.GET("/search/*query", rt.cliHandlers.CliSearch)
		cliAlias.GET("/list/*path", rt.cliHandlers.CliList)
		cliAlias.GET("/install.sh", rt.cliHandlers.InstallScript)
		cliAlias.GET("/fuzhan.sh", rt.cliHandlers.FuzhanScript)
	}

	// 下载路由别名（无需认证，/download 作为 /api/v1/download 的快捷入口）
	downloadAlias := r.Group("/download")
	{
		downloadAlias.GET("/*path", rt.downloadHandlers.DownloadFile)
		downloadAlias.HEAD("/*path", rt.downloadHandlers.DownloadFile)
	}

	// Open API 路由组（v3 Phase 3）
	// 架构要求中间件链: IP白名单 → 功能开关 → 频率限制 → 调用统计 → 路由分发 → Handler
	// 见 architecture-v3.md
	openAPI := r.Group("/api/open/v1")
	openAPI.Use(
		openapi.FeatureGateMiddleware(rt.openAPICfg.Enabled),
		openapi.IPAccessMiddleware(rt.openAPIIPAccessCfg),
		openapi.RateLimitMiddleware(rt.openAPIRateLimiter),
		openapi.CallStatMiddleware(rt.openAPICallStat),
		middleware.ResponseWrapperMiddleware(),
	)
	{
		// FR-3.1: 文件列表 API（公开，无需认证）
		openAPI.GET("/files/list", rt.openAPIHandler.ListFiles)
		// FR-3.2: 文件搜索 API（公开）
		openAPI.GET("/files/search", rt.openAPIHandler.SearchFiles)
		// FR-3.3: 文件下载 API（公开）
		openAPI.GET("/files/download/*path", rt.openAPIHandler.DownloadFile)
		// FR-3.5: 重复文件查询 API（需认证，需要 reader scope）
		openAPI.GET("/files/duplicates", rt.rbacService.RequireScope("open_api:reader"), rt.openAPIHandler.ListDuplicates)
		// FR-3.6: 文件备注查询 API（需认证，需要 reader scope）
		openAPI.GET("/files/notes", rt.rbacService.RequireScope("open_api:reader"), rt.openAPIHandler.GetNotes)
		// FR-4.1: 文件依赖树查询 API（需认证，需要 reader scope）
		openAPI.GET("/files/:id/depends", rt.rbacService.RequireScope("open_api:reader"), rt.openAPIHandler.GetDependencyTree)
		// FR-3.10: OpenAPI 文档（公开）
		openAPI.GET("/openapi.json", rt.openAPIHandler.GetOpenAPISpec)
		openAPI.GET("/docs", rt.openAPIHandler.GetDocsPage)
	}
}
