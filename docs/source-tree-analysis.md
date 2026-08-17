# 源码目录分析

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-22

---

## 目录结构

```
fuzhan/                              # 项目根目录
│
├── server/                              # Go 后端
│   ├── cmd/
│   │   ├── fuzhan/                 # 程序入口
│   │   │   └── main.go                 # 入口点，初始化应用（含 worker 生命周期管理）
│   │   └── xxh3sum/                    # xxhash 命令行工具
│   │       └── main.go
│   ├── go.mod                           # Go 1.26, Gin 1.11.0, GORM 1.30.0
│   ├── go.sum                           # 依赖校验
│   │
│   ├── internal/                        # 核心应用代码（重构后）
│   │   ├── access/                      # 访问协议
│   │   │   ├── ftp/                     # FTP 虚拟文件系统
│   │   │   └── webdav/                  # WebDAV 服务器
│   │   │
│   │   ├── admin/                       # 管理员模块
│   │   │   ├── handler.go               # 管理员 HTTP 处理器
│   │   │   ├── service.go               # 管理员服务
│   │   │   └── url_download_handler.go  # URL 下载任务管理
│   │   │
│   │   ├── appconfig/                   # 应用配置
│   │   │   ├── config.go                # 配置结构体定义
│   │   │   ├── database.go              # 数据库连接
│   │   │   ├── dto.go                   # DTO 定义
│   │   │   ├── validator.go             # 配置验证
│   │   │   └── writer.go                # 配置写入
│   │   │
│   │   ├── auth/                        # 认证模块
│   │   │   ├── handler.go               # 用户注册/登录/Token 刷新
│   │   │   ├── api_key.go               # API Key 认证
│   │   │   ├── ldap.go                  # LDAP 认证
│   │   │   └── rbac.go                  # RBAC 权限控制
│   │   │
│   │   ├── cli/                         # CLI 模块
│   │   │   ├── cli.go                   # CLI 命令逻辑
│   │   │   └── handler.go               # CLI HTTP 处理器
│   │   │
│   │   ├── config/                      # 配置管理
│   │   │   ├── config.go                # Config 结构体
│   │   │   ├── store.go                 # 配置存储
│   │   │   ├── handler.go               # 配置处理器
│   │   │   └── binding_example.go       # 数据绑定示例
│   │   │
│   │   ├── constants/                   # 常量定义
│   │   │   └── context_keys.go          # Gin Context Key 常量
│   │   │
│   │   ├── file/                        # 文件操作模块
│   │   │   ├── handler.go               # 文件列表/重命名/移动/删除
│   │   │   ├── download.go              # 文件下载
│   │   │   ├── search.go                # 文件搜索（SSE 流式）
│   │   │   ├── preview.go               # 文件预览
│   │   │   ├── private.go               # 私有存储
│   │   │   ├── upload.go                # 分片上传会话
│   │   │   ├── recent.go                # 最近上传/操作记录
│   │   │   ├── base.go                  # 基础处理器
│   │   │   ├── legacy.go                # 遗留代码
│   │   │   ├── storage.go               # 存储服务
│   │   │   └── temp_upload.go           # 临时文件上传
│   │   │
│   │   ├── ftp/                         # FTP 服务器模块
│   │   │   └── handler.go
│   │   │
│   │   ├── health/                      # 健康检查
│   │   │   └── handler.go
│   │   │
│   │   ├── index/                       # 文件索引服务
│   │   │
│   │   ├── middleware/                  # Gin 中间件
│   │   │   ├── auth.go                  # JWT 认证（AuthRequired/AuthOptional/RequireAdmin）
│   │   │   ├── audit.go                 # 审计日志中间件
│   │   │   ├── monitoring.go            # Prometheus 监控指标
│   │   │   ├── response_wrapper.go      # 统一响应包装
│   │   │   ├── token_refresh.go         # Token 自动刷新
│   │   │   └── security.go              # 安全头中间件
│   │   │
│   │   ├── models/                      # 数据模型层（GORM）
│   │   │   ├── user.go                  # User 模型
│   │   │   ├── file.go                  # 文件相关模型
│   │   │   ├── migration.go             # 数据库迁移模型
│   │   │   ├── temp_file.go             # TempFile 模型
│   │   │   ├── upload.go                # UploadSession, UploadedChunk, UploadRecord
│   │   │   ├── url_download_task.go     # URLDownloadTask 模型
│   │   │   ├── api_key.go               # ApiKey 模型
│   │   │   └── notification.go          # Notification 模型
│   │   │
│   │   ├── monitor/                     # 系统监控
│   │   │   ├── handler.go               # 监控处理器
│   │   │   └── metrics.go               # Prometheus 指标
│   │   │
│   │   ├── notification/                # 通知系统
│   │   │   ├── handler.go               # 通知处理器
│   │   │   └── service.go               # 通知服务
│   │   │
│   │   ├── openapi/                     # OpenAPI 支持
│   │   │   ├── spec.go                  # OpenAPI 规范
│   │   │   ├── stats_handler.go         # 统计处理器
│   │   │   ├── middleware.go            # OpenAPI 中间件
│   │   │   └── middleware_test.go
│   │   │
│   │   ├── repositories/                # 数据访问层
│   │   │   ├── upload_repo.go
│   │   │   └── url_download_task_repo.go
│   │   │
│   │   ├── resources/                   # 嵌入资源
│   │   │   ├── embed.go                 # 资源嵌入
│   │   │   ├── cli_help.txt
│   │   │   ├── systemd.service.txt
│   │   │   └── launchd.plist.txt
│   │   │
│   │   ├── router/                      # 路由配置
│   │   │   └── router.go                # Gin 路由设置
│   │   │
│   │   ├── services/                    # 业务服务层
│   │   │   ├── auth_service.go          # 认证服务
│   │   │   ├── file_service.go          # 文件 CRUD、配额、路径解析
│   │   │   ├── search_service.go        # 搜索服务（并发 goroutine + channel）
│   │   │   ├── download_service.go      # 下载服务
│   │   │   ├── cleanup_service.go       # 定时清理过期文件/会话
│   │   │   ├── admin_service.go         # 管理员服务
│   │   │   ├── temp_file_service.go     # 临时文件服务
│   │   │   ├── upload_utils.go          # 上传工具函数
│   │   │   ├── disk_space_unix.go       # Unix 磁盘空间查询
│   │   │   ├── disk_space_windows.go    # Windows 磁盘空间查询
│   │   │   ├── prealloc_default.go      # 文件预分配（默认）
│   │   │   ├── prealloc_linux.go        # 文件预分配（Linux）
│   │   │   └── prealloc_windows.go      # 文件预分配（Windows）
│   │   │
│   │   │   └── migration/               # 数据库迁移服务（跨 DB 引擎）
│   │   │       ├── migration_service.go # 迁移编排
│   │   │       ├── backup_service.go    # 备份/恢复
│   │   │       ├── exporter.go          # 数据导出
│   │   │       ├── importer.go          # 数据导入
│   │   │       ├── sqlite_migrator.go   # SQLite 迁移器
│   │   │       ├── mysql_migrator.go    # MySQL 迁移器
│   │   │       ├── postgres_migrator.go # PostgreSQL 迁移器
│   │   │       ├── progress.go          # 进度跟踪
│   │   │       ├── stages.go            # 迁移阶段定义
│   │   │       ├── ddl_parser.go        # DDL 解析
│   │   │       ├── type_mapper.go       # 类型映射
│   │   │       ├── transformer.go       # 数据转换
│   │   │       ├── index_converter.go   # 索引转换
│   │   │       ├── foreignkey_handler.go# 外键处理
│   │   │       ├── lock.go              # 迁移锁
│   │   │       ├── event_emitter.go     # 事件发射器（SSE 进度推送）
│   │   │       ├── recovery.go          # 恢复逻辑
│   │   │       ├── rollback.go          # 回滚逻辑
│   │   │       ├── rollback_test.go
│   │   │       ├── recovery_test.go
│   │   │       ├── migration_service_test.go
│   │   │       ├── dependencies.go      # 表依赖分析
│   │   │       ├── errors.go            # 错误定义
│   │   │       ├── utils.go             # 工具函数
│   │   │       └── *_test.go            # 各模块测试
│   │   │
│   │   ├── setup/                       # 初始化向导
│   │   │   └── handler.go
│   │   │
│   │   ├── temp/                        # 临时文件模块
│   │   │   ├── model.go                 # 临时文件模型
│   │   │   └── service.go               # 临时文件服务
│   │   │
│   │   └── utils/                       # 工具函数
│   │       ├── asset_utils.go           # 嵌入资源工具
│   │       ├── error_handler.go         # 错误处理
│   │       ├── file_type.go             # 文件类型检测
│   │       ├── file_utils.go            # 文件操作工具
│   │       ├── http_utils.go            # HTTP 工具
│   │       ├── jwt.go                   # JWT 工具
│   │       ├── logger.go                # Zap + lumberjack 日志
│   │       ├── metrics.go               # Prometheus 指标注册
│   │       ├── path_utils.go            # 路径工具
│   │       ├── preview_utils.go         # 预览工具
│   │       ├── url_validator.go         # URL 验证（SSRF 防护）
│   │       ├── pathpolicy/              # 路径策略
│   │       │   └── policy.go
│   │       └── service/                 # 服务安装
│   │           ├── installer.go
│   │           ├── windows.go
│   │           └── service.go
│   │
│   ├── pkg/                             # 公共包
│   │   ├── jwt/                         # JWT 工具
│   │   ├── pathutils/                   # 路径工具
│   │   ├── response/                    # 响应工具
│   │   └── xxh3/                        # xxh3 实现
│   │
│   └── integration/                     # 集成测试
│       ├── health_handler_test.go
│       ├── recent_handler_test.go
│       ├── monitor_handler_test.go
│       └── setup_handler_test.go
│
├── ui/                                  # Vue 3 前端
│   ├── src/                             # 源码目录
│   │   ├── main.js                      # 入口点（初始化、通知轮询、Token 验证）
│   │   ├── App.vue                      # 根组件
│   │   │
│   │   ├── api/                         # API 客户端
│   │   │   └── index.js                 # Axios + 11 个 API 模块
│   │   │
│   │   ├── assets/                      # 静态资源（图片、字体等）
│   │   │
│   │   ├── components/                  # 组件
│   │   │   ├── common/                  # 通用组件
│   │   │   │   ├── AppHeader.vue        # 头部导航 + 登录/注册弹框
│   │   │   │   ├── AppFooter.vue        # 底部栏
│   │   │   │   └── NotificationBell.vue # 通知铃铛
│   │   │   ├── file/                    # 文件相关
│   │   │   │   ├── FileItem.vue         # 文件项
│   │   │   │   ├── FilePreview.vue      # 文件预览
│   │   │   │   └── RecentUploadsFull.vue # 最近上传完整列表
│   │   │   ├── upload/                  # 上传组件
│   │   │   │   └── UploadManager.vue    # 上传管理
│   │   │   ├── settings/                # 设置组件
│   │   │   ├── setup/                   # 初始化组件
│   │   │   ├── migration/               # 数据库迁移组件
│   │   │   ├── Toast.vue                # 通知提示
│   │   │   └── ToastContainer.vue       # 通知容器
│   │   │
│   │   ├── views/                       # 页面组件
│   │   │   ├── HomeView.vue             # /files - 首页文件浏览
│   │   │   ├── SetupView.vue            # /setup - 初始化向导
│   │   │   ├── TempView.vue             # /temp - 临时文件
│   │   │   ├── PrivateStorageView.vue   # /private - 私有存储
│   │   │   ├── RecentView.vue           # /recent - 最近上传
│   │   │   ├── HotView.vue              # /hot - 热门文件
│   │   │   ├── UserView.vue             # /user - 个人中心
│   │   │   ├── SettingsView.vue         # /settings - 系统设置（管理员）
│   │   │   ├── AdminDashboardView.vue   # /admin/dashboard - 仪表盘
│   │   │   ├── AdminFilesView.vue       # /admin/files - 文件管理
│   │   │   ├── AdminUsersView.vue       # /admin/users - 用户管理
│   │   │   ├── AdminZombieView.vue      # /admin/zombie - 僵尸文件
│   │   │   └── AdminURLTaskView.vue     # /admin/url-tasks - URL 下载任务管理
│   │   │
│   │   ├── router/                      # 路由
│   │   │   └── index.js                 # Hash 模式，13+ 路由，登录弹框事件
│   │   │
│   │   ├── store/                       # 状态管理
│   │   │   └── index.js                 # reactive 响应式状态
│   │   │
│   │   ├── composables/                 # 组合式函数
│   │   │   └── useToast.js              # Toast 通知
│   │   │
│   │   ├── config/                      # 前端配置
│   │   │   └── preview.js               # 预览配置加载
│   │   │
│   │   ├── plugins/                     # Vue 插件
│   │   │   └── naive-ui.js              # Naive UI 按需注册
│   │   │
│   │   ├── styles/                      # 全局样式
│   │   │
│   │   └── utils/                       # 工具函数
│   │       ├── error.js                 # 错误处理
│   │       ├── xxhash.js                # 分片校验（浏览器端 xxh3）
│   │       ├── index.js                 # 工具函数导出
│   │       └── __tests__/               # 测试
│   │           └── xxhash.test.js
│   │
│   ├── public/                          # 静态资源（favicon 等）
│   ├── package.json                     # Vue 3.5.24, Vite 7.2.4, Naive UI 2.41.0
│   ├── vite.config.js                   # Vite 构建配置
│   └── README.md                        # 前端说明
│
├── docs/                                # 项目文档
│   ├── index.md                         # 文档索引
│   ├── architecture.md                  # 架构文档
│   ├── api-contracts.md                 # API 文档
│   ├── component-inventory.md           # 组件清单
│   ├── data-models.md                   # 数据模型
│   ├── development-guide.md             # 开发指南
│   ├── user-guide.md                    # 用户手册
│   └── source-tree-analysis.md          # 本文档
│
├── _bmad-output/                        # BMad 工作流产出
├── _bmad/                               # BMad 配置
├── fuzhan.yaml                      # 默认配置文件
├── build.ps1                            # Windows 构建脚本
├── build.sh                             # Linux/macOS 构建脚本
└── CLAUDE.md                            # AI 开发指南
```

