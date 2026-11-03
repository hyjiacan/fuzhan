package main

import (
	"net/http"

	"fuzhan/internal/access/webdav"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	litepkg "fuzhan/internal/lite"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/openapi"
	"fuzhan/internal/utils"
	webdavsvc "fuzhan/internal/webdav"

	"github.com/gin-gonic/gin"
)

// buildWebDAVHandler 依据当前配置构造 WebDAV 文件系统与 Handler。
// 供 /api/v1/webdav 子组（registerApiRoutes）与 /webdav 别名（registerTopLevelRoutes）共用。
// prefix 指定该 Handler 的挂载前缀（路由注册路径），需与调用方实际挂载路径一致。
func (rt *runCtx) buildWebDAVHandler(prefix string) (http.Handler, webdav.UserAuthenticator) {
	authenticate := webdav.UserAuthenticator(func(username, password string) (string, bool, error) {
		user, err := rt.authService.LoginByPassword(username, password)
		if err != nil {
			return "", false, err
		}
		return user.UUID, user.Disabled, nil
	})

	privatePath := ""
	if rt.cfg.Storage.Private.Enabled {
		privatePath = rt.cfg.Storage.Private.Path
	}
	wdService := webdavsvc.NewWebDAVBackend(rt.db, rt.indexService, privatePath)

	publicFS := webdav.NewPublicFileSystem(appconfig.RootNames)
	publicFS.SetBackend(wdService)
	// 注入公开多根目录的存储后端映射：远端后端（s3/minio/oss/webdav）经 Storage
	// 接口访问对象存储，本地后端不注入（保持物理路径语义）
	publicFS.SetStorages(rt.publicStorages)

	var unifiedFS *webdav.UnifiedFileSystem
	if rt.cfg.Storage.Private.Enabled {
		privateFS := webdav.NewPrivateFileSystem(rt.cfg.Storage.Private.Path)
		privateFS.SetBackend(wdService)
		unifiedFS = webdav.NewUnifiedFileSystem(publicFS, privateFS)
	} else {
		unifiedFS = webdav.NewUnifiedFileSystem(publicFS, nil)
	}

	// 操作录制（审计）：优先走全局有界记录器，与 FTP 行为一致
	recordingFS := webdav.NewRecordingFileSystem(unifiedFS, &webdavRecordingRepo{store: rt.recordRepo})
	davHandler := webdav.NewHandler(prefix, recordingFS)
	// 检索支持：SEARCH（DASL / RFC 5323）与 REPORT（ownCloud 非标准）由
	// SearchableHandler 接管，直接检索文件索引表；其余方法透传标准 Handler。
	searchable := &webdav.SearchableHandler{Standard: davHandler, DB: rt.db, Prefix: prefix}
	return searchable, authenticate
}

