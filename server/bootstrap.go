package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/accessguard"
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
	"fuzhan/internal/user"
	"fuzhan/internal/utils"
	"fuzhan/internal/webhook"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// bootstrap 依次完成启动阶段 1～4（配置加载 → 数据库与迁移 → 服务与 Handler 组装 →
// 路由上下文汇总），返回组装好的 runCtx 供主循环使用。
// createAdmin 非空时在完成基础设施初始化后创建/更新管理员并直接返回（不做路由）。
func bootstrap(configPath, createAdmin string) (*runCtx, bool) {
	// ===== 启动阶段1: 配置加载 =====
	utils.Info("========================================")
	utils.Info("[启动] 阶段1: 配置加载")
	cfg := appconfig.DoInitWithConfig(webAssets, configPath)

	// ===== 启动阶段2: 数据库连接 =====
	utils.Info("[启动] 阶段2: 数据库连接")
	db, err := appconfig.InitDB(&cfg.Database)
	if err != nil {
		utils.Fatal("数据库初始化失败", utils.Err(err))
	}
	appconfig.SetDB(db)

	// 初始化 JWT 密钥
	// 先设置 getter 避免循环导入（始终从 GlobalConfig 读取，确保热重载后一致性）
	jwt.SetJWTSecretGetter(func() string {
		cfg := appconfig.GetConfig()
		return cfg.Server.JWTSecret
	})
	// 若既未配置 JWT 密钥也未通过环境变量指定，则生成随机密钥并持久化到配置，
	// 确保重启后签名密钥不变（避免已签发 token 每次重启后全部失效）
	ensureJWTPersisted()
	if err := jwt.InitJWTSecret(); err != nil {
		utils.Fatal("JWT 密钥初始化失败", utils.Err(err))
	}

	// 设置安全配置获取器（避免 utils 和 config 循环导入）
	utils.SetSecurityConfigGetter(func() utils.SecurityConfig {
		sec := appconfig.GetSecurityConfig()
		return utils.SecurityConfig{
			TrustProxy:     sec.TrustProxy,
			AllowedOrigins: sec.AllowedOrigins,
		}
	})

	// 自动迁移数据库表（创建表结构，未发版前不做结构迁移）
	db.AutoMigrate(&models.UploadSession{}, &models.UploadedChunk{}, &models.OperationRecord{}, &models.User{}, &models.TempFile{}, &models.URLDownloadTask{}, &models.FileRecordPublic{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{}, &models.FileDependency{}, &models.ApiKey{}, &models.OAuthClient{}, &models.AuthRecord{}, &models.ScanRecord{}, &models.TaskRecord{}, &models.ResourceMetric{}, &models.SecurityEvent{}, &models.LoginLog{})

	// 为 file_records_public/temp/private 统一创建索引（命名格式：idx__{table}__{col1}_{col2}_...）
	models.EnsureFileRecordIndexes(db)
	// 为操作记录表创建组合索引（最近/热门列表按 action + created_at 过滤排序）
	models.EnsureOperationRecordIndexes(db)
	// 幂等清理 operation_records 历史冗余索引（须在 EnsureOperationRecordIndexes 之后，确保保留索引已建好）
	models.CleanupOperationRecordIndexes(db)
	// 为安全事件表创建索引
	models.EnsureSecurityEventIndexes(db)

	// 回填公共文件下载次数（从下载记录统计，升级/启动时执行一次）
	services.BackfillPublicDownloadCounts(db)
	// 回填历史操作记录的 file_record_id（建立身份关联，移动/重命名后不丢）
	services.BackfillFileRecordIDs(db)

	// 回填下载来源身份（source_type/source_id），为行为分析跨来源聚合提供一致身份
	services.BackfillOperationRecordSource(db)

	// 回填下载统计签名（dl_event/dl_ok），供行为分析等值过滤并命中覆盖索引
	services.BackfillDownloadSignature(db)

	// 回填下载计数签名（dl_counted）：该列首次引入时补齐存量记录。补齐后索引表里的
	// download_count 只是历史累加值，需按记录集全量对齐一次（幂等）才能与记录唯一对应；
	// 未发生补齐（列已是最新）时跳过，避免每次启动都做全量重算。
	if services.BackfillDownloadCounted(db) > 0 {
		services.RecountAllPublicDownloadCounts(db)
	}

	// 初始化日志记录器 (已在 appconfig.DoInit 中初始化)
	utils.Sync()

	// 初始化监控指标
	utils.InitMetrics()

	// 处理命令行创建管理员（在所有初始化完成后执行，确保日志、监控等基础设施就绪）
	if createAdmin != "" {
		createAdminUser(db, createAdmin)
		return nil, true
	}

	// 初始化验证器
	validator := appconfig.NewValidator()

	// ===== 启动阶段3: 服务初始化 =====
	utils.Info("[启动] 阶段3: 服务初始化")

	// 构建公开多根目录的存储后端映射（rootName → Storage）。
	// 本地后端：rootPath 为磁盘根目录；远端后端：rootPath 为虚拟前缀。
	publicStorages, storErr := buildPublicStorages(&cfg)
	if storErr != nil {
		return nil, true
	}

	// 使用 ServiceBuilder 明确服务依赖顺序
	serviceDeps := services.BuildAllServices(
		db,
		appconfig.RootNames,
		cfg.Storage.Temp.Path,
		cfg.Storage.Private.Path,
		publicStorages,
	)

	// 从构建结果中获取服务实例
	recordRepo := serviceDeps.RecordRepo
	indexService := serviceDeps.IndexService
	fileService := serviceDeps.FileService
	downloadService := serviceDeps.DownloadService
	searchService := serviceDeps.SearchService
	authService := serviceDeps.AuthService

	// 初始化全局有界操作记录器（固定 worker + 有界缓冲），
	// 让下载/搜索/FTP 等记录点都走有界异步，避免每次操作起新协程造成瞬态尖峰
	services.InitRecordWriter(recordRepo, services.RecordWriterWorkers, services.RecordWriterBuffer)
	// 初始化全局有界安全事件写入器（攻击场景事件可能成片爆发，走有界异步不阻塞被拒响应）
	secRepo := repositories.NewSecurityEventRepository(db)
	security.InitWriter(secRepo, security.WriterWorkers, security.WriterBuffer)
	// 注入共享/临时码/口令暴破锁定回调：命中的防护锁定写入安全事件（高危）
	accessguard.SetFailHook(func(scope, ip string) {
		// WebDAV 锁定源于 Basic Auth 失败，操作归一为 auth.login；其余 scope 为下载类
		operation := "download." + scope
		if scope == accessguard.SCOPE_WEBDAV {
			operation = "auth.login"
		}
		security.Record(&models.SecurityEvent{
			EventType: models.EventCodeBruteForce,
			Severity:  models.SeverityHigh,
			ClientIP:  ip,
			Identity:  "anonymous",
			Operation: operation,
			Target:    scope,
			Guard:     security.GuardForScope(scope),
			Reason:    security.FailLockReason(scope),
		})
	})
	// 初始化 DDoS 高频防护检测器（默认开启，超阈值自动限流拦截；未启用仅日志提示），配置保存后热启停
	security.InitDDoSDetector()
	appconfig.AddOnSaveHook(security.SyncDDoSDetector)
	// 初始化下载计数批量记录器：下载热路径只入队（非阻塞），后台周期攒批批量落库，
	// 消除每次下载同步 UPDATE 索引表造成的写放大与请求线程阻塞
	services.InitDownloadCountRecorder(db)

	// 初始化 Webhook 派发器：进程内有界队列异步推送关键事件（任务/安全/配置变更）。
	// 未启用时 Send 静默丢弃，零开销；配置保存后由钩子同步启用开关支持热启停。
	webhook.Init(cfg.Webhook)
	webhook.Get().Start()
	appconfig.AddOnSaveHook(func() {
		webhook.Sync(appconfig.GetWebhookConfig())
	})

	// LDAP 认证服务（v3 Phase 2）
	ldapService := auth.NewLDAPService(db, &auth.LDAPConfig{
		Enabled:            cfg.Auth.LDAP.Enabled,
		Host:               cfg.Auth.LDAP.Host,
		Port:               cfg.Auth.LDAP.Port,
		UseSSL:             cfg.Auth.LDAP.UseSSL,
		BaseDN:             cfg.Auth.LDAP.BaseDN,
		BindDN:             cfg.Auth.LDAP.BindDN,
		BindPassword:       cfg.Auth.LDAP.BindPassword,
		UserFilter:         cfg.Auth.LDAP.UserFilter,
		SyncInterval:       cfg.Auth.LDAP.SyncInterval,
		AutoCreateUser:     cfg.Auth.LDAP.AutoCreateUser,
		InsecureSkipVerify: cfg.Auth.LDAP.InsecureSkipVerify,
	})

	// 将 LDAP 服务注入 AuthService（无论初始是否启用，都注入以便配置保存后热启用）
	authService.SetLDAPService(ldapService)
	if ldapService.IsEnabled() {
		utils.Info("LDAP 认证已启用", utils.String("host", cfg.Auth.LDAP.Host))
	} else {
		utils.Info("LDAP 认证未启用")
	}

	// 配置保存后热更新 LDAP 配置（启用/地址/过滤器等修改无需重启；目录同步间隔由 worker 每周期重读）
	appconfig.AddOnSaveHook(func() {
		cur := appconfig.GetConfig().Auth.LDAP
		ldapService.UpdateConfig(&auth.LDAPConfig{
			Enabled:            cur.Enabled,
			Host:               cur.Host,
			Port:               cur.Port,
			UseSSL:             cur.UseSSL,
			BaseDN:             cur.BaseDN,
			BindDN:             cur.BindDN,
			BindPassword:       cur.BindPassword,
			UserFilter:         cur.UserFilter,
			SyncInterval:       cur.SyncInterval,
			AutoCreateUser:     cur.AutoCreateUser,
			InsecureSkipVerify: cur.InsecureSkipVerify,
		})
	})

	// 外部 OAuth2/OIDC 认证服务（配置保存后热更新，无需重启）
	oauth2Service := auth.NewOAuth2Service(db, oauth2ConfigFromApp(cfg.Auth.OAuth2))
	if oauth2Service.IsEnabled() {
		utils.Info("OAuth2 外部认证已启用", utils.String("provider", cfg.Auth.OAuth2.ProviderName))
	} else {
		utils.Info("OAuth2 外部认证未启用")
	}
	appconfig.AddOnSaveHook(func() {
		oauth2Service.UpdateConfig(oauth2ConfigFromApp(appconfig.GetConfig().Auth.OAuth2))
	})

	// 创建认证中间件（实时查库检查用户禁用状态）
	authMiddleware := middleware.NewAuthMiddleware(db)

	// 创建处理器
	baseHandler := file.NewBaseHandler(&cfg, indexService, recordRepo, publicStorages)
	fileHandlers := file.NewFileHandlers(baseHandler, fileService, db)
	downloadHandlers := file.NewDownloadHandlers(downloadService)
	searchHandlers := file.NewSearchHandlers(searchService, recordRepo)
	cliHandlers := cli.NewCLIHandlers(fileService, searchService)
	bindingExampleHandler := configh.NewBindingExampleHandler()
	authHandler := auth.NewHandler(authService, db)
	healthHandler := health.NewHealthHandler(db)
	setupHandler := setup.NewSetupHandler()
	configHandler := configh.NewHandler()

	// 上传会话处理器
	chunkSize := cfg.Upload.ChunkSize
	uploadSessionSvc := services.NewUploadSessionService(db, chunkSize, indexService).WithPublicStorages(publicStorages)
	taskService := services.NewTaskService(db)
	// 启动对账：先把上一进程遗留的 pending/running 任务收尾为"进程重启中断"，
	// 再启动扫描/哈希等任务，避免系统作业页残留"运行中"僵死记录。
	// 在此处调用安全：此时本进程尚未创建任何任务，DB 的 task_records 表已建。
	if _, rerr := taskService.ReconcileInterruptedTasks(); rerr != nil {
		utils.Warn("启动对账失败", utils.Err(rerr))
	}
	// 启动对账：把上一进程遗留的 downloading 任务重新入队为 pending，
	// 并清理不支持续传的孤儿临时文件，使重启后由后台队列继续执行。
	urlTaskRepo := repositories.NewURLDownloadTaskRepository(db)
	requeued, rerr := urlTaskRepo.RequeueInterrupted()
	if rerr != nil {
		utils.Warn("URL上传任务启动对账失败", utils.Err(rerr))
	} else {
		for _, tk := range requeued {
			if tk.TempFilePath == "" || tk.ResumeSupported {
				continue
			}
			if rmErr := os.Remove(tk.TempFilePath); rmErr != nil && !os.IsNotExist(rmErr) {
				utils.Warn("清理URL上传孤儿文件失败",
					utils.String("task_id", tk.ID),
					utils.String("path", tk.TempFilePath),
					utils.Err(rmErr))
				// 删除失败则保留进度，避免进度与残留文件不一致
				continue
			}
			// 半截文件已清理且不支持续传，同步清零进度，避免重启后进度与实际不符
			if perr := urlTaskRepo.UpdateProgress(tk.ID, 0); perr != nil {
				utils.Warn("重置URL上传任务进度失败",
					utils.String("task_id", tk.ID), utils.Err(perr))
			}
		}
		if len(requeued) > 0 {
			utils.Info("URL上传任务启动对账: 重新入队并清理孤儿文件", utils.Int("count", len(requeued)))
		}
	}
	uploadSessionHandler := file.NewUploadSessionHandler(uploadSessionSvc, db, chunkSize, recordRepo, indexService, taskService)

	// URL 上传后台队列调度器：全局并行上限来自配置，超出即按入队顺序排队；
	// 任务持久化在 url_download_tasks，关闭浏览器乃至进程重启后仍可继续执行。
	urlDispatcher := file.NewURLDownloadDispatcher(urlTaskRepo, uploadSessionHandler, cfg.Upload.URLUpload.MaxConcurrent)
	uploadSessionHandler.SetDispatcher(urlDispatcher)
	urlDispatcher.Start()
	// 配置保存后唤醒调度器：后台调大「并行上传数」无需等待任何事件即可立即生效
	appconfig.AddOnSaveHook(urlDispatcher.Notify)
	for range requeued {
		urlDispatcher.Notify()
	}
	privateUploadHandler := file.NewPrivateUploadHandler(db, chunkSize, cfg.Storage.Private.Path, indexService)
	privateStorageHandler := file.NewPrivateStorageHandlers()
	adminService := services.NewAdminService(db)
	// 行为分析服务提前创建：既供统计接口使用，也供记录删除后主动失效缓存
	behaviorAnalyticsService := behavioranalytics.NewService(db)
	adminHandler := admin.NewHandler(adminService, db, behaviorAnalyticsService)
	notificationHandler := notification.NewHandler(db)
	securityHandler := security.NewHandler(secRepo)
	adminURLDownloadHandler := admin.NewURLDownloadHandler(db)
	// 注入调度器钩子：管理员重试可立即唤醒队列，删除可中断执行中的下载
	adminURLDownloadHandler.SetTaskHooks(urlDispatcher.Notify, urlDispatcher.CancelTask)
	taskHandler := admin.NewTaskHandler(taskService)
	logHandler := admin.NewLogHandler()
	// 将任务记录服务注入索引服务，使扫描/哈希等任务自动写入 task_records
	indexService.SetTaskService(taskService)
	// 将公开多根目录的存储后端注入索引服务（远端后端供扫描/哈希/一致性校验/增量同步使用；
	// 本地后端不注入时保持 rootNames 物理路径语义）
	indexService.SetStorages(publicStorages)

	// ===== 文件名检索索引（自动补全 / 拼写纠错）=====
	// 数据存放于 <data>/search_index，用于 HomeView 搜索框的下拉联想与纠错。
	var idxSearch *search.SearchIndex
	idx, err := search.OpenIndex(filepath.Join(appconfig.GetDataDir(), "search_index"))
	if err != nil {
		utils.Warn("文件名检索索引初始化失败，搜索联想/纠错功能不可用", utils.Err(err))
	} else {
		utils.Info("文件名检索索引已打开", utils.String("dir", filepath.Join(appconfig.GetDataDir(), "search_index")))
		if rerr := idx.RebuildFromDB(db); rerr != nil {
			utils.Warn("文件名检索索引全量构建失败，将依赖后续增量同步", utils.Err(rerr))
		} else {
			utils.Info("文件名检索索引全量构建完成")
		}
		// 文件增/删/改后增量同步检索索引
		indexService.SetFileIndexNotifier(idx)
		idxSearch = idx
	}
	// 文件名检索提示处理器（索引未打开时为 nil，搜索联想/纠错不可用）
	suggestHandler := search.NewHandler(idxSearch)
	// /lite 轻量版页面检索的推荐/纠错同样依赖检索索引
	liteHandlers := lite.NewHandlers(searchService, authService, db)
	liteHandlers.SearchIndex = idxSearch

	// 临时文件处理器 (基于IP，无需认证)
	tempSvcConfig := services.TempServiceConfig{
		Path:              cfg.Storage.Temp.Path,
		Enabled:           cfg.Storage.Temp.Enabled,
		DefaultExpireDays: cfg.Storage.Temp.DefaultExpireDays,
		DeleteOnDownload:  cfg.Storage.Temp.DeleteOnDownload,
	}
	tempService := services.NewTempFileServiceWithConfig(db, tempSvcConfig)
	tempHandler := temph.NewHandler(db, cfg.Storage.Temp, tempService, indexService)
	if !cfg.Storage.Temp.Enabled {
		utils.Info("临时文件功能未启用")
	}

	// 文件索引 Handler（v3 Phase 0）
	indexHandler := index.NewHandler(indexService)

	// 文件依赖服务（v3 Phase 1）
	depService := index.NewDependencyService(db)
	depHandler := index.NewDependencyHandler(depService)

	// API Key 服务（v3 Phase 2）
	apiKeyService := auth.NewApiKeyService(db)
	apiKeyHandler := auth.NewApiKeyHandler(apiKeyService)
	rbacService := auth.NewRBACService(apiKeyService) // Open API 认证中间件

	// Open API 服务（v3 Phase 3）
	openAPICfg := &appconfig.GlobalConfig.OpenAPI
	openAPIHandler := openapi.NewHandler(db, indexService, depService, appconfig.RootNames)
	openAPIRateLimiter := openapi.NewRateLimiter(openapi.NewRateLimitConfigFromAppConfig(
		openAPICfg.RateLimitEnabled,
		openAPICfg.RequestsPerMinute,
		openAPICfg.BurstSize,
	))
	openAPIIPAccessCfg := openapi.NewIPAccessConfigFromAppConfig(
		openAPICfg.IPAccessMode,
		openAPICfg.IPWhitelist,
		openAPICfg.IPBlacklist,
		openAPICfg.DomainWhitelist,
	)
	// 配置保存后热更新 OpenAPI IP/域名访问控制（无需重启即生效）
	appconfig.AddOnSaveHook(func() {
		cur := appconfig.GetConfig().OpenAPI
		openAPIIPAccessCfg.UpdateConfig(openapi.NewIPAccessConfigFromAppConfig(
			cur.IPAccessMode,
			cur.IPWhitelist,
			cur.IPBlacklist,
			cur.DomainWhitelist,
		))
	})
	openAPICallStat := openapi.NewAPICallStat(10000)
	openAPIStatsHandler := openapi.NewStatsHandler(openAPICallStat)

	// 最近上传记录处理器 (无需认证)
	recentHandler := file.NewRecentHandler(recordRepo, db)

	// 个人中心处理器（登录/操作记录等，需认证）
	userHandler := user.NewHandler(recordRepo, db)

	// 系统监测处理器 (无需认证)
	monitorService := services.NewMonitorService(recordRepo, db)
	monitorHandler := monitor.NewHandler(monitorService)

	// 行为分析处理器（需管理员认证；基于 operation_records 公开下载聚合）
	behaviorAnalyticsHandler := behavioranalytics.NewHandler(behaviorAnalyticsService)

	// 服务器资源监控（需管理员认证；worker 在下方启动）
	resourceCollector := resource.NewCollector(func() *gorm.DB { return appconfig.GetDB() })
	resourceHandler := resource.NewHandler(resourceCollector)

	// 预览处理器 (无需认证)
	previewHandler := file.NewPreviewHandler(baseHandler)

	// 汇总运行期依赖，供路由注册与 worker 使用
	rt := &runCtx{
		db:                       db,
		cfg:                      &cfg,
		validator:                validator,
		recordRepo:               recordRepo,
		indexService:             indexService,
		fileService:              fileService,
		downloadService:          downloadService,
		searchService:            searchService,
		authService:              authService,
		taskService:              taskService,
		baseHandler:              baseHandler,
		fileHandlers:             fileHandlers,
		downloadHandlers:         downloadHandlers,
		searchHandlers:           searchHandlers,
		cliHandlers:              cliHandlers,
		liteHandlers:             liteHandlers,
		bindingExampleHandler:    bindingExampleHandler,
		configHandler:            configHandler,
		authHandler:              authHandler,
		healthHandler:            healthHandler,
		setupHandler:             setupHandler,
		uploadSessionHandler:     uploadSessionHandler,
		privateUploadHandler:     privateUploadHandler,
		privateStorageHandler:    privateStorageHandler,
		adminHandler:             adminHandler,
		securityHandler:          securityHandler,
		notificationHandler:      notificationHandler,
		adminURLDownloadHandler:  adminURLDownloadHandler,
		taskHandler:              taskHandler,
		logHandler:               logHandler,
		tempHandler:              tempHandler,
		indexHandler:             indexHandler,
		depHandler:               depHandler,
		apiKeyHandler:            apiKeyHandler,
		openAPIHandler:           openAPIHandler,
		suggestHandler:           suggestHandler,
		recentHandler:            recentHandler,
		userHandler:              userHandler,
		monitorHandler:           monitorHandler,
		resourceHandler:          resourceHandler,
		previewHandler:           previewHandler,
		behaviorAnalyticsHandler: behaviorAnalyticsHandler,
		authMiddleware:           authMiddleware,
		rbacService:              rbacService,
		openAPICfg:               openAPICfg,
		openAPIRateLimiter:       openAPIRateLimiter,
		openAPIIPAccessCfg:       openAPIIPAccessCfg,
		openAPICallStat:          openAPICallStat,
		openAPIStatsHandler:      openAPIStatsHandler,
		idxSearch:                idxSearch,
		resourceCollector:        resourceCollector,
		ldapService:              ldapService,
		publicStorages:           publicStorages,
	}

	// ===== 启动阶段4: 路由设置 =====
	utils.Info("[启动] 阶段4: 路由设置完成")

	return rt, false
}

// buildPublicStorages 根据配置的存储后端为每个公开根目录创建对应的 Storage 实例。
// 本地后端直接使用物理根路径；远端后端使用虚拟前缀 + 连接配置。
func buildPublicStorages(cfg *appconfig.Config) (map[string]storage.Storage, error) {
	storages := make(map[string]storage.Storage, len(appconfig.RootNames))

	conn := storage.ConnConfig{
		Endpoint:       cfg.Storage.S3.Endpoint,
		AccessKey:      cfg.Storage.S3.AccessKey,
		SecretKey:      cfg.Storage.S3.SecretKey,
		Bucket:         cfg.Storage.S3.Bucket,
		Region:         cfg.Storage.S3.Region,
		UseSSL:         cfg.Storage.S3.UseSSL,
		WebDAVURL:      cfg.Storage.WebDAV.URL,
		WebDAVUsername: cfg.Storage.WebDAV.Username,
		WebDAVPassword: cfg.Storage.WebDAV.Password,
	}

	for rootName, rootPath := range appconfig.RootNames {
		st, err := storage.NewStorageFromConfig(cfg.Storage.Backend, rootPath, conn)
		if err != nil {
			utils.Error("创建存储后端失败",
				utils.String("root", rootName),
				utils.String("backend", cfg.Storage.Backend),
				utils.Err(err))
			return nil, err
		}
		storages[rootName] = st
	}

	if appconfig.StorageBackendIsRemote() {
		utils.Info("存储后端已切换为远端模式",
			utils.String("backend", cfg.Storage.Backend),
			utils.Int("roots", len(storages)))
	}
	return storages, nil
}

// createAdminUser 创建或更新管理员用户（-create-admin 命令行入口）
func createAdminUser(db *gorm.DB, spec string) {
	parts := splitAdminSpec(spec)
	username, password := parts[0], parts[1]

	// 命令行设密属于「设密」场景：非空口令需遵循最少 8 位的新策略
	if password != "" && (len(password) < 8 || len(password) > 128) {
		utils.Fatal("密码长度需在8到128个字符之间")
	}

	// 检查是否已存在
	var existing models.User
	if err := db.Where("username = ?", username).First(&existing).Error; err == nil {
		// 更新为管理员
		existing.Role = "admin"
		if password != "" {
			hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				utils.Error("密码哈希生成失败", utils.Err(err))
				return
			}
			existing.PasswordHash = string(hashed)
		}
		if err := db.Save(&existing).Error; err != nil {
			utils.Error("更新管理员用户失败", utils.String("username", username), utils.Err(err))
			return
		}
		utils.Info("用户已更新为管理员", utils.String("username", username))
	} else {
		// 创建新管理员
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			utils.Error("密码哈希生成失败", utils.Err(err))
			return
		}
		uuid, err := utils.GenerateUUID()
		if err != nil {
			utils.Error("UUID生成失败", utils.Err(err))
			return
		}
		user := &models.User{
			UUID:         uuid,
			Username:     username,
			PasswordHash: string(hashed),
			Role:         "admin",
		}
		if err := db.Create(user).Error; err != nil {
			utils.Error("创建管理员用户失败", utils.String("username", username), utils.Err(err))
			return
		}
		utils.Info("管理员用户创建成功", utils.String("username", username))
	}
}