---

## 关键目录说明

### server/internal/ - 核心应用代码

按功能模块组织的目录结构：

| 目录 | 职责 |
|------|------|
| `auth/` | 用户注册、登录、Token 刷新、API Key、LDAP、RBAC |
| `file/` | 文件列表、重命名、移动、删除、下载、搜索、预览、上传 |
| `admin/` | 用户 CRUD、会话管理、数据库迁移、备份管理、TLS 证书 |
| `temp/` | 临时文件管理（IP 配额、分片上传） |
| `cli/` | CLI 命令 HTTP 接口 |
| `config/` | 系统配置获取与更新 |
| `health/` | 健康检查端点 |
| `monitor/` | 系统监控（存储/访问/关键词/热门下载/排名） |
| `notification/` | URL 下载通知（获取/标记已读/合并匿名） |
| `setup/` | 初始化向导 |
| `ftp/` | FTP 服务器 |
| `access/` | WebDAV 服务器 |
| `index/` | 文件索引服务 |
| `openapi/` | OpenAPI 规范 |
| `repositories/` | 数据访问层 |

### server/internal/services/ - 业务服务层

| 服务 | 核心功能 |
|------|----------|
| `auth_service.go` | 用户认证、JWT 生成/刷新、用户禁用状态检查 |
| `file_service.go` | 文件 CRUD、配额计算、根目录路径解析 |
| `search_service.go` | 并发文件搜索（goroutine + channel + 结果去重） |
| `cleanup_service.go` | 定时清理过期临时文件/分片会话 |
| `download_service.go` | 文件下载、路径安全验证 |
| `upload_utils.go` | 上传工具函数 |
| `admin_service.go` | 用户管理、会话列表/清理 |
| `temp_file_service.go` | 临时文件 CRUD、IP 配额管理 |
| `migration/` | 跨数据库引擎迁移（SQLite↔MySQL↔PostgreSQL） |

