package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fuzhan/internal/admin"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/cli"
	configh "fuzhan/internal/config"
	"fuzhan/internal/file"
	"fuzhan/internal/health"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/monitor"
	"fuzhan/internal/notification"
	"fuzhan/internal/openapi"
	"fuzhan/internal/resource"
	"fuzhan/internal/search"
	"fuzhan/internal/services"
	"fuzhan/internal/setup"
	temph "fuzhan/internal/temp"
	"fuzhan/internal/utils"
	"fuzhan/internal/utils/service"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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
	db.AutoMigrate(&models.UploadSession{}, &models.UploadedChunk{}, &models.OperationRecord{}, &models.User{}, &models.TempFile{}, &models.URLDownloadTask{}, &models.FileRecordPublic{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{}, &models.FileDependency{}, &models.ApiKey{}, &models.OAuthClient{}, &models.AuthRecord{}, &models.ScanRecord{}, &models.TaskRecord{}, &models.ResourceMetric{})

	// 为 file_records_public/temp/private 统一创建索引（命名格式：idx__{table}__{col1}_{col2}_...）
	models.EnsureFileRecordIndexes(db)
	// 为操作记录表创建组合索引（最近/热门列表按 action + created_at 过滤排序）
	models.EnsureOperationRecordIndexes(db)

	// 回填公共文件下载次数（从下载记录统计，升级/启动时执行一次）
	services.BackfillPublicDownloadCounts(db)
	// 回填历史操作记录的 file_record_id（建立身份关联，移动/重命名后不丢）
	services.BackfillFileRecordIDs(db)

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

	// 初始化全局有界操作记录器（固定 worker + 有界缓冲），
	// 让下载/搜索/FTP 等记录点都走有界异步，避免每次操作起新协程造成瞬态尖峰
	services.InitRecordWriter(recordRepo, services.RecordWriterWorkers, services.RecordWriterBuffer)

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

	// 将 LDAP 服务注入到 AuthService
	if ldapService.IsEnabled() {
		authService.SetLDAPService(ldapService)
		utils.Info("LDAP 认证已启用", utils.String("host", cfg.Auth.LDAP.Host))
	} else {
		utils.Info("LDAP 认证未启用")
	}

	// 创建认证中间件（实时查库检查用户禁用状态）
	authMiddleware := middleware.NewAuthMiddleware(db)

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

	// 服务器资源监控（需管理员认证；worker 在下方启动）
	resourceCollector := resource.NewCollector(func() *gorm.DB { return appconfig.GetDB() })
	resourceHandler := resource.NewHandler(resourceCollector)

	// 预览处理器 (无需认证)
	previewHandler := file.NewPreviewHandler(baseHandler)

	// 汇总运行期依赖，供路由注册与 worker 使用
	rt := &runCtx{
		db:                      db,
		cfg:                     &cfg,
		validator:               validator,
		recordRepo:              recordRepo,
		indexService:            indexService,
		fileService:             fileService,
		downloadService:         downloadService,
		searchService:           searchService,
		authService:             authService,
		taskService:             taskService,
		baseHandler:             baseHandler,
		fileHandlers:            fileHandlers,
		downloadHandlers:        downloadHandlers,
		searchHandlers:          searchHandlers,
		cliHandlers:             cliHandlers,
		bindingExampleHandler:   bindingExampleHandler,
		configHandler:           configHandler,
		authHandler:             authHandler,
		healthHandler:           healthHandler,
		setupHandler:            setupHandler,
		uploadSessionHandler:    uploadSessionHandler,
		privateUploadHandler:    privateUploadHandler,
		privateStorageHandler:   privateStorageHandler,
		adminHandler:            adminHandler,
		notificationHandler:     notificationHandler,
		adminURLDownloadHandler: adminURLDownloadHandler,
		taskHandler:             taskHandler,
		tempHandler:             tempHandler,
		indexHandler:            indexHandler,
		depHandler:              depHandler,
		apiKeyHandler:           apiKeyHandler,
		openAPIHandler:          openAPIHandler,
		suggestHandler:          suggestHandler,
		recentHandler:           recentHandler,
		monitorHandler:          monitorHandler,
		resourceHandler:         resourceHandler,
		previewHandler:          previewHandler,
		authMiddleware:          authMiddleware,
		rbacService:             rbacService,
		openAPICfg:              openAPICfg,
		openAPIRateLimiter:      openAPIRateLimiter,
		openAPIIPAccessCfg:      openAPIIPAccessCfg,
		openAPICallStat:         openAPICallStat,
		openAPIStatsHandler:     openAPIStatsHandler,
		idxSearch:               idxSearch,
		resourceCollector:       resourceCollector,
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

	// ===== 启动阶段5: 进入主循环（热重启驱动 worker）=====
	rt.run()
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

// ensureJWTPersisted 当 JWT 密钥既未配置也未通过环境变量指定时，
// 生成随机密钥并写入配置文件，保证重启后签名密钥稳定。
func ensureJWTPersisted() {
	if os.Getenv("JWT_SECRET") != "" {
		return
	}
	if len(appconfig.GlobalConfig.Server.JWTSecret) >= 32 {
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		utils.Warn("生成固定 JWT 密钥失败，本次运行将使用动态密钥", utils.Err(err))
		return
	}
	secretHex := hex.EncodeToString(secret)
	if err := appconfig.SaveConfigWithComments(appconfig.ConfigPath, map[string]interface{}{"server.jwt_secret": secretHex}); err != nil {
		utils.Warn("持久化 JWT 密钥失败，本次运行将使用动态密钥", utils.Err(err))
		return
	}
	appconfig.GlobalConfig.Server.JWTSecret = secretHex
	utils.Info("检测到未配置 JWT 密钥，已生成固定密钥并写入配置")
}