// splitAdminSpec 解析 -create-admin 的 username:password 规格
func splitAdminSpec(spec string) []string {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		utils.Fatal("创建管理员格式错误，应为 username:password")
	}
	return parts
}

// printStartupSummary 输出监听地址与共享目录列表（启动信息）
func printStartupSummary(cfg *appconfig.Config) {
	// 服务器地址
	host := cfg.Server.Host
	if host == "" {
		host = "0.0.0.0"
	}
	address := fmt.Sprintf("%s:%d", host, cfg.Server.HTTP.Port)
	utils.Info("监听地址", utils.String("address", address))

	// 输出共享目录列表
	utils.Info("共享目录列表", utils.Int("count", len(appconfig.RootNames)))
	for _, name := range sortedKeys(appconfig.RootNames) {
		path := appconfig.RootNames[name]
		quota := "-"
		for _, d := range cfg.Storage.Public.RootDirs {
			if d.Path == path {
				if d.Quota > 0 {
					quota = fmt.Sprintf("%d", d.Quota)
				}
				break
			}
		}
		utils.Info("  "+name, utils.String("path", path), utils.String("quota", quota))
	}

	// 检查未初始化状态
	if !cfg.App.Initialized {
		configPath := appconfig.ConfigPath
		if configPath == "" {
			configPath = "fuzhan.yaml"
		}
		utils.Info("系统尚未初始化，请完成配置向导")
		utils.Info("方式一: 打开浏览器访问", utils.String("url", fmt.Sprintf("http://localhost:%d/#/setup", cfg.Server.HTTP.Port)))
		utils.Info("方式二: 手动编辑配置文件", utils.String("path", configPath))
	}
	// 检查端口是否被占用
	if appconfig.IsPortInUse(cfg.Server.HTTP.Port) {
		utils.Fatal("启动失败：端口已被占用", utils.Int("port", cfg.Server.HTTP.Port))
	}
}