### server/internal/models/ - 数据模型层

| 模型 | 说明 |
|------|------|
| `User` | 用户（UUID/用户名/密码哈希/角色/禁用标志） |
| `TempFile` | 临时文件（码/文件名/大小/IP/目录/下载计数） |
| `UploadSession` | 上传会话（文件名/分片信息/校验和/目标类型/状态） |
| `UploadedChunk` | 已上传分片（会话ID/索引/校验和/大小） |
| `UploadRecord` | 上传记录（路径/类型/IP/用户/操作类型/搜索关键词） |
| `URLDownloadTask` | URL 下载任务（URL/状态/存储类型/是否支持断点续传） |
| `Notification` | 通知（用户ID/任务ID/消息/是否已读） |
| `ApiKey` | API Key（名称/密钥/过期时间/IP 限制） |
| `MigrationStatus` | 数据库迁移状态 |
| `MigrationTableProgress` | 迁移表级进度 |

### ui/src/views/ - 页面组件

| 页面 | 路由 | 权限 |
|------|------|------|
| HomeView | `/files`, `/files/:path*` | 公开 |
| SetupView | `/setup` | 首次运行 |
| TempView | `/temp` | 公开 |
| PrivateStorageView | `/private` | 需登录 |
| RecentView | `/recent` | 公开 |
| HotView | `/hot` | 公开 |
| UserView | `/user` | 需登录 |
| SettingsView | `/settings` | 管理员 |
| AdminDashboardView | `/admin/dashboard` | 管理员 |
| AdminFilesView | `/admin/files`, `/admin/files/:path*` | 管理员 |
| AdminUsersView | `/admin/users` | 管理员 |
| AdminZombieView | `/admin/zombie` | 管理员 |
| AdminURLTaskView | `/admin/url-tasks` | 管理员 |