// registerTopLevelRoutes 注册顶层快捷入口（/cli、/download、/webdav）与 Open API 路由组（/api/open/v1）
func (rt *runCtx) registerTopLevelRoutes(r *gin.Engine) {
	// CLI 路由别名（开启「强制登录」时需认证；/cli 作为 /api/v1/cli 的快捷入口，安装/升级脚本保持匿名）
	cliAlias := r.Group("/cli")
	cliAlias.Use(rt.authMiddleware.LoginGate(cliScriptWhitelist()))
	{
		cliAlias.GET("/", rt.cliHandlers.HandleCli)
		cliAlias.GET("/search/*query", rt.cliHandlers.CliSearch)
		cliAlias.GET("/list/*path", rt.cliHandlers.CliList)
		cliAlias.GET("/install.sh", rt.cliHandlers.InstallScript)
		cliAlias.GET("/fuzhan.sh", rt.cliHandlers.FuzhanScript)
	}

	// 下载路由别名（开启「强制登录」时需认证；/download 作为 /api/v1/web/download 的快捷入口。
	// 浏览器原生标签/新窗口/外部下载工具发起的请求只能使用 URL、无法携带请求头：
	// 前端先在脚本内用登录 token（Authorization 头）换取一次性下载票据（URL 上 t= 参数），
	// 再以其打开原生下载链接；LoginGate 对携带 t= 的请求做票据验签并注入用户上下文。）
	downloadAlias := r.Group("/download")
	downloadAlias.Use(rt.authMiddleware.LoginGate(), middleware.DownloadRateLimit())
	{
		downloadAlias.GET("/*path", rt.downloadHandlers.DownloadFile)
		downloadAlias.HEAD("/*path", rt.downloadHandlers.DownloadFile)
	}

	// WebDAV 路由别名（/webdav 作为 /api/v1/webdav 的快捷入口）
	// 仅在配置启用 WebDAV 时注册，保持与原路由一致的行为
	if rt.cfg.Server.WebDAV.Enabled {
		davHandler, authenticate := rt.buildWebDAVHandler(constants.WebDAVAliasPathPrefix)
		// 注意：别名路径前缀必须显式指定为 /webdav，否则 WebDAV 协议内部资源定位会出错
		webdav.SetupUnifiedRouter(
			r.Group(constants.WebDAVAliasPathPrefix),
			davHandler,
			authenticate,
			rt.cfg.Upload.MaxFileSize,
		)
		utils.Info("WebDAV 别名端点已启用", utils.String("path", constants.WebDAVAliasPathPrefix))
	}

	// 轻量版入口（开启「强制登录」时需认证）：服务器渲染的纯 HTML 列表，兼容老旧浏览器，
	// 提供目录进入与文件下载链接，不含样式/上传/其它操作。
	// 认证入口放了白名单（匿名可访问）：登录/注册 GET 渲染与 POST 提交，以及登出 POST。
	// 注意：gin 不允许同一前缀下静态段与 catch-all 共存（/lite/login 与 /lite/*path 冲突），
	// 因此登录/注册/登出与浏览检索统一经 /*path 接收，由 HandleAll 内部按 path+method 分发。
	// 且必须同时注册 GET 与 POST 两个 catch-all：缺少 POST 时，登录/注册/登出的 POST 请求
	// 会因无匹配路由掉进 NoRoute（SPA fallback）而错误返回 Vue 页面。
	lite := r.Group("/lite")
	lite.Use(rt.authMiddleware.LoginGate(litepkg.LiteAuthWhiteList()...))
	lite.GET("/*path", rt.liteHandlers.HandleAll)
	lite.POST("/*path", rt.liteHandlers.HandleAll)

	// Open API 路由组（v3 Phase 3）
	// 架构要求中间件链: IP白名单 → 功能开关 → 频率限制 → 鉴权 → 调用统计 → 路由分发 → Handler
	// 鉴权说明（L6 加固）：任何 auth_mode 都要求身份校验，不再匿名放行。
	//   - auth_mode=api_key：校验系统签发的 API Key（携带 scope），无 Key/无效一律 401。
	//   - auth_mode=none：要求有效凭证（Web 会话 JWT——Cookie 或 Bearer——或 API Key），匿名 401。
	// 强制登录（account.require_login）最高：none 模式下 JWT 会话复用 AuthMiddleware 的实时禁用态校验。
	// 两个同前缀组（数据端点 / 文档）共用基础链：功能开关 → IP 白名单 → 频率限制。
	openAPICommonMW := []gin.HandlerFunc{
		openapi.FeatureGateMiddleware(rt.openAPICfg.Enabled),
		openapi.IPAccessMiddleware(rt.openAPIIPAccessCfg),
		openapi.RateLimitMiddleware(rt.openAPIRateLimiter),
	}

	openAPI := r.Group("/api/open/v1")
	openAPI.Use(openAPICommonMW...)
	openAPI.Use(
		openapi.AuthGateMiddleware(rt.authMiddleware, rt.rbacService),
		openapi.CallStatMiddleware(rt.openAPICallStat),
		middleware.ResponseWrapperMiddleware(),
	)
	{
		// FR-3.1: 文件列表 API（需认证；api_key 模式下需 open_api:reader，Web 会话 JWT 无该 scope 时 403）
		openAPI.GET("/files/list", openapi.RequireReaderScope(rt.rbacService), rt.openAPIHandler.ListFiles)
		// FR-3.2: 文件搜索 API（需认证；api_key 模式下需 open_api:reader，Web 会话 JWT 无该 scope 时 403）
		openAPI.GET("/files/search", openapi.RequireReaderScope(rt.rbacService), rt.openAPIHandler.SearchFiles)
		// FR-3.3: 文件下载 API（需认证；api_key 模式下需 open_api:reader，Web 会话 JWT 无该 scope 时 403）
		openAPI.GET("/files/download/*path", openapi.RequireReaderScope(rt.rbacService), rt.openAPIHandler.DownloadFile)
		// 文件详情 API（需认证，需要文件详情查询权限；reader/writer 层级同样覆盖）
		openAPI.GET("/files/info", rt.rbacService.RequireScope(models.ScopeFilesInfo), rt.openAPIHandler.GetFileInfo)
		// 文件上传 API（需认证，需要文件上传权限；仅 writer/admin 层级覆盖）
		openAPI.POST("/files/upload", rt.rbacService.RequireScope(models.ScopeFilesUpload), rt.openAPIHandler.UploadFile)
		// 分片上传 API：创建会话 → 上传分片 → 完成合并（权限同上）
		openAPI.POST("/files/upload/init", rt.rbacService.RequireScope(models.ScopeFilesUpload), rt.openAPIHandler.InitChunkUpload)
		openAPI.POST("/files/upload/chunk", rt.rbacService.RequireScope(models.ScopeFilesUpload), rt.openAPIHandler.UploadFileChunk)
		openAPI.POST("/files/upload/finalize", rt.rbacService.RequireScope(models.ScopeFilesUpload), rt.openAPIHandler.FinalizeChunkUpload)
		openAPI.GET("/files/upload/:id", rt.rbacService.RequireScope(models.ScopeFilesUpload), rt.openAPIHandler.GetChunkUploadStatus)
		// FR-3.5: 重复文件查询 API（需认证，需要重复文件查询权限）
		openAPI.GET("/files/duplicates", rt.rbacService.RequireScope(models.ScopeFilesDuplicates), rt.openAPIHandler.ListDuplicates)
		// FR-3.6: 文件备注查询 API（需认证，需要文件备注查询权限）
		openAPI.GET("/files/notes", rt.rbacService.RequireScope(models.ScopeFilesNotes), rt.openAPIHandler.GetNotes)
		// FR-4.1: 文件依赖树查询 API（需认证，需要文件依赖树查询权限）
		openAPI.GET("/files/:id/depends", rt.rbacService.RequireScope(models.ScopeFilesDepends), rt.openAPIHandler.GetDependencyTree)
		openAPI.GET("/docs", rt.openAPIHandler.GetDocsPage)
	}

	// OpenAPI 文档：从组级 AuthGate 中单独抽出，改走 LoginRequiredWhenForced。
	// L6 加固后与 AuthGate 语义一致：none 模式需有效 Web 会话 JWT 或 API Key，匿名 401；
	// api_key 模式下要求 API Key（不开新洞）。
	openapiDoc := r.Group("/api/open/v1")
	openapiDoc.Use(openAPICommonMW...)
	openapiDoc.Use(
		openapi.LoginRequiredWhenForced(rt.authMiddleware, rt.rbacService),
		openapi.CallStatMiddleware(rt.openAPICallStat),
		middleware.ResponseWrapperMiddleware(),
	)
	openapiDoc.GET("/openapi.json", rt.openAPIHandler.GetOpenAPISpec)
}
