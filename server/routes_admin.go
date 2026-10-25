package main

import (
	"github.com/gin-gonic/gin"
)

// registerAdminRoutes 注册 /api/v1/web/admin 下需要管理员权限的路由
func (rt *runCtx) registerAdminRoutes(admin *gin.RouterGroup) {
	adminHandler := rt.adminHandler
	fileHandlers := rt.fileHandlers
	adminURLDownloadHandler := rt.adminURLDownloadHandler
	taskHandler := rt.taskHandler
	apiKeyHandler := rt.apiKeyHandler
	indexHandler := rt.indexHandler
	depHandler := rt.depHandler
	setupHandler := rt.setupHandler

	// 全量/触发扫描（需管理员鉴权，防止匿名资源耗尽 DoS）
	admin.POST("/index/scan", indexHandler.TriggerScan)
	admin.POST("/index/scan/trigger", indexHandler.TriggerFullScan)
	admin.GET("/users", adminHandler.UsersHandler)
	admin.POST("/users", adminHandler.CreateUserHandler)
	admin.PUT("/users/:uuid/reset-password", adminHandler.ResetPasswordHandler)
	admin.PUT("/users/:uuid/disabled", adminHandler.SetUserDisabledHandler)
	admin.DELETE("/users/:uuid", adminHandler.DeleteUserHandler)
	admin.GET("/sessions", adminHandler.SessionsHandler)
	admin.POST("/sessions/cleanup", adminHandler.CleanupSessionsHandler)
	admin.POST("/sessions/cleanup-all", adminHandler.CleanupAllZombieSessionsHandler)

	// 在线 IP 统计路由（与登录无关，依据最近请求判定在线）
	admin.GET("/online-ips", adminHandler.OnlineIPs)

	// 操作记录清空/删除路由
	admin.POST("/records/clear", adminHandler.ClearRecordsHandler)
	admin.POST("/records/delete", adminHandler.DeleteRecordHandler)

	// 安全事件分析路由（攻击/越权/异常请求 记录查看与分析，仅管理员）
	if sh := rt.securityHandler; sh != nil {
		admin.GET("/security/events", sh.ListEvents)
		admin.GET("/security/events/unread-summary", sh.UnreadSummary)
		admin.POST("/security/events/read", sh.MarkRead)
		admin.GET("/security/events/stats", sh.Stats)
	}

	// 行为分析路由（下载行为 /admin/behavior-analytics，基于 operation_records 公开下载聚合）
	if h := rt.behaviorAnalyticsHandler; h != nil {
		admin.GET("/behavior-analytics/summary", h.SummaryHandle)
		admin.GET("/behavior-analytics/trend", h.TrendHandle)
		admin.GET("/behavior-analytics/top-files", h.TopFilesHandle)
		admin.GET("/behavior-analytics/sources", h.SourcesHandle)
		admin.GET("/behavior-analytics/failures", h.FailuresHandle)
		admin.GET("/behavior-analytics/file", h.FileDetailHandle)
		admin.GET("/behavior-analytics/heatmap", h.HeatmapHandle)
		admin.GET("/behavior-analytics/aggregate", h.AggregateHandle)
		admin.GET("/behavior-analytics/lifecycle", h.LifecycleHandle)
	}

	// TLS 证书上传路由
	admin.POST("/upload-cert", adminHandler.UploadCertHandler)
	admin.POST("/upload-key", adminHandler.UploadKeyHandler)

	// 网络接口列表（初始化后由设置页使用，需管理员鉴权）
	admin.GET("/network-interfaces", setupHandler.GetNetworkInterfaces)

	// 文件管理路由
	admin.GET("/files/list", fileHandlers.ListDirectories)
	admin.POST("/files/move", fileHandlers.MoveFileHandler)
	admin.DELETE("/files", fileHandlers.DeleteFileHandler)

	// URL 下载任务管理路由
	admin.GET("/url-tasks", adminURLDownloadHandler.ListURLTasks)
	admin.POST("/url-tasks/:id/retry", adminURLDownloadHandler.RetryURLTask)
	admin.DELETE("/url-tasks/:id", adminURLDownloadHandler.DeleteURLTask)

	// 系统作业路由
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

	// 日志查看（后端日志文件列表与增量读取）
	if lh := rt.logHandler; lh != nil {
		admin.GET("/logs/files", lh.Files)
		admin.GET("/logs/tail", lh.Tail)
	}

	// 服务器资源监控
	resourceRoutes := admin.Group("/resource")
	{
		resourceRoutes.GET("/snapshot", rt.resourceHandler.Snapshot)
		resourceRoutes.GET("/history", rt.resourceHandler.History)
		// SSE 实时推送：前端使用 fetch 流式 + Authorization 请求头解析 data: 帧（EventSource 无法携带请求头）
		resourceRoutes.GET("/stream", rt.resourceHandler.Stream)
	}
}