---

## 入口点

### 后端入口

```
server/cmd/fuzhan/main.go
    │
    ├── flag.Parse()                    # 解析命令行参数
    ├── appconfig.DoInitWithConfig()    # 加载配置（支持热重载）
    ├── config.InitDB()                 # 初始化数据库连接
    ├── db.AutoMigrate()                # 自动迁移模型
    ├── 初始化所有服务：
    │   ├── auth service
    │   ├── file service
    │   ├── upload services
    │   ├── search service
    │   ├── cleanup service
    │   ├── preview service
    │   └── ...（其他服务）
    ├── FTP server start（goroutine）
    ├── cleanup worker start（goroutine）
    ├── router.SetupRouter()           # 设置 Gin 路由
    ├── router.RegisterAPIRoutes()     # 注册 API 路由
    ├── http.Server{...} + ListenAndServe
    ├── signal.Notify（SIGINT/SIGTERM）
    ├── context cancellation → hot-restart loop
    └── 关闭 FTP → Shutdown HTTP → 等待所有 goroutine
```

启动流程使用 **context cancellation + hot-restart loop**：收到退出信号后触发优雅关闭，如果关闭超时则强制退出，并支持在 config 文件变更时热重启整个 worker。

### 前端入口

```
ui/src/main.js
    │
    ├── createApp(App)
    ├── use(naive)          # Naive UI 插件注册
    ├── use(router)         # Vue Router 安装
    ├── mount(#app)         # 挂载应用
    │
    ├── 异步初始化（mount 后执行）：
    │   ├── initAnonymousId()                  # 生成/加载匿名 ID
    │   ├── loadConfigAndCheckSetup()          # 加载公开配置 + 检查初始化状态
    │   │   └── SystemApi.getOptions()         # 获取系统配置
    │   │   └── SetupApi.getStatus()           # 检查是否已初始化
    │   │   └── loadPreviewConfig(data)        # 加载预览配置
    │   ├── validateTokenOnServer()            # 服务端验证 token 有效性
    │   └── startNotificationPolling()         # 启动通知轮询（3s 首次，后每 10s）
```

