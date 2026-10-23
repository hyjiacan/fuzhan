package main

import (
	"net/http"

	"fuzhan/internal/access/webdav"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/middleware"
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
	downloadAlias.Use(rt.authMiddleware.LoginGate())
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
	lite := r.Group("/lite")
	lite.Use(rt.authMiddleware.LoginGate())
	lite.GET("/*path", rt.liteHandlers.Handle)

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
