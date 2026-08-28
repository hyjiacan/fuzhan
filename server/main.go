package main

import (
    "context"
    "embed"
    "flag"
    "fmt"
    "io/fs"
    "net/http"
    "os"
    "os/signal"
    "path/filepath"
    "sort"
    "strings"
    "sync"
    "syscall"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/robfig/cron/v3"
    "golang.org/x/crypto/bcrypt"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/middleware"
    "fuzhan/internal/access/webdav"
    "fuzhan/internal/file"
    "fuzhan/internal/ftp"
    "fuzhan/internal/health"
    "fuzhan/internal/index"
    "fuzhan/internal/notification"
    "fuzhan/internal/setup"
    configh "fuzhan/internal/config"
    "fuzhan/internal/admin"
    "fuzhan/internal/auth"
    temph "fuzhan/internal/temp"
    "fuzhan/internal/monitor"
    "fuzhan/internal/openapi"
    "fuzhan/internal/cli"
    "fuzhan/internal/search"
    "fuzhan/internal/models"
    "fuzhan/pkg/jwt"
    "fuzhan/internal/services"
    "fuzhan/internal/utils"
    "fuzhan/internal/utils/service"
)

//go:embed web/index.html web/scalar.html all:web/assets fuzhan.sample.yaml
var webAssets embed.FS

// webFS 提供对嵌入的前端资源的访问
var webFS, _ = fs.Sub(webAssets, "web")
var assetsFS, _ = fs.Sub(webAssets, "web/assets")

// extractWebAssets 将嵌入的 web 资源提取到目标目录
func extractWebAssets(assets embed.FS, targetDir string) error {
    return fs.WalkDir(assets, "web", func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }

        targetPath := filepath.Join(targetDir, path)

        if d.IsDir() {
            return os.MkdirAll(targetPath, 0755)
        }

        content, err := fs.ReadFile(assets, path)
        if err != nil {
            return fmt.Errorf("读取文件 %s 失败: %w", path, err)
        }

        if err := os.WriteFile(targetPath, content, 0644); err != nil {
            return fmt.Errorf("写入文件 %s 失败: %w", targetPath, err)
        }

        return nil
    })
}

func isCLIRequest(c *gin.Context) bool {
    ua := strings.ToLower(c.GetHeader("User-Agent"))
    return strings.HasPrefix(ua, "curl") || strings.HasPrefix(ua, "wget")
}

