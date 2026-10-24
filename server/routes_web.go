package main

import (
	"net/http"

	"fuzhan/internal/access/webdav"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/file"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"

	"github.com/gin-gonic/gin"
)

// registerApiRoutes 注册 /api/v1 下的全部业务路由。
// web 为 /api/v1/web 组，承载 Web 界面（含管理后台）直接调用的接口；
// api 为 /api/v1 组，仅承载非界面渠道（CLI、WebDAV、就绪探针）。
func (rt *runCtx) registerApiRoutes(r *gin.Engine, api, web *gin.RouterGroup) {
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
	web.GET("/health", healthHandler.Health)
	// 就绪探针（基础设施，非界面调用）
	api.GET("/ready", healthHandler.Ready)
	// 监控指标路由（需要认证，防止信息泄露）
	api.GET("/metrics", authMiddleware.AuthRequired(), middleware.PrometheusHandler())

	// 系统监测路由
	// 存储与访问统计是重查询（整盘 Walk / operation_records 计数），仅管理面板使用，
	// 加管理员认证避免未授权者反复触发磁盘 IO/全表统计（廉价 DoS）。
	// keywords/recent/rankings/hot-downloads 被公开的 /hot、/recent 页使用，保持可匿名访问。
	web.GET("/monitor/storage", authMiddleware.AuthRequired(), middleware.RequireAdmin(), monitorHandler.Storage)
	web.GET("/monitor/access", authMiddleware.AuthRequired(), middleware.RequireAdmin(), monitorHandler.Access)
	web.GET("/monitor/keywords", monitorHandler.Keywords)
	web.GET("/monitor/recent", monitorHandler.RecentKeywords)
	web.GET("/monitor/rankings", monitorHandler.Rankings)
	web.GET("/monitor/hot-downloads", monitorHandler.HotDownloads)

	// 系统选项路由（无需认证，供前端加载时使用）
	web.GET("/options", configHandler.GetOptions)

	// 认证路由（无需认证）
	auth := web.Group("/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		// 外部 OAuth2/OIDC 认证：状态查询、发起授权、回调换取本地令牌
		auth.GET("/oauth2/status", authHandler.OAuth2Status)
		auth.GET("/oauth2/authorize", authHandler.OAuth2Authorize)
		auth.GET("/oauth2/callback", authHandler.OAuth2Callback)
	}

	// 分享下载路由（无需认证）
	r.GET("/share/:code", privateStorageHandler.Download)
	r.HEAD("/share/:code", privateStorageHandler.Download)

	// 私有存储路由（需要认证）
	private := web.Group("/private")
	private.Use(authMiddleware.AuthRequired(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	{
		private.GET("/files", privateStorageHandler.List)
		private.GET("/quota", privateUploadHandler.QuotaHandler)
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

	// 换取一次性下载票据：登录凭证只走请求头，换得的票据（URL 上的 t=）供浏览器原生
	// 标签/新窗口/外部下载工具直连下载与内联预览（它们无法携带请求头）。
	// 随 web 组一并受 LoginGate 保护（require_login 开启时需先登录）。
	web.POST("/download/ticket", downloadHandlers.IssueTicket)

	// 管理页面搜索和下载（需管理员鉴权；下载返回文件流不套 ResponseWrapper）
	web.GET("/admin/search/*query", authMiddleware.AuthRequired(), middleware.RequireAdmin(), searchHandlers.AdminSearchFiles)
	web.GET("/admin/download/*path", authMiddleware.AuthRequired(), middleware.RequireAdmin(), downloadHandlers.AdminDownloadFile)
	web.HEAD("/admin/download/*path", authMiddleware.AuthRequired(), middleware.RequireAdmin(), downloadHandlers.AdminDownloadFile)

	// 文件记录搜索（用于 autocomplete，无需 admin）
	web.GET("/files/search-records", authMiddleware.AuthOptional(), middleware.ResponseWrapperMiddleware(), indexHandler.SearchFileRecords)

	// 扫描状态路由（无需认证，footer 与页面用于展示扫描状态）
	scanRoutes := web.Group("/admin/index")
	{
		scanRoutes.GET("/scan/progress", indexHandler.GetScanProgress)
		scanRoutes.GET("/scan/status", indexHandler.GetScanStatus)
	}

	// 管理员路由（需要认证和管理员权限）
	adminGroup := web.Group("/admin")
	adminGroup.Use(authMiddleware.AuthRequired(), middleware.RequireAdmin(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	rt.registerAdminRoutes(adminGroup)

	// 配置引导路由（无需认证；初始化后除 status 外的所有 setup 路由返回 404）
	setup := web.Group("/setup")
	setup.GET("/status", setupHandler.IsInitialized)
	setupInitOnly := web.Group("/setup")
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
	notifications := web.Group("/notifications")
	notifications.Use(authMiddleware.AuthOptional())
	{
		notifications.GET("", notificationHandler.GetNotifications)
		notifications.POST("/read", notificationHandler.MarkRead)
		notifications.POST("/merge", notificationHandler.MergeAnonymous)
	}

	// 系统配置路由（需要管理员认证）
	configGroup := web.Group("/config")
	configGroup.Use(authMiddleware.AuthRequired(), middleware.RequireAdmin(), middleware.TokenRefreshMiddleware(rt.authService), middleware.ResponseWrapperMiddleware())
	configGroup.GET("", configHandler.GetConfig)
	configGroup.POST("", configHandler.SaveConfig)
	configGroup.POST("/ldap/test", configHandler.TestLDAPConfig)
	configGroup.POST("/oauth2/test", configHandler.TestOAuth2Config)
	configGroup.GET("/partitions", configHandler.ListPartitions)

	// 临时文件路由（基于IP，无需认证）
	temp := web.Group("/temp")
	{
		temp.GET("/list", tempHandler.ListHandler)
		temp.GET("/quota", tempHandler.QuotaHandler)
		temp.GET("/client-ip", tempHandler.ClientIPHandler)
		temp.GET("/:code", tempHandler.InfoHandler)
		temp.GET("/:code/download", tempHandler.DownloadHandler)
		temp.HEAD("/:code/download", tempHandler.DownloadHandler)
		temp.DELETE("/:code", tempHandler.DeleteHandler)
	}

	// 临时文件分片上传路由
	tempUploads := web.Group("/temp/upload")
	{
		tempUploads.POST("/session", middleware.UploadRateLimit(), tempHandler.CreateTempSessionHandler)
		tempUploads.GET("/session/:uploadId", tempHandler.GetTempSessionHandler)
		tempUploads.POST("/session/:uploadId/resume", tempHandler.ResumeTempSessionHandler)
		tempUploads.DELETE("/session/:uploadId", tempHandler.CancelTempSessionHandler)
		tempUploads.POST("/chunk", tempHandler.UploadTempChunkHandler)
		tempUploads.POST("/finalize", middleware.UploadRateLimit(), tempHandler.FinalizeTempUploadHandler)
	}

	// 上传会话路由（无需认证，支持断点续传）
	uploads := web.Group("/uploads")
	{
		uploads.POST("/session", middleware.UploadRateLimit(), authMiddleware.AuthOptional(), uploadSessionHandler.CreateSession)
		uploads.GET("/session/:id", uploadSessionHandler.GetSession)
		uploads.POST("/session/:id/resume", uploadSessionHandler.ResumeSession)
		uploads.DELETE("/session/:id", uploadSessionHandler.CancelSession)
		uploads.POST("/chunk", uploadSessionHandler.UploadChunk)
		uploads.POST("/finalize", middleware.UploadRateLimit(), authMiddleware.AuthOptional(), uploadSessionHandler.FinalizeSession)
		uploads.POST("/url", middleware.UploadRateLimit(), authMiddleware.AuthOptional(), uploadSessionHandler.UploadFromURL) // 从URL上传（入队，后台队列调度）
		uploads.POST("/url-probe", authMiddleware.AuthOptional(), uploadSessionHandler.ProbeURLs)                             // 批量探测URL（文件名/存在性/历史）
		uploads.GET("/url-task/:taskId", authMiddleware.AuthOptional(), uploadSessionHandler.GetURLTask)                      // URL任务进度查询
		uploads.POST("/url_info", func(c *gin.Context) {
			file.GetFileInfoFromURL(c.Writer, c.Request)
		})
		uploads.GET("/sessions", authMiddleware.AuthOptional(), uploadSessionHandler.ListUploadSessions)
		uploads.GET("/url-tasks", authMiddleware.AuthOptional(), uploadSessionHandler.ListURLTasks)
		uploads.POST("/url-tasks/:taskId/cancel", authMiddleware.AuthOptional(), uploadSessionHandler.CancelURLTask)
		uploads.POST("/url-tasks/:taskId/retry", authMiddleware.AuthOptional(), uploadSessionHandler.RetryURLTask)
		uploads.POST("/url-tasks/:taskId/checksum", authMiddleware.AuthOptional(), uploadSessionHandler.ComputeURLTaskChecksum) // 计算已完成任务的校验值（md5/sha1/sha256）
		uploads.DELETE("/url-tasks/:taskId", authMiddleware.AuthOptional(), uploadSessionHandler.DeleteURLTask)
	}

	// 无需认证的文件路由
	web.GET("/files/list", fileHandlers.ListDirectories)
	web.GET("/files/depends/:id", depHandler.GetDependencyTree)

	// 下载路由（无需认证；/download/*path 根级别名指向同一 handler）
	web.GET("/download/*path", downloadHandlers.DownloadFile)
	web.HEAD("/download/*path", downloadHandlers.DownloadFile)

	// 最近上传路由（无需认证）
	files := web.Group("/files")
	{
		files.GET("/recent", recentHandler.GetRecent)
		files.GET("/recent/carousel", recentHandler.GetRecentCarousel)
		files.GET("/preview/*path", previewHandler.PreviewFile)     // 文件预览
		files.GET("/preview-chunk/*path", previewHandler.GetChunk)  // 读取文件分块
		files.POST("/checksum", previewHandler.ComputeFileChecksum) // 计算已上传文件校验值（md5/sha1/sha256）
		files.PUT("/records/:id/notes", indexHandler.PublicUpdateNotes)
		files.GET("/records/:id/notes", indexHandler.PublicGetNotes)
		files.POST("/dependencies", depHandler.CreateDependencyPublic)
		files.DELETE("/dependencies/:id", depHandler.DeleteDependencyPublic)
		files.GET("/record", indexHandler.FindFileRecord)
		files.GET("/scan/status", indexHandler.GetScanStatus) // Footer 扫描状态

		// 搜索路由（无需认证）
		web.GET("/search/*query", searchHandlers.SearchFiles)

		// 文件名检索：自动补全（搜索框实时联想）与拼写纠错
		web.GET("/search-suggest", suggestHandler.Autocomplete)
		web.GET("/search-spellcheck", suggestHandler.SpellCheck)
		web.GET("/search-related", suggestHandler.Related) // 输入空格后的下一段相关词候选
	}

	// 需要认证的路由组
	protected := web.Group("/")
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
		davHandler, authenticate := rt.buildWebDAVHandler(constants.WebDAVPathPrefix)
		// 挂到 /api/v1/webdav 子组，避免 /*path 通配符与 /api/v1 下同级静态路由冲突
		webdav.SetupUnifiedRouter(api.Group("/webdav"), davHandler, authenticate, rt.cfg.Upload.MaxFileSize)
		utils.Info("WebDAV 统一端点已启用", utils.String("path", constants.WebDAVPathPrefix))
	} else {
		utils.Info("WebDAV 服务未启用")
	}

	// CLI 路由（开启「强制登录」时需认证；安装/升级脚本保持匿名）
	cli := api.Group("/cli")
	cli.Use(rt.authMiddleware.LoginGate(cliScriptWhitelist()))
	{
		cli.GET("/", rt.cliHandlers.HandleCli)
		cli.GET("/search/*query", rt.cliHandlers.CliSearch)
		cli.GET("/list/*path", rt.cliHandlers.CliList)
		cli.GET("/install.sh", rt.cliHandlers.InstallScript)
		cli.GET("/fuzhan.sh", rt.cliHandlers.FuzhanScript)
	}
}