// cleanupTimeout 关闭收尾（关库 + 刷日志）的等待上限；超时直接退出进程，
// 避免收尾步骤阻塞（如 SQLite 关连接、stdout 管道刷盘）导致进程迟迟不返回控制台
const cleanupTimeout = 5 * time.Second

// runServer 用 gin.ReleaseMode 准备运行环境，进入主循环（热重启驱动 worker）
func runServer(configPath string) {
	gin.SetMode(gin.ReleaseMode)
	rt, done := bootstrap(configPath, "")
	if done {
		return
	}
	printStartupSummary(rt.cfg)
	rt.run()

	// 主循环退出后的收尾：关闭数据库连接并刷空日志缓冲。
	// 放到独立 goroutine 并设置上限，任何一步阻塞都不会无限等待
	start := time.Now()
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		utils.Info("关闭收尾：正在关闭数据库连接")
		appconfig.CloseDB()
		utils.Info("关闭收尾：数据库连接已关闭",
			utils.Duration("elapsed", time.Since(start)))
		utils.Sync()
	}()
	select {
	case <-cleanupDone:
		utils.Info("关闭收尾完成", utils.Duration("elapsed", time.Since(start)))
	case <-time.After(cleanupTimeout):
		fmt.Fprintf(os.Stderr, "关闭收尾超时(%s)，强制退出进程\n", cleanupTimeout)
		os.Exit(0)
	}
}