---

## 关键文件说明

| 文件 | 说明 |
|------|------|
| `server/cmd/fuzhan/main.go` | 入口点，含 worker 生命周期（context 取消 + 热重启循环） |
| `server/internal/config/` | YAML 配置加载，支持文件监听热重载 |
| `server/internal/appconfig/config.go` | 完整配置结构体（多个子配置块） |
| `server/internal/utils/logger.go` | Zap + lumberjack 日志，支持轮转压缩 |
| `server/internal/middleware/auth.go` | JWT 中间件（Required/Optional/Admin），含用户禁用检查 |
| `server/internal/services/migration/` | 跨数据库迁移引擎 |
| `ui/src/api/index.js` | Axios 客户端，API 模块封装 |
| `ui/src/store/index.js` | 响应式状态管理（reactive + readonly） |
| `ui/src/router/index.js` | Vue Router Hash 模式，含登录弹框事件 |

### 架构模式

后端采用 **分层架构**：
- **Handler**（处理器）：按模块组织在 `internal/{module}/handler.go`
- **Service**（服务）：业务逻辑编排、事务管理
- **Model**（模型）：GORM 模型定义
- **Repository**（仓储）：数据访问层

前端采用 **View → Store → API** 模式：
- **View**（页面组件）：路由视图，组合展示组件
- **Store**（状态管理）：reactive 响应式状态
- **API**（API 客户端）：Axios 封装，拦截器处理 Token 和错误