func main() {
    // 命令行参数解析
    createAdmin := flag.String("create-admin", "", "创建管理员用户，格式: username:password")
    configPath := flag.String("c", "", "配置文件路径")
    extractWeb := flag.Bool("extract-web", false, "将嵌入的 web 资源释放到可执行文件所在目录")

    // 服务安装参数（使用 Bool 检测是否存在，Name 参数在后面）
    var installFlag, uninstallFlag, statusFlag bool
    flag.BoolVar(&installFlag, "install", false, "安装服务")
    flag.BoolVar(&uninstallFlag, "uninstall", false, "卸载服务")
    flag.BoolVar(&statusFlag, "status", false, "查看服务状态")
    serviceName := flag.String("name", "fuzhan", "服务名称")
    serviceUser := flag.String("user", "", "服务运行用户 (Linux/macOS)")

    flag.Parse()

    // 提取 web 资源到可执行文件所在目录
    if *extractWeb {
        exe, err := os.Executable()
        if err != nil {
            fmt.Fprintf(os.Stderr, "获取可执行文件路径失败: %v\n", err)
            os.Exit(1)
        }
        targetDir := filepath.Dir(exe)
        if err := extractWebAssets(webAssets, targetDir); err != nil {
            fmt.Fprintf(os.Stderr, "提取失败: %v\n", err)
            os.Exit(1)
        }
        fmt.Printf("Web 资源已释放到 %s 下的 web/ 目录\n", targetDir)
        os.Exit(0)
        return
    }

    if installFlag || uninstallFlag || statusFlag {
        installer, err := service.NewServiceInstaller(service.InstallOptions{
            BinaryPath: "",
            ConfigPath: *configPath,
            Name:       *serviceName,
            User:       *serviceUser,
        })
        if err != nil {
            fmt.Fprintf(os.Stderr, "错误: %v\n", err)
            os.Exit(1)
        }

        if installFlag {
            if err := installer.Install(); err != nil {
                fmt.Fprintf(os.Stderr, "安装失败: %v\n", err)
                os.Exit(1)
            }
            os.Exit(0)
            return
        } else if uninstallFlag {
            if err := installer.Uninstall(); err != nil {
                fmt.Fprintf(os.Stderr, "卸载失败: %v\n", err)
                os.Exit(1)
            }
            os.Exit(0)
            return
        } else if statusFlag {
            status, err := installer.Status()
            if err != nil {
                fmt.Fprintf(os.Stderr, "查询状态失败: %v\n", err)
                os.Exit(1)
            }
            fmt.Printf("服务状态: %s\n", status.Message)
            os.Exit(0)
            return
        }
    }

    // 设置为发布模式以提高性能
    gin.SetMode(gin.ReleaseMode)

    // ===== 启动阶段1: 配置加载 =====
    utils.Info("========================================")
    utils.Info("[启动] 阶段1: 配置加载")
    cfg := appconfig.DoInitWithConfig(webAssets, *configPath)

    // ===== 启动阶段2: 数据库连接 =====
    utils.Info("[启动] 阶段2: 数据库连接")
    db, err := appconfig.InitDB(&cfg.Database)
    if err != nil {
        utils.Fatal("数据库初始化失败", utils.Err(err))
    }
    appconfig.SetDB(db)
    defer appconfig.CloseDB()

    // 初始化 JWT 密钥
    // 先设置 getter 避免循环导入（始终从 GlobalConfig 读取，确保热重载后一致性）
    jwt.SetJWTSecretGetter(func() string {
        cfg := appconfig.GetConfig()
        return cfg.Server.JWTSecret
    })
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
    db.AutoMigrate(&models.UploadSession{}, &models.UploadedChunk{}, &models.OperationRecord{}, &models.User{}, &models.TempFile{}, &models.URLDownloadTask{}, &models.FileRecordPublic{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{}, &models.FileDependency{}, &models.ApiKey{}, &models.OAuthClient{}, &models.AuthRecord{}, &models.ScanRecord{}, &models.TaskRecord{})

    // 为 file_records_public/temp/private 统一创建索引（命名格式：idx__{table}__{col1}_{col2}_...）
    models.EnsureFileRecordIndexes(db)

    // 初始化日志记录器 (已在 appconfig.DoInit 中初始化)
    defer utils.Sync()

    // 初始化监控指标
    utils.InitMetrics()

    // 处理命令行创建管理员（在所有初始化完成后执行，确保日志、监控等基础设施就绪）
    if *createAdmin != "" {
        parts := strings.Split(*createAdmin, ":")
        if len(parts) != 2 {
            utils.Fatal("创建管理员格式错误，应为 username:password")
        }
        username, password := parts[0], parts[1]

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
        return
    }

    // 初始化验证器
    validator := appconfig.NewValidator()

    // ===== 启动阶段3: 服务初始化 =====
    utils.Info("[启动] 阶段3: 服务初始化")
    // 使用 ServiceBuilder 明确服务依赖顺序
    serviceDeps := services.BuildAllServices(
        db,
        appconfig.RootNames,
        cfg.Storage.Temp.Path,
        cfg.Storage.Private.Path,
    )

    // 从构建结果中获取服务实例
    recordRepo := serviceDeps.RecordRepo
    indexService := serviceDeps.IndexService
    fileService := serviceDeps.FileService
    downloadService := serviceDeps.DownloadService
    searchService := serviceDeps.SearchService
    authService := serviceDeps.AuthService
    privateStorageService := services.NewPrivateStorageService(fileService)

    // LDAP 认证服务（v3 Phase 2）
    ldapService := auth.NewLDAPService(db, &auth.LDAPConfig{
        Enabled:        cfg.Auth.LDAP.Enabled,
        Host:           cfg.Auth.LDAP.Host,
        Port:           cfg.Auth.LDAP.Port,
        UseSSL:         cfg.Auth.LDAP.UseSSL,
        BaseDN:         cfg.Auth.LDAP.BaseDN,
        BindDN:         cfg.Auth.LDAP.BindDN,
        BindPassword:   cfg.Auth.LDAP.BindPassword,
        UserFilter:     cfg.Auth.LDAP.UserFilter,
        SyncInterval:   cfg.Auth.LDAP.SyncInterval,
        AutoCreateUser: cfg.Auth.LDAP.AutoCreateUser,
        InsecureSkipVerify: cfg.Auth.LDAP.InsecureSkipVerify,
    })

    // 将 LDAP 服务注入到 AuthService
    if ldapService.IsEnabled() {
        authService.SetLDAPService(ldapService)
        utils.Info("LDAP 认证已启用", utils.String("host", cfg.Auth.LDAP.Host))
    } else {
        utils.Info("LDAP 认证未启用")
    }

    // 创建认证中间件（实时查库检查用户禁用状态）
    authMiddleware := middleware.NewAuthMiddleware(db)

    // 创建速率限制器 (每分钟最多60个请求) - 已禁用以支持分片上传
    // rateLimiter := middleware.NewRateLimiter(60, 1*time.Minute)

    // 创建处理器
    baseHandler := file.NewBaseHandler(&cfg, indexService, recordRepo)
    fileHandlers := file.NewFileHandlers(baseHandler, fileService, db)
    downloadHandlers := file.NewDownloadHandlers(downloadService)
    searchHandlers := file.NewSearchHandlers(searchService, recordRepo)
    cliHandlers := cli.NewCLIHandlers(fileService, searchService)
    bindingExampleHandler := configh.NewBindingExampleHandler()
    authHandler := auth.NewHandler(authService)
    healthHandler := health.NewHealthHandler(db)
    setupHandler := setup.NewSetupHandler()
    configHandler := configh.NewHandler()

    // 上传会话处理器
    chunkSize := cfg.Upload.ChunkSize
    uploadSessionSvc := services.NewUploadSessionService(db, chunkSize, indexService)
    taskService := services.NewTaskService(db)
    uploadSessionHandler := file.NewUploadSessionHandler(uploadSessionSvc, db, chunkSize, recordRepo, indexService, taskService)
    privateUploadHandler := file.NewPrivateUploadHandler(uploadSessionSvc, db, chunkSize, cfg.Storage.Private.Path, indexService)
    privateStorageHandler := file.NewPrivateStorageHandlers()
    adminService := services.NewAdminService(db)
    adminHandler := admin.NewHandler(adminService, db)
    notificationHandler := notification.NewHandler(db)
    adminURLDownloadHandler := admin.NewURLDownloadHandler(db)
    taskHandler := admin.NewTaskHandler(taskService)
    // 将任务记录服务注入索引服务，使扫描/哈希等任务自动写入 task_records
    indexService.SetTaskService(taskService)

    // ===== 文件名检索索引（自动补全 / 拼写纠错）=====
    // 数据存放于 <data>/search_index，用于 HomeView 搜索框的下拉联想与纠错。
    idxSearch, err := search.OpenIndex(filepath.Join(appconfig.GetDataDir(), "search_index"))
    if err != nil {
        utils.Warn("文件名检索索引初始化失败，搜索联想/纠错功能不可用", utils.Err(err))
    } else {
        utils.Info("文件名检索索引已打开", utils.String("dir", filepath.Join(appconfig.GetDataDir(), "search_index")))
        if rerr := idxSearch.RebuildFromDB(db); rerr != nil {
            utils.Warn("文件名检索索引全量构建失败，将依赖后续增量同步", utils.Err(rerr))
        } else {
            utils.Info("文件名检索索引全量构建完成")
        }
        // 文件增/删/改后增量同步检索索引
        indexService.SetFileIndexNotifier(idxSearch)
    }
    suggestHandler := search.NewHandler(idxSearch)

    // 临时文件处理器 (基于IP，无需认证)
    tempSvcConfig := services.TempServiceConfig{
        Path:              cfg.Storage.Temp.Path,
        Enabled:           cfg.Storage.Temp.Enabled,
        DefaultExpireDays: cfg.Storage.Temp.DefaultExpireDays,
        DeleteOnDownload:  cfg.Storage.Temp.DeleteOnDownload,
    }
    tempService := services.NewTempFileServiceWithConfig(db, tempSvcConfig)
    tempHandler := temph.NewHandler(db, cfg.Storage.Temp, tempService, indexService)
    // 初始化临时上传会话表（仅在临时文件功能启用时）
    if cfg.Storage.Temp.Enabled {
        tempHandler.InitTempSessionTable()
    } else {
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
    )
    openAPICallStat := openapi.NewAPICallStat(10000)
    openAPIStatsHandler := openapi.NewStatsHandler(openAPICallStat)

    // 最近上传记录处理器 (无需认证)
    recentHandler := file.NewRecentHandler(recordRepo)

    // 系统监测处理器 (无需认证)
    monitorService := services.NewMonitorService(recordRepo, db)
    monitorHandler := monitor.NewHandler(monitorService)

    // 预览处理器 (无需认证)
    previewHandler := file.NewPreviewHandler(baseHandler)

    // 封装路由设置（便于热重启时重新创建 Gin Engine）
    // 返回 (Gin Engine, FTP Handler)，确保每次热重启时 FTP Handler 也能重建
    setupRouter := func() (*gin.Engine, *ftp.FTPHandler) {
        // 创建 FTP 处理器（在 setupRouter 内创建，每次热重启时重建）
        ftpHandler := ftp.NewFTPHandler()
        ftpHandler.SetAuthUser(ftp.AuthUserFunc(func(username, password string) (string, error) {
            user, err := authService.LoginByPassword(username, password)
            if err != nil {
                return "", err
            }
            return user.UUID, nil
        }))
        // 设置 FTP 操作录制回调
        ftpHandler.SetRecordFunc(func(action, filePath, fileName, rootName, fileTypeTag, clientIP, userUUID string, fileSize int64) {
            // 异步录制，避免影响 FTP 性能
            go func() {
                if err := recordRepo.Create(&models.OperationRecord{
                    FileName:   fileName,
                    FilePath:   filePath,
                    // FullPath = /RootName + FilePath
                    FullPath:   "/" + rootName + filePath,
                    RootName:   rootName,
                    FileSize:   fileSize,
                    ClientIP:   clientIP,
                    UserID:     userUUID,
                    Action:     action,
                    UploadTime: time.Now(),
                }); err != nil {
                    utils.Debug("FTP操作记录失败",
                        utils.String("action", action),
                        utils.String("file", filePath),
                        utils.Err(err))
                }
            }()
        })

        // 创建 Gin 路由
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

        // 添加速率限制中间件 - 已禁用以支持分片上传
        // r.Use(rateLimiter.Limit())

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
                    cliHandlers.HandleCli(c)
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
                    cliHandlers.HandleCli(c)
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
        {
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
            private.Use(authMiddleware.AuthRequired(), middleware.TokenRefreshMiddleware(authService), middleware.ResponseWrapperMiddleware())
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

            // 管理页面搜索和下载（无需认证，前端使用 EventSource 无法携带 Authorization 头）
            api.GET("/admin/search/*query", searchHandlers.AdminSearchFiles)
            api.GET("/admin/download/*path", downloadHandlers.AdminDownloadFile)
            api.HEAD("/admin/download/*path", downloadHandlers.AdminDownloadFile)

            // 文件记录搜索（用于 autocomplete，无需 admin）
            api.GET("/files/search-records", authMiddleware.AuthOptional(), middleware.ResponseWrapperMiddleware(), indexHandler.SearchFileRecords)

            // 扫描相关路由（无需认证，前端 Footer 所有人都可点击立即扫描）
            scanRoutes := api.Group("/admin/index")
            {
                scanRoutes.POST("/scan", indexHandler.TriggerScan)
                scanRoutes.POST("/scan/trigger", indexHandler.TriggerFullScan)
                scanRoutes.GET("/scan/progress", indexHandler.GetScanProgress)
                scanRoutes.GET("/scan/status", indexHandler.GetScanStatus)
            }

            // 管理员路由（需要认证和管理员权限）
            admin := api.Group("/admin")
            admin.Use(authMiddleware.AuthRequired(), middleware.RequireAdmin(), middleware.TokenRefreshMiddleware(authService), middleware.ResponseWrapperMiddleware())
            {
                admin.GET("/users", adminHandler.UsersHandler)
                admin.PUT("/users/:uuid/reset-password", adminHandler.ResetPasswordHandler)
                admin.PUT("/users/:uuid/disabled", adminHandler.SetUserDisabledHandler)
                admin.DELETE("/users/:uuid", adminHandler.DeleteUserHandler)
                admin.GET("/sessions", adminHandler.SessionsHandler)
                admin.POST("/sessions/cleanup", adminHandler.CleanupSessionsHandler)

                // 操作记录清空路由
                admin.POST("/records/clear", adminHandler.ClearRecordsHandler)

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
                admin.GET("/open-api/stats", openAPIStatsHandler.GetStats)
            }

            // 配置引导路由（无需认证）
            setup := api.Group("/setup")
            setup.GET("/status", setupHandler.IsInitialized)
            setup.POST("/save", setupHandler.SaveConfig)
            setup.POST("/validate-dir", setupHandler.ValidateDirectory)
            setup.GET("/network/interfaces", setupHandler.GetNetworkInterfaces)
            setup.GET("/default-config", setupHandler.GetDefaultConfig)

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
            configGroup.Use(authMiddleware.AuthRequired(), middleware.RequireAdmin(), middleware.TokenRefreshMiddleware(authService), middleware.ResponseWrapperMiddleware())
            configGroup.GET("", configHandler.GetConfig)
            configGroup.POST("", configHandler.SaveConfig)

            // 临时文件路由（基于IP，无需认证）
            temp := api.Group("/temp")
            temp.GET("/list", tempHandler.ListHandler)
            temp.POST("/upload", tempHandler.UploadHandler)
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
                uploads.POST("/url", uploadSessionHandler.UploadFromURL) // 从URL上传
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
                files.GET("/preview/*path", previewHandler.PreviewFile) // 文件预览
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
            protected.Use(authMiddleware.AuthRequired(), middleware.TokenRefreshMiddleware(authService), middleware.ResponseWrapperMiddleware())
            {
                // 认证相关路由
                protected.POST("/auth/refresh", authHandler.Login) // 复用登录逻辑
                protected.GET("/auth/user", authHandler.GetCurrentUser)
                protected.PUT("/auth/password", authHandler.ChangePassword)

                // 文件管理路由
                files := protected.Group("/files")
                {
                    files.POST("/rename", func(c *gin.Context) {
                        file.HandleRenameFile(c.Writer, c.Request)
                    })
                    files.POST("/move", func(c *gin.Context) {
                        file.HandleMoveFile(c.Writer, c.Request)
                    })
                }

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
            if cfg.Server.WebDAV.Enabled {
                authenticate := webdav.UserAuthenticator(func(username, password string) (string, bool, error) {
                    user, err := authService.LoginByPassword(username, password)
                    if err != nil {
                        return "", false, err
                    }
                    return user.UUID, user.Disabled, nil
                })

                publicFS := webdav.NewPublicFileSystem(appconfig.RootNames)
                var unifiedFS *webdav.UnifiedFileSystem
                if cfg.Storage.Private.Enabled {
                    privateFS := webdav.NewPrivateFileSystem(cfg.Storage.Private.Path)
                    unifiedFS = webdav.NewUnifiedFileSystem(publicFS, privateFS)
                } else {
                    unifiedFS = webdav.NewUnifiedFileSystem(publicFS, nil)
                }

                davHandler := webdav.NewHandler("/api/v1/webdav", unifiedFS)
                // 挂到 /api/v1/webdav 子组，避免 /*path 通配符与 /api/v1 下同级静态路由冲突
                webdav.SetupUnifiedRouter(api.Group("/webdav"), davHandler, authenticate, cfg.Upload.MaxFileSize)
                utils.Info("WebDAV 统一端点已启用", utils.String("path", "/api/v1/webdav"))
            } else {
                utils.Info("WebDAV 服务未启用")
            }

            // CLI 路由（无需认证）
            cli := api.Group("/cli")
            {
                cli.GET("", cliHandlers.HandleCli)
                cli.GET("/search/*query", cliHandlers.CliSearch)
                cli.GET("/list/*path", cliHandlers.CliList)
                cli.GET("/install.sh", cliHandlers.InstallScript)
                cli.GET("/fuzhan.sh", cliHandlers.FuzhanScript)
            }
        }

        // CLI 路由别名（无需认证，/cli 作为 /api/v1/cli 的快捷入口）
        cliAlias := r.Group("/cli")
        {
            cliAlias.GET("", cliHandlers.HandleCli)
            cliAlias.GET("/search/*query", cliHandlers.CliSearch)
            cliAlias.GET("/list/*path", cliHandlers.CliList)
            cliAlias.GET("/install.sh", cliHandlers.InstallScript)
            cliAlias.GET("/fuzhan.sh", cliHandlers.FuzhanScript)
        }

        // 下载路由别名（无需认证，/download 作为 /api/v1/download 的快捷入口）
        downloadAlias := r.Group("/download")
        {
            downloadAlias.GET("/*path", downloadHandlers.DownloadFile)
            downloadAlias.HEAD("/*path", downloadHandlers.DownloadFile)
        }

        // Open API 路由组（v3 Phase 3）
        // 架构要求中间件链: IP白名单 → 功能开关 → 频率限制 → 调用统计 → 路由分发 → Handler
        // 见 architecture-v3.md
        openAPI := r.Group("/api/open/v1")
        openAPI.Use(
            openapi.FeatureGateMiddleware(openAPICfg.Enabled),
            openapi.IPAccessMiddleware(openAPIIPAccessCfg),
            openapi.RateLimitMiddleware(openAPIRateLimiter),
            openapi.CallStatMiddleware(openAPICallStat),
            middleware.ResponseWrapperMiddleware(),
        )
        {
            // FR-3.1: 文件列表 API（公开，无需认证）
            openAPI.GET("/files/list", openAPIHandler.ListFiles)
            // FR-3.2: 文件搜索 API（公开）
            openAPI.GET("/files/search", openAPIHandler.SearchFiles)
            // FR-3.3: 文件下载 API（公开）
            openAPI.GET("/files/download/*path", openAPIHandler.DownloadFile)
            // FR-3.5: 重复文件查询 API（需认证，需要 reader scope）
            openAPI.GET("/files/duplicates", rbacService.RequireScope("open_api:reader"), openAPIHandler.ListDuplicates)
            // FR-3.6: 文件备注查询 API（需认证，需要 reader scope）
            openAPI.GET("/files/notes", rbacService.RequireScope("open_api:reader"), openAPIHandler.GetNotes)
            // FR-4.1: 文件依赖树查询 API（需认证，需要 reader scope）
            openAPI.GET("/files/:id/depends", rbacService.RequireScope("open_api:reader"), openAPIHandler.GetDependencyTree)
            // FR-3.10: OpenAPI 文档（公开）
            openAPI.GET("/openapi.json", openAPIHandler.GetOpenAPISpec)
            openAPI.GET("/docs", openAPIHandler.GetDocsPage)
        }

        return r, ftpHandler
    }

    // 服务器地址
    address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.HTTP.Port)
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

    // ===== 启动阶段4: 路由设置 =====
    utils.Info("[启动] 阶段4: 路由设置完成")

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

    // 配置变更通知通道（缓冲1，非阻塞发送）
    configCh := make(chan struct{}, 1)

    // 创建主上下文（用于 worker 生命周期管理）
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 注册热更新钩子（内联处理无需重启的逻辑，信号通知主循环重启 worker）
    appconfig.AddOnSaveHook(func() {
        // 1. JWT 密钥热更新（简单内联处理，无需重启）
        if s := appconfig.GlobalConfig.Server.JWTSecret; len(s) >= 32 && s != string(jwt.GetJWTSecret()) {
            jwt.UpdateJWTSecret([]byte(s))
            utils.Info("JWT密钥已热更新")
        }

        // 2. 日志配置热重载（简单内联处理，无需重启）
        if err := utils.ReinitLogger(appconfig.GlobalConfig.Log); err != nil {
            utils.Warn("日志热重载失败", utils.Err(err))
        } else {
            utils.Info("日志配置已热重载")
        }

        // 通知主循环处理需要重启的 worker
        select {
        case configCh <- struct{}{}:
        default:
        }
    })

    // 信号通道
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    // workerManager 封装单轮迭代的 Worker 生命周期（每轮重建，避免跨迭代 WaitGroup 竞争）
    type workerManager struct {
        wg       sync.WaitGroup
        cancels  map[string]context.CancelFunc
        mu       sync.Mutex
    }

    newWorkerManager := func() *workerManager {
        return &workerManager{cancels: make(map[string]context.CancelFunc)}
    }

    // start 在当前迭代中启动一个 worker goroutine
    workerStart := func(wm *workerManager, name string, fn func(ctx context.Context)) {
        workerCtx, workerCancel := context.WithCancel(ctx)
        wm.mu.Lock()
        wm.cancels[name] = workerCancel
        wm.mu.Unlock()
        wm.wg.Add(1)
        go func() {
            defer wm.wg.Done()
            defer func() {
                wm.mu.Lock()
                delete(wm.cancels, name)
                wm.mu.Unlock()
            }()
            fn(workerCtx)
        }()
    }

    // stopAllWorkers 取消当前迭代的所有 worker
    stopAllWorkers := func(wm *workerManager) {
        wm.mu.Lock()
        for name, cancel := range wm.cancels {
            cancel()
            delete(wm.cancels, name)
            utils.Info("已停止 worker", utils.String("name", name))
        }
        wm.mu.Unlock()
    }

    // waitWorkersWithTimeout 等待当前迭代的所有 worker 退出，超时则记录警告并继续
    waitWorkersWithTimeout := func(wm *workerManager) {
        c := make(chan struct{}, 1)
        go func() {
            wm.wg.Wait()
            c <- struct{}{}
        }()
        select {
        case <-c:
            utils.Info("所有 worker 已退出")
        case <-time.After(30 * time.Second):
            utils.Warn("等待 workers 超时 (30s)，强制继续")
        }
    }

    // 不依赖热重启变量的 worker 函数（定义一次，循环复用）
    runFTPWorker := func(ctx context.Context, ftpHandler *ftp.FTPHandler) {
        if err := ftpHandler.Start(); err != nil {
            utils.Warn("FTP服务器启动失败", utils.Err(err))
        }
        <-ctx.Done()
        ftpHandler.Stop()
    }

    runCleanupWorker := func(ctx context.Context) {
        // 启动时立即执行一次清理（统一在 worker 内处理，不使用独立 goroutine）
        utils.Info("[启动] 执行初始清理任务")
        if err := uploadSessionHandler.CleanupExpiredSessions(); err != nil {
            utils.Warn("初始清理过期会话失败", utils.Err(err))
        }
        tempHandler.CleanupTempSessions()
        tempHandler.CleanupExpiredFiles()

        // 定时清理
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                if err := uploadSessionHandler.CleanupExpiredSessions(); err != nil {
                    utils.Warn("清理过期会话失败", utils.Err(err))
                }
                tempHandler.CleanupTempSessions()
                tempHandler.CleanupExpiredFiles()
            case <-ctx.Done():
                return
            }
        }
    }

    runPrivateCleanupWorker := func(ctx context.Context) {
        // 快照配置，避免与 SaveConfig 并发读写 GlobalConfig（通过 GetConfig 安全复制）
        privateCfg := appconfig.GetConfig().Storage.Private
        if !privateCfg.Enabled {
            utils.Info("私有存储功能未启用")
            <-ctx.Done()
            return
        }
        timer := time.NewTimer(5 * time.Second)
        select {
        case <-timer.C:
        case <-ctx.Done():
            timer.Stop()
            return
        }
        ticker := time.NewTicker(1 * time.Hour)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                privateStorageService.CleanUpExpiredFiles(privateCfg)
            case <-ctx.Done():
                return
            }
        }
    }

    runIndexScanWorker := func(ctx context.Context) {
        // 启动时检测各 scope 的索引表是否为空，为空则触发全量扫描
        // 应用配置的扫描延迟，避免阻塞启动
        scanDelay := cfg.Index.ScanStartDelaySeconds
        if scanDelay <= 0 {
            scanDelay = 10 // 默认延迟 10 秒
        }
        utils.Info("[启动] 等待索引扫描延迟",
            utils.Int("delay_seconds", scanDelay))

        // 等待延迟或上下文取消
        timer := time.NewTimer(time.Duration(scanDelay) * time.Second)
        select {
        case <-timer.C:
        case <-ctx.Done():
            timer.Stop()
            return
        }
        timer.Stop()

        utils.Info("[启动] 开始索引扫描检查")

        scopes := []struct {
            scope   index.ScanScope
            name    string
        }{
            {index.ScanScopePublic, "公开文件"},
            {index.ScanScopeTemp, "临时文件"},
            {index.ScanScopePrivate, "私有文件"},
        }

        for _, s := range scopes {
            // 检查 scope 对应的功能是否已启用
            switch s.scope {
            case index.ScanScopeTemp:
                if !appconfig.GetConfig().Storage.Temp.Enabled {
                    utils.Info("临时文件索引扫描已跳过（临时文件功能未启用）")
                    continue
                }
            case index.ScanScopePrivate:
                if !appconfig.GetConfig().Storage.Private.Enabled {
                    utils.Info("私有文件索引扫描已跳过（私有存储功能未启用）")
                    continue
                }
            }
            // 检查扫描是否正在进行中
            progress := indexService.GetScanProgressByScope(s.scope)
            if progress.Status == index.ScanStatusRunning {
                utils.Info(s.name + "索引扫描正在进行中, 跳过")
                continue
            }

            utils.Info(s.name + "索引表, 启动初始扫描")
            if err := indexService.StartScanByScope(s.scope); err != nil {
                utils.Error(s.name+"初始扫描启动失败", utils.Err(err))
            }
        }
    }

    runScanTimerWorker := func(ctx context.Context) {
        // 等待初始扫描完成后再启动定时扫描
        pollTicker := time.NewTicker(5 * time.Second)
        defer pollTicker.Stop()

        for {
            progress := indexService.GetScanProgress()
            if progress.Status == index.ScanStatusIdle ||
                progress.Status == index.ScanStatusCompleted ||
                progress.Status == index.ScanStatusFailed {
                break
            }
            select {
            case <-pollTicker.C:
                continue
            case <-ctx.Done():
                return
            }
        }

        indexService.StartScanTimer()

        // 启动文件监听器（实时增量索引）
        indexService.StartWatcher()

        // 注册配置变更回调（热生效：设置页面保存后自动重启定时器和监听器）
        appconfig.AddOnSaveHook(func() {
            indexService.RestartScanTimer()
            indexService.RestartWatcher()
        })

        <-ctx.Done()
        indexService.StopScanTimer()
    }

    runConsistencyCheckWorker := func(ctx context.Context) {
        // 等待初始扫描完成后再启动一致性校验
        // 轮询扫描状态直到完成（不设超时）
        pollTicker := time.NewTicker(5 * time.Second)
        defer pollTicker.Stop()

        for {
            progress := indexService.GetScanProgress()
            if progress.Status == index.ScanStatusIdle ||
                progress.Status == index.ScanStatusCompleted ||
                progress.Status == index.ScanStatusFailed {
                break
            }
            select {
            case <-pollTicker.C:
                continue
            case <-ctx.Done():
                return
            }
        }

        ticker := time.NewTicker(24 * time.Hour)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                // 检查是否正在扫描, 如果是则跳过本次校验
                progress := indexService.GetScanProgress()
                if progress.Status == index.ScanStatusRunning {
                    utils.Info("一致性校验跳过: 全量扫描进行中")
                    continue
                }
                utils.Info("开始定期一致性校验")
                if _, err := indexService.RunConsistencyCheck(ctx); err != nil {
                    utils.Error("一致性校验失败", utils.Err(err))
                }
            case <-ctx.Done():
                return
            }
        }
    }

    runSearchReconcileWorker := func(ctx context.Context) {
        // 等待初始扫描完成后再启动检索索引对齐（依赖索引表已就绪）
        pollTicker := time.NewTicker(5 * time.Second)
        defer pollTicker.Stop()

        for {
            progress := indexService.GetScanProgress()
            if progress.Status == index.ScanStatusIdle ||
                progress.Status == index.ScanStatusCompleted ||
                progress.Status == index.ScanStatusFailed {
                break
            }
            select {
            case <-pollTicker.C:
                continue
            case <-ctx.Done():
                return
            }
        }

        // 检索索引未打开时跳过（启动时初始化失败）
        if idxSearch == nil {
            utils.Info("检索索引未可用，检索索引对齐任务不启动")
            <-ctx.Done()
            return
        }

        // 执行一次检索索引对齐（默认每天 05:00，与定时扫描错开，可在设置页配置）
        getReconcileCron := func() string {
            expr := appconfig.GlobalConfig.Index.SearchReconcileCronExpression
            if expr == "" {
                return "0 5 * * *"
            }
            return expr
        }

        runReconcileOnce := func(trigger string) {
            // 全量扫描进行中时跳过，避免与扫描写盘竞争
            if p := indexService.GetScanProgress(); p.Status == index.ScanStatusRunning {
                utils.Info("检索索引对齐跳过: 全量扫描进行中")
                return
            }
            task, err := taskService.CreateTask("search_reconcile", "检索索引对齐")
            if err != nil {
                utils.Warn("创建检索索引对齐任务失败", utils.Err(err))
                return
            }
            if err := taskService.StartTask(task.ID); err != nil {
                utils.Warn("启动检索索引对齐任务失败", utils.Err(err))
            }
            utils.Info("开始检索索引对齐", utils.String("trigger", trigger))
            res, rerr := idxSearch.ReconcileIndex(db, func(done, total int64) {
                p := 0
                if total > 0 {
                    p = int(done * 100 / total)
                }
                _ = taskService.UpdateTaskProgress(task.ID, p, done, total)
            })
            if rerr != nil {
                _ = taskService.FailTask(task.ID, rerr.Error())
                utils.Error("检索索引对齐失败", utils.Err(rerr))
                return
            }
            _ = taskService.CompleteTask(task.ID)
            utils.Info("检索索引对齐完成",
                utils.Int64("indexed_total", res.IndexedTotal),
                utils.Int64("missing_indexed", res.MissingIndexed),
                utils.Int64("orphans_removed", res.OrphansRemoved))
        }

        // 定时循环（配置变更会触发 worker 热重启，从而重新读取 cron）
        parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
        cronExpr := getReconcileCron()
        schedule, err := parser.Parse(cronExpr)
        if err != nil {
            utils.Warn("检索索引对齐 cron 表达式无效，使用默认值",
                utils.String("cron", cronExpr), utils.Err(err))
            cronExpr = "0 5 * * *"
            schedule, _ = parser.Parse(cronExpr)
        }
        nextTime := schedule.Next(time.Now())
        utils.Info("检索索引对齐下次执行时间",
            utils.String("cron", cronExpr),
            utils.String("next", nextTime.Format("2006-01-02 15:04:05")))

        for {
            select {
            case <-ctx.Done():
                utils.Info("检索索引对齐 worker 已停止")
                return
            default:
            }
            if !time.Now().Before(nextTime) {
                runReconcileOnce(cronExpr)
                nextTime = schedule.Next(time.Now())
                utils.Info("检索索引对齐下次执行时间",
                    utils.String("next", nextTime.Format("2006-01-02 15:04:05")))
            }
            waitDuration := time.Until(nextTime)
            if waitDuration < 0 {
                waitDuration = 0
            }
            timer := time.NewTimer(min(waitDuration, 30*time.Second))
            select {
            case <-ctx.Done():
                timer.Stop()
                utils.Info("检索索引对齐 worker 已停止")
                return
            case <-timer.C:
            }
        }
    }
    currentDbDriver := cfg.Database.Driver
    currentDbDSN := cfg.Database.DSN

    // 数据库热切换
    hotSwapDB := func() {
        dbCfg := appconfig.GetConfig().Database
        if dbCfg.Driver == currentDbDriver && dbCfg.DSN == currentDbDSN {
            return // 数据库配置未变更
        }
        utils.Info("数据库配置已变更，正在热切换...")
        oldDB := appconfig.GetDB()
		_, err := appconfig.InitDBWithAutoMigrate(
			&dbCfg,
			&models.UploadSession{}, &models.UploadedChunk{}, &models.OperationRecord{},
			&models.User{}, &models.TempFile{},
			&models.URLDownloadTask{},
			&models.FileRecordPublic{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{},
			&temph.TempUploadSession{}, &temph.ChunkUploadRecord{},
            &models.FileDependency{},
            &models.ApiKey{},
            &models.OAuthClient{},
            &models.AuthRecord{},
        )
        if err != nil {
            utils.Error("数据库切换失败，保留原连接", utils.Err(err))
            return
        }
        // InitDBWithAutoMigrate 已调用 dbAtomic.Store(newDB)
        // 关闭旧连接
        if oldDB != nil {
            if sqlDB, err := oldDB.DB(); err == nil {
                sqlDB.Close()
            }
        }
        currentDbDriver = dbCfg.Driver
        currentDbDSN = dbCfg.DSN
        utils.Info("数据库已切换到新连接")
    }

    // Worker 热重启循环
    for {
        // 每轮迭代创建独立的 WorkerManager，防止跨迭代 WaitGroup 竞争
        wm := newWorkerManager()

        // 每次循环重新创建 Gin Engine 和 FTP Handler，确保配置变更后使用最新值
        r, ftpHandler := setupRouter()
        loopCfg := appconfig.GetConfig()
        address := fmt.Sprintf("%s:%d", loopCfg.Server.Host, loopCfg.Server.HTTP.Port)

        // HTTP 服务器 worker（需要捕获循环内的 r 和 address）
        runHTTPServer := func(workerCtx context.Context) {
            srv := &http.Server{
                Addr:    address,
                Handler: r,
            }
            go func() {
                <-workerCtx.Done()
                shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer shutdownCancel()
                srv.Shutdown(shutdownCtx)
            }()
            utils.Info("HTTP 服务器启动", utils.String("address", address))
            if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                utils.Error("HTTP 服务器启动失败，服务不可用", utils.String("address", address), utils.Err(err))
            }
        }

        // 启动所有 worker（使用当前迭代的 WorkerManager）
        workerStart(wm, "http", runHTTPServer)
        workerStart(wm, "ftp", func(ctx context.Context) { runFTPWorker(ctx, ftpHandler) })
        workerStart(wm, "cleanup", runCleanupWorker)
        workerStart(wm, "private-cleanup", runPrivateCleanupWorker)
        workerStart(wm, "index-scan", runIndexScanWorker)
        workerStart(wm, "scan-timer", runScanTimerWorker)
        workerStart(wm, "consistency-check", runConsistencyCheckWorker)
        workerStart(wm, "search-reconcile", runSearchReconcileWorker)

        // ===== 启动完成 =====
        utils.Info("========================================")
        utils.Info("[启动] 启动完成，服务已就绪")
        utils.Info("========================================")

        // 等待信号或配置变更
        select {
        case <-sigCh:
            utils.Info("正在关闭服务器...")
            cancel()
            stopAllWorkers(wm)
            waitWorkersWithTimeout(wm)
            if idxSearch != nil {
                if cerr := idxSearch.CloseWriter(); cerr != nil {
                    utils.Warn("关闭文件名检索索引失败", utils.Err(cerr))
                }
            }
            utils.Info("服务器已关闭")
            return
        case <-configCh:
            utils.Info("检测到配置变更，正在热重启 worker...")
            stopAllWorkers(wm)
            waitWorkersWithTimeout(wm)
            hotSwapDB()
            // 检查重启期间是否收到关闭信号
            select {
            case <-sigCh:
                utils.Info("重启期间收到关闭信号，退出")
                cancel()
                return
            default:
            }
            utils.Info("所有 worker 已停止，正在启动新实例...")
            continue
        }
    }
}

// sortedKeys 返回 map 的排序后的键列表
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
