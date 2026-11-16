# 浮栈（Fuzhan）项目上下文

> 目的：让新会话无需从零分析代码库即可获得准确的项目全貌。
> **维护规则：新增/修改路由、worker、数据模型、配置项、前端路由、构建产物路径时，必须同步更新本文件。**
> 最后核对：2026-11-08（基于当前工作区代码）。

---

## 1. 项目是什么

轻量级文件共享 / 存储工具，单二进制交付：Go 后端内嵌前端静态资源，启动后浏览器访问即可使用。

| 项 | 值 |
|---|---|
| 产品名 | 浮栈 / Fuzhan（仓库名 lightshare） |
| 形态 | monorepo：`server/`（Go）+ `ui/`（Vue 3） |
| Go module | `fuzhan`（`server/go.mod`，`go 1.26.0`） |
| 架构 | Handler → Service → Repository/Model 分层，路由层按作用域拆分文件 |
| 交付 | `build.ps1`：前端产物直接输出到 `server/web`，Go 用 `embed` 打包成单文件（windows/amd64、linux/amd64） |

核心能力：多根目录公开共享、临时分享（按 IP 配额/过期/下载后删除）、私有存储（按用户配额）、分片上传与断点续传、URL 下载队列、Bluge 全文检索 + 文件名联想/纠错、FTP/FTPS、WebDAV、Open API（API Key + RBAC scope）、管理后台（用户/索引/日志/资源/安全事件/行为分析/Webhook）。

---

## 2. 顶层目录

```
lightshare/
├── build.ps1 / build.sh        构建入口
├── README.md / CHANGELOG.md / PLAN.md
├── docs/                       需求、设计、用户、开发文档（见 docs/index.md）
├── .trae/documents/            专项设计记录（安全事件覆盖、Webhook 多端点等）
├── server/                     Go 后端
│   ├── main.go                 进程入口与 CLI 参数
│   ├── bootstrap.go            启动阶段 1~4：配置 → DB → 服务/Handler → runCtx
│   ├── runctx.go               runCtx 依赖汇总 + 路由辅助（白名单、UA 判断）
│   ├── server.go               run() 主循环：worker 生命周期、热重启、优雅关闭
│   ├── router.go               newRouter()：FTP Handler + Gin Engine + 中间件 + 分组
│   ├── routes_web.go           所有非管理后台接口（含 web 分组）
│   ├── routes_admin.go         管理后台接口
│   ├── routes_top.go           顶层路由：/cli /download /lite /api/v1/open + WebDAV
│   ├── workers.go              后台 worker（cron / 扫描 / 清理 / 一致性 / 检索对齐）
│   ├── assets.go               go:embed 前端与 scalar 资源
│   ├── fuzhan.sample.yaml      配置模板（带注释，字段语义以此为准）
│   ├── fuzhan.yaml             实际运行配置
│   ├── web/                    前端构建产物（Vite outDir）
│   ├── scalar/                 OpenAPI 文档页独立构建产物（scalar.html + assets）
│   └── internal/               按领域拆分的实现包（见 §5）
└── ui/                         Vue 3 前端
    ├── vite.config.js          构建到 ../server/web，dev 端口 3000 + 代理
    ├── scalar.vite.config.js   scalar 独立构建
    ├── playwright*.config.js   e2e 配置
    ├── index.html / scalar.html
    └── src/
```

---

## 3. 启动链路

### 3.1 进程入口（`server/main.go`）

参数：

- `-c <path>`：指定配置文件
- `-create-admin username:password`：完成基础设施初始化后创建/更新管理员并退出
- `-extract-web`：把内嵌 `web`、`scalar` 资源释放到可执行文件所在目录
- `-install` / `-uninstall` / `-status` / `-name` / `-user`：注册为系统服务

正常路径：`gin.SetMode(ReleaseMode)` → `runServer(configPath)`（`bootstrap.go` 末尾）。

### 3.2 `bootstrap(configPath, createAdmin)`（`bootstrap.go:48`）

1. **阶段1 配置加载**：`appconfig.DoInitWithConfig(webAssets, configPath)`（创建/读取配置、展开 `${work_dir}`、建 data 目录、初始化日志）。
2. **阶段2 数据库**：`appconfig.InitDB(&cfg.Database)` → `SetDB`；初始化 JWT 密钥（`jwt.SetJWTSecretGetter` 每次从 `GlobalConfig` 读；未配置则生成并持久化）；设置安全配置 getter。
3. **AutoMigrate**（`bootstrap.go:85`，模型清单见 §6）+ 索引保障：`EnsureFileRecordIndexes`、`EnsureOperationRecordIndexes`、`CleanupOperationRecordIndexes`、`EnsureSecurityEventIndexes`。
4. **历史数据回填（幂等）**：`DedupePublicFileRecords`（收敛同一路径的重复索引记录）、`BackfillPublicDownloadCounts`、`BackfillFileRecordIDs`、`BackfillOperationRecordSource`、`BackfillDownloadSignature`、`BackfillDownloadCounted`（有补齐时再 `RecountAllPublicDownloadCounts`）。
5. `utils.InitMetrics()`；`createAdmin` 非空则创建管理员后返回。
6. **阶段3 服务初始化**：`buildPublicStorages`（rootName → `storage.Storage`，本地后端可不注入）→ 按顺序构造 service / handler（列表见 §5.2）。
7. **阶段4**：汇总为 `*runCtx` 返回，进入 `rt.run()`。

### 3.3 主循环 `(*runCtx).run()`（`server.go:28`）

- 注册配置保存钩子：内联处理 JWT 密钥热更新与日志 `ReinitLogger`，再向 `configCh` 发信号（触发 worker 热重启）。
- 每轮迭代：`rt.newRouter()` 重建 Gin Engine 与 FTP Handler → 解析监听地址（`appconfig.LocalhostWhenSetup`：未初始化时强制只监听 127.0.0.1）→ 启动 worker：

| worker 名 | 函数 | 说明 |
|---|---|---|
| http | 内联 | `http.Server`，10s 优雅关闭，超时强制 `Close()`（SSE 长连接） |
| ftp | `runFTPWorker` | FTP/FTPS |
| cleanup | `runCleanupWorker` | 启动即跑一次 + cron |
| index-scan | `runIndexScanWorker` | 延迟后按 scope（public/temp）检查并触发初始扫描；私有元数据由 DB 维护不扫描 |
| scan-timer | `runScanTimerWorker` | 等初始扫描结束 → 定时扫描 + 文件监听 + 注册热更新钩子 |
| consistency-check | `runConsistencyCheckWorker` | 等初始扫描结束 → cron 一致性校验（结束后兜底收敛重复索引记录） |
| search-reconcile | `runSearchReconcileWorker` | 等初始扫描结束 → cron 检索索引对齐 |
| resource-monitor | `resourceCollector.Run` | 资源采样 |
| storage-alert | `storageAlertService.Run` | 非 nil 才启动 |
| ldap-sync | `ldapService.StartSyncWorker` | 非 nil 才启动 |

- 收到 SIGINT/SIGTERM：取消 worker → 等待（上限 30s，超时打印 goroutine 堆栈）→ 停 Watcher → 关检索索引 writer → 依次排空 `StopRecordWriter` / `StopDDoSDetector` / `StopWriter` / `StopDownloadCountRecorder` / `webhook.Stop()`。第二次信号直接 `os.Exit(1)`。
- 收到配置变更：停全部 worker → `hotSwapDB()`（driver/DSN 变更才切换并 AutoMigrate 子集）→ `continue` 重建路由与 worker。

---

## 4. 配置体系

### 4.1 文件与路径约定

- 文件名常量：`appconfig.ConfigFileName = "fuzhan.yaml"`（`internal/appconfig/loader.go`）。
- 查找顺序：启动目录 → 二进制所在目录（缺失时自动生成默认配置并内嵌 `fuzhan.sample.yaml` 模板）。
- `work_dir`：留空 = 配置文件所在目录；支持相对/绝对路径；其它路径字段可用 `${work_dir}` 占位符（`ExpandWorkDirPath`）。
- SQLite 默认：`<work_dir>/data/fuzhan.db`；`GetDataDir()` = `filepath.Join(WorkDir, "data")`；日志目录 `<work_dir>/logs`。
- 日志默认：level=info、MaxSize=10MB、MaxBackups=30、MaxAge=30（`loader.go` `InitLogging`）。
- 配置保存后执行 `RunOnSaveHooks`（热生效）；`AddOnSaveHook` 在 `server.go` 与 `runScanTimerWorker` 中注册。

### 4.2 `Config` 结构（`internal/appconfig/config.go`，约 474 行）

```
work_dir, app, account, server, database, storage, upload, download,
ddos, preview, mime_table, log, security, auth, open_api, index, resource, webhook
```

常用子项（`fuzhan.sample.yaml` 为准）：

- `app`：`name`、`initialized`（false 时走 setup 向导且仅监听本机）
- `server`：`host`、`jwt_secret`、`http{enabled,port(8888)}`、`https{enabled,port(8443)}`、`ftp{enabled,port(21),passive_port_start/end}`、`ftps{enabled,port(990)}`、`tls{cert_file,key_file}`
- `storage.public.root_dirs[]`：`path`、`quota`、`allow_upload`
- `storage.private`：`enabled`、`path`、`quota{global,per_user}`
- `storage.temp`：`enabled`、`path`、`quota{global,per_ip}`、`default_expire_days`、`delete_on_download`
- `storage.allowed_extensions[]`
- `upload`：`chunk_size`(10M)、`max_file_size`(16G)、`url_upload{enabled,allowed_ip_ranges,insecure_skip_verify,max_concurrent,max_retries,max_size}`、`rate_limit{window_minutes,max_requests}`
- `download.rate_limit`：`window_minutes`、`max_requests`、`lock_after`、`lock_minutes`
- `ddos`：`enabled`、`window_seconds`、`max_requests`、`cooldown_minutes`
- `index`：`scan_start_delay_seconds`=10、`scan_cron_expression`="0 1 * * *"、`search_reconcile_cron_expression`="0 5 * * *"、`cleanup_cron_expression`="0 * * * *"、`consistency_check_cron_expression`="0 3 * * *"
- `resource`：`enabled`、`sampling_interval`=5、`collect_interval`=60、`retention_days`=7
- `security`：`trust_proxy`、`allowed_origins`
- `account`：含 `require_login`（强制登录，驱动 LoginGate）
- 大小字段支持 `B/K/M/G/T`（含 `KB/MB/GB/TB`），无单位=字节；`ParseQuotaString` 解析。

---

## 5. 后端分层与包职责

### 5.1 `server/internal` 包清单

```
access, accessguard, admin, alert, appconfig, appconfig/api, auth,
behavioranalytics, captcha, cli, constants, downloadticket, file, filedir,
ftp, githubReleaseGetter, health, index, lite, mailer, middleware, models,
monitor, notification, online, openapi, repositories, resource, resources,
response, search, security, services, setup, storage, temp, uploadutil,
user, utils, webdav, webhook
```

关键约定：

- `appconfig`：配置结构、加载/校验/写入/热保存钩子、DB 初始化（`database.go`）。
- `middleware`：`auth.go`（AuthRequired/AuthOptional/RequireAdmin/LoginGate）、`cookie_auth.go`、`token_refresh.go`、`monitoring.go`、`audit.go`、`api_activity.go`、`response_wrapper.go`、`static_assets.go`、`download_guard.go`、`upload_guard.go`、`web_action.go`。
- `models`：GORM 模型（§6）。
- `index`：索引表扫描/写入/一致性/依赖/哈希 worker/写入门控/文件监听（`scanner*.go`、`walker.go`、`syncer.go`、`hashworker.go`、`write_gate.go`、`file_gate.go`、`watch.go`、`watcher.go`、`consistency.go`、`scan_timer.go`、`records_service.go`、`scan_facade.go`、`service.go`、`handler.go`、`dependency*.go`、`upload_target.go`）。
- `search`：Bluge 全文检索 + 文件名联想/纠错（`SearchIndex`、`ReconcileIndex`、`Handler`）。
- `services`：业务服务（§5.2 关键文件：`upload_service/session/storage/finalizer`、`download_*`、`session_*`、`record_writer`、`task_service`、`temp_file_service`、`file_service`、`search_service`、`admin_service`、`auth_service`、`monitor_service`、`storage_alert_service`、`user_cleanup`、`disk_space_*`、`prealloc_*`、`builder.go`）。
- `storage`：`storage.go`（接口）+ `local.go` / `s3.go` / `oss.go` / `webdav.go` + `factory.go`。
- `file`：公开文件浏览/下载/预览/分片上传/私有存储/最近列表等 handler。
- `temp`：临时分享 handler。`webdav`、`ftp`：协议入口。`openapi`：Open API handler + 限流/scope/IP 控制。
- `repositories`：审计/操作记录存储接口。`response`：统一响应体。

### 5.2 服务装配（`bootstrap.go` 阶段3，按顺序）

`buildPublicStorages` → `indexService` / `fileService` / `downloadService` / `searchService` / `authService`（含 `LDAPService`、`OAuth2Service`）→ handler 批量构造（`file.NewBaseHandler`、`FileHandlers`、`DownloadHandlers`、`SearchHandlers`、`cli`、`lite`、`auth`、`health`、`setup`、`config api`）→ `services.NewUploadSessionService(...).WithPublicStorages(...)`、`NewTaskService` → `file.NewUploadSessionHandler` → `file.NewURLDownloadDispatcher`（`upload.url_upload.max_concurrent`）→ `PrivateUploadHandler`、`PrivateStorageHandlers`、`adminService`、`behaviorAnalyticsService`、`notification`、`security`、`admin.URLDownloadHandler`、`TaskHandler`、`LogHandler` → `search.NewHandler(idxSearch)`、`lite`、`TempFileService`、`temp.Handler` → `index.Handler`、`DependencyService/Handler` → `ApiKeyService/Handler`、`RBACService` → `openapi.Handler`（OpenAPI 文档 + 统计）→ `RecentHandler`、`user.Handler`、`MonitorService/Handler`、`StorageAlertService`、`behavioranalytics.Handler`、`resource.Handler`、`file.PreviewHandler`。

### 5.3 `runCtx`（`runctx.go:39`）

集中持有：`db`、`cfg`、`validator`、`recordRepo`、`indexService`、`fileService`、`downloadService`、`searchService`、`authService`、`taskService`；全部 handler（file/cli/lite/config/auth/health/setup/upload/private/admin/security/notification/temp/index/dep/apiKey/openapi/suggest/recent/user/monitor/storageAlert/resource/preview/behaviorAnalytics）；`authMiddleware`、`rbacService`；Open API 的 cfg/limiter/IP 控制/统计；`storageAlertService`、`idxSearch`、`resourceCollector`、`ldapService`、`publicStorages map[string]storage.Storage`。

同文件辅助函数：`isCLIRequest`（UA 前缀 curl/wget）、`isIEBrowser`、`cliScriptWhitelist`、`loginGateWhiteList`、`redirectIEToLite`、`webdavRecordingRepo`。

---

## 6. 数据模型（`server/internal/models`）

文件：`alert_message`、`api_key`、`file_dependency`、`file_record`、`file`、`login_log`、`resource_metric`、`scan_record`、`security_event`、`task_record`、`temp_file`、`upload`、`url_download_task`、`user`。

**AutoMigrate 清单**（`bootstrap.go:85`，改模型时同步此处）：

```
UploadSession, UploadedChunk, OperationRecord, User, TempFile, URLDownloadTask,
FileRecordPublic, FileRecordTemp, FileRecordPrivate, FileDependency, ApiKey,
OAuthClient, AuthRecord, ScanRecord, TaskRecord, ResourceMetric, SecurityEvent,
LoginLog, AlertMessage
```

DB 热切换时的迁移子集（`server.go`）：`UploadSession, UploadedChunk, OperationRecord, User, TempFile, URLDownloadTask, FileRecord×3, FileDependency, ApiKey, OAuthClient, AuthRecord, ResourceMetric`。

`ApiKey` 额外包含 `AllowedPaths`（换行分隔的 `/rootName[/sub]` 前缀，空或 `*` 表示所有路径），用于 Open API 公开目录操作的路径授权；校验通过后写入上下文 `auth.ContextAllowedPaths`。

### 6.1 文件索引记录（`file_record.go`，public/temp/private 三表共用字段）

| 字段 | 说明 |
|---|---|
| `ID` | 主键 |
| `FileName`(512) / `FilePath`(1024) / `FullPath`(1024，带前导 `/`) / `RootName`(128) | 路径信息 |
| `FileSize` / `IsDir` / `ModTime` | 文件属性 |
| `Xxh3Hash`(64) / `HashStatus`(20，默认 `pending`，取值 `pending/done/failed`) / `HashError`(512) / `HashRetries` | xxh3 哈希与重试状态 |
| `Notes`(4096) / `DownloadCount` / `UploaderIP`(45) | 业务字段 |
| `Status`（`FileStatus`，默认 `active`） | 记录状态 |
| `OwnerID`(64) | 空 = public；temp = IP；private = 用户 ID |
| `ShareCode`(64) | 私密分享码 |
| `LastSyncedAt` / `CreatedAt` / `UpdatedAt` / `DeletedAt(gorm.DeletedAt)` | 时间与软删除 |

索引命名约定：`idx__{table}__{col1}_{col2}_...`（由 `EnsureFileRecordIndexes` 等统一创建）。

---

## 7. 路由总览

> **字段命名：** 所有 `/api/v1/**` 接口（web / admin / open 三套命名空间）的请求参数与响应字段一律 `snake_case`，详见 [API_STYLE_GUIDE.md](../design/api/API_STYLE_GUIDE.md) §4/§5。Go 结构体必须显式写 `json` tag；`gin.H` / `map` 字面量 key 同样写 snake。例外见 §7 例外清单（文件流、SSE、WebDAV、Lite HTML、安装脚本、OAuth2 回调参数）。

### 7.1 中间件与分组（`router.go`）

Gin 全局 Use 顺序：`MonitoringMiddleware` → `StaticAssetsMiddleware`（仅 `/assets`、`/scalar/assets`）→ `SecurityHeadersMiddleware` → `CORSMiddleware` → `RequestIDMiddleware` → `AuditMiddleware` → `gin.CustomRecovery`（结构化日志）。

分组：

- `api := r.Group("/api/v1")`；`web := api.Group("/web")`，并 `web.Use(LoginGate(...))`
- `web` 下：`registerApiRoutes(r, api, web)`；引擎级：`registerTopLevelRoutes(r)`（`/share/:code` 也在此阶段注册）

**LoginGate 白名单**（仅强制登录开启时生效）：`/api/v1/web/auth/`、`/api/v1/web/setup/`、`/api/v1/web/health`、`/api/v1/web/options`；`/cli/install.sh`、`/cli/fuzhan.sh` 恒匿名。

### 7.2 `/api/v1/web`（`routes_web.go`）

基础与监控

- `GET /health`、`GET /ready`
- `GET /monitor/metrics`（AuthRequired + Prometheus）
- `GET /monitor/storage|access`（AuthRequired+RequireAdmin）、`/monitor/keywords|recent|rankings|hot-downloads`
- `GET /options`、`GET /runtime-config`（AuthOptional）

认证 `/auth`

- `GET /captcha`（+CaptchaRateLimit）、`POST /register`、`POST /login`、`POST /logout`、`GET /oauth2/{status,authorize,callback}`

分享与下载

- `GET|HEAD /share/:code`（LoginGate）
- `POST /download/ticket`（TicketRateLimit）、`GET|HEAD /download/*path`（DownloadRateLimit）
- `GET /admin/search/*query`、`GET|HEAD /admin/download/*path`（AuthRequired+RequireAdmin）
- `GET /files/search-records`（AuthOptional + ResponseWrapper）

私有存储 `/private`

- `GET /private/files/*path`、`GET /private/quota`、`DELETE /private/files/:code`
- `/private/uploads`：`POST /sessions`、`GET /sessions/:id`、`POST /sessions/:id/resume`、`DELETE /sessions/:id`、`POST /chunks`、`POST /finalize`

索引扫描

- `/admin/index`：`GET /scan/progress`、`GET /scan/status`

初始化 `/setup`

- `GET /status`；仅未初始化可用的：`POST /save`、`POST /validate-dir`、`GET /network/interfaces`、`GET /default-config`

通知 `/notifications`

- `GET ""`、`POST /read`、`POST /merge`

配置 `/config`

- `GET ""`、`POST ""`、`POST /ldap/test`、`POST /oauth2/test`、`POST /webhook/test`、`GET /webhook/events`、`GET /partitions`

临时分享 `/temp`

- `GET /temp/list/*path|/quota|/client-ip`、`GET /:code`、`GET|HEAD /:code/download`、`DELETE /:code`
- `/temp/uploads`：`POST /sessions`、`GET /sessions/:id`、`POST /sessions/:id/resume`、`DELETE /sessions/:id`、`POST /chunks`、`POST /finalize`

公开上传 `/uploads`

- `POST /sessions`、`GET /sessions/:id`、`POST /sessions/:id/resume`、`DELETE /sessions/:id`、`POST /chunks`、`POST /finalize`
- URL 上传：`POST /url-tasks`、`POST /url-tasks/probe`、`GET /url-tasks/:id`、`POST /url-info`
- 会话/任务：`GET /sessions`、`GET /url-tasks`、`GET /url-tasks/stream`（SSE）、`POST /url-tasks/:id/cancel|retry|checksum`、`DELETE /url-tasks/:id`

公开文件 `/files`

- `GET /files/list/*path`、`GET /files/depends/:id`
- `GET /files/recent`、`GET /files/recent/carousel`
- `GET /files/preview/*path`、`GET /files/preview-chunk/*path`、`POST /files/checksum`
- `GET|PUT /files/records/:id/notes`
- `POST /files/dependencies`、`DELETE /files/dependencies/:id`
- `GET /files/record/*path`、`GET /files/scan/status`

搜索

- `GET /search/*query`、`GET /search-suggest`、`GET /search-spellcheck`、`GET /search-related`

登录后（`protected := web.Group("/")` + AuthRequired）

- `POST /auth/refresh`、`GET /auth/user`、`PUT /auth/password`、`PUT /auth/profile`、`DELETE /auth/account`
- `GET /user/login-logs`、`GET /user/records`、`GET /user/stats`
- `POST /files/url-info`
- `/examples`：`POST /upload`、`POST /rename`、`POST /validation/upload`

WebDAV / CLI

- WebDAV：`api.Group("/webdav")` → `webdav.SetupUnifiedRouter(...)`
- `/api/v1/cli`：`GET /`、`GET /search/*query`、`GET /list/*path`、`GET /install.sh`、`GET /fuzhan.sh`

### 7.3 管理后台 `/api/v1/web/admin`（`routes_admin.go`）

索引/任务/用户

- `POST /admin/index/scan`、`POST /admin/index/scan/trigger`
- `GET|POST /admin/users`、`PUT /admin/users/:id/reset-password`、`PUT /admin/users/:id/disabled`、`DELETE /admin/users/:id`
- `GET /admin/sessions`、`POST /admin/sessions/cleanup`、`POST /admin/sessions/cleanup-all`
- `GET /admin/online-ips`
- `POST /admin/records/clear`、`POST /admin/records/delete`

安全/告警/行为分析

- `GET /admin/security/events`、`GET /admin/security/events/unread-summary`、`POST /admin/security/events/read`、`GET /admin/security/events/stats`
- `GET /admin/alert-messages`、`GET /admin/alert-messages/unread-count`、`POST /admin/alert-messages/read`
- `GET /admin/behavior-analytics/{summary,trend,top-files,sources,failures,file,heatmap,aggregate,lifecycle}`

证书/网络/文件

- `POST /admin/tls/certificate`、`POST /admin/tls/private-key`、`GET /admin/network-interfaces`
- `GET /admin/files/list/*path`、`GET /admin/files/download-records`、`POST /admin/files/move`、`DELETE /admin/files`

URL 任务/后台任务

- `GET /admin/url-tasks`、`POST /admin/url-tasks/:id/retry`、`DELETE /admin/url-tasks/:id`
- `GET /admin/tasks`、`GET /admin/tasks/history`、`GET /admin/tasks/:id`、`POST /admin/tasks/:id/cancel`

API Key / 索引记录 / Open API / 日志 / 资源

- `/admin/api-keys`：`GET|POST ""`、`GET /:id`、`PUT /:id/status`、`DELETE /:id`
- `/admin/index`：`GET /records`、`GET /stats`、`POST /check`、`DELETE /records/:id`、`GET /duplicates`、`POST /duplicates/:id/keep`、`PUT /records/:id/notes`、`GET|POST /dependencies`、`GET /records/:id/dependencies`、`DELETE /dependencies/:id`
- `GET /admin/open-api/stats`、`GET /admin/open-api/scopes`
- `GET /admin/logs/files`、`GET /admin/logs/tail`、`GET /admin/logs/stream`
- `/admin/resource`：`GET /snapshot`、`GET /history`、`GET /stream`
  - `/snapshot` 返回完整数据（`hardware` 必带、`serverRecent`/`programRecent` 为内存缓冲全量，约 720 点），供首屏与轮询兜底；
  - `/stream`（SSE）帧结构相同但为**增量**：`serverRecent`/`programRecent` 仅带最后 `recentFramePoints`（2）个点，前端按 `timestamp` 合并去重（客户端上限 720 点）。新增字段时注意前端是合并而非整段替换。

### 7.4 顶层路由（`routes_top.go`）

- `/cli`：`GET /`、`GET /search/*query`、`GET /list/*path`、`GET /install.sh`、`GET /fuzhan.sh`（与 `/api/v1/cli` 等价别名）
- `/download/*path`：`GET|HEAD`
- WebDAV 顶层别名：`constants.WebDAVAliasPathPrefix`
- `/lite`：`GET|POST /*path`（老旧浏览器轻量页面；IE 访问 SPA 会 302 到 `/lite`）
- `/api/v1/open`（scope 校验）：
  - `GET /files/list/*path`、`GET /files/search`、`GET /files/download/*path`（Reader scope）
  - `GET /files/info/*path`、`POST /files/exists`、`POST /files/upload`、`POST /files/upload/init|chunk|finalize`、`GET /files/upload/:id`
  - `GET /files/duplicates`、`POST /files/rename`、`DELETE /files`（任意 delete scope）
  - `GET /docs`（301 → `/scalar.html`）、`GET /openapi.json`：独立分组，走 `LoginRequiredWhenForced`，未开启强制登录时匿名可读，开启后需凭证，不做 scope 校验
  - API Key 的 `allowedPaths` 路径授权：越界操作 403；列表/搜索/重复文件按允许路径裁剪（搜索建议词在启用路径限制时为空）；Web 会话不受限制

---

## 8. 后台 worker 与默认 cron（`workers.go`）

| 函数 | 行为 | 默认表达式 |
|---|---|---|
| `runCronJob` | 通用 cron 执行器，仅 5 段解析；表达式空/非法回退默认值；每 30s 唤醒以响应取消 | — |
| `runCleanupWorker` | 启动立即清理 + 周期清理：公开/私有/临时上传会话 + 过期临时文件；写 `cleanup` 任务记录 | `0 * * * *` |
| `runIndexScanWorker` | 延迟 `scan_start_delay_seconds`（≤0 用 10s）后，按 public/temp 检查；temp 未启用则跳过；进行中跳过。私有文件元数据由数据库维护，不做文件系统扫描 | 启动一次 |
| `runScanTimerWorker` | 轮询（5s）等初始扫描结束 → `StartScanTimer` + `StartWatcher`，并注册配置保存钩子重启 | — |
| `runConsistencyCheckWorker` | 等扫描结束 → 全量扫描进行中则跳过 → `RunConsistencyCheck` | `0 3 * * *` |
| `runSearchReconcileWorker` | 等扫描结束 → `idxSearch` 为 nil 则不启动 → `ReconcileIndex` 并记录任务进度/详情 | `0 5 * * *` |
| `runFTPWorker` | 启动 FTP，ctx 取消时 Stop | — |

---

## 9. 关键业务链路

### 9.1 分片上传（统一抽象）

- `services.UploadSessionService` + `UploadStorage` + `UploadFinalizer` 策略化，公开 / 私有 / 临时三套 handler 共用流程。
- 接口序列：`POST /uploads/sessions`（创建，含配额/限流校验）→ `POST /uploads/chunks`（分片，不受频繁限流）→ `POST /uploads/finalize`（合并、写索引、算哈希）→ 失败可 `GET /uploads/sessions/:id` + `POST /uploads/sessions/:id/resume` 续传，或 `DELETE` 取消。
- 分片大小：`upload.chunk_size`（默认 10M）；单文件上限 `upload.max_file_size`（默认 16G）。
- 公开上传的根目录选择与存储后端由 `publicStorages` 决定（本地物理路径 / 远端对象存储）。

### 9.2 索引与检索

- 双写：SQL 索引表（`file_records_*`）+ Bluge 全文检索索引。
- 增量同步：`SyncFile` 持有 `lockWrite` 串行化“查-建”，写库后调用 `collapsePublicRecordDuplicates` 收敛同路径重复行；SQLite/PostgreSQL 另建部分唯一索引 `idx__file_records_public__active_path`（`WHERE status='active'`）兜底，MySQL 依赖应用层锁。
- 哈希：xxh3 哈希 worker 在“文件落盘稳定”后计算，通过 `FileSettledGate` / `WriteGate` / `FileGate` 串行化写入与哈希，避免读到未完成文件。
- 一致性：`consistency.go` 周期比对文件系统与索引表，结束后调用 `DedupePublicFileRecords` 兜底收敛同一路径重复记录（保留最早行、合并 uploader_ip/哈希/备注/下载次数、重定向 operation_records/file_dependencies 引用后软删）；`snapshot`/`duplicates`/`dependencies` 供后台页面使用。
- 检索对齐：`ReconcileIndex` 输出 `IndexedTotal` / `MissingIndexed` / `OrphansRemoved`。
- 搜索接口：全文（`/search/*query`，最多 1000 条）、联想（`search-suggest`）、纠错（`search-spellcheck`）、相关词（`search-related`）。

### 9.3 下载

- 免登录路径：`/share/:code`、`/temp/:code/download`、匿名 FTP RETR、WebDAV GET/HEAD，共用 `download.rate_limit`（限流 + 失败锁定）。
- 浏览器下载先取 ticket（`POST /download/ticket`）再走 `/download/*path`。
- 下载计数由批量记录器维护（`StopDownloadCountRecorder` 关机排空），支持回填与重算。

### 9.4 URL 下载

- 提交 `POST /uploads/url-tasks` 入队；调度器 `NewURLDownloadDispatcher` 按 `max_concurrent` 并发、`max_retries` 重试；SSRF 防护（默认拒绝内网/回环/保留地址）。
- 进度：`GET /uploads/url-tasks/stream`（SSE）或 `GET /uploads/url-tasks/:id`。

---

## 10. 前端（`ui/`）

### 10.1 技术栈与构建

- Vue 3（`^3.5.24`）+ Vue Router 4 + Element Plus（`~2.4.0`，按需自动导入）+ Vite + `@vitejs/plugin-legacy` + ECharts + axios + marked/dompurify + hash-wasm + `@scalar/api-reference`。
- `unplugin-auto-import`（`src/auto-imports.d.ts`）与 `unplugin-vue-components`（`src/components.d.ts`）自动生成类型文件。
- npm scripts：`dev`（vite，端口 3000）、`build`、`build:scalar`、`build:all`、`preview`。包管理：yarn（`.yarnrc.yml`，构建脚本用 `yarn.cmd`）。
- `vite.config.js`：`base:'/'`、`@ → src`、`outDir='../server/web'`、`emptyOutDir:true`、`manualChunks`（vue-vendor、axios-vendor）、`minify:'esbuild'`、build 时 `drop:['console','debugger']`、资源分目录（`assets/js`、`assets/icons`、`assets/[ext]`）。
- dev 代理到 `http://localhost:8888`：`/api`、`/download`、`/share`。
- scalar 独立构建：`scalar.vite.config.js` → `server/scalar/scalar.html`（+ `scalar/assets`）。

### 10.2 路由（`src/router/index.js`）

| 路径 | 组件 | 备注 |
|---|---|---|
| `/files` | HomeView | 公开文件主页 |
| `/files/:pathMatch(.*)*` | HomeView | 目录深链 |
| `/setup` | SetupView | 初始化向导 |
| `/login` | LoginView | 登录/注册 |
| `/temp` | TempView | 临时分享 |
| `/recent` / `/hot` | RecentView / HotView | 最近 / 热门 |
| `/oauth2/callback` | OAuth2CallbackView | OAuth2 回调 |
| `/user` | PersonalLayout（requiresAuth） | 子路由：`info`、`storage`、`files`→PrivateStorageView、`login-logs`、`records` |
| `/admin` | AdminLayout（requiresAuth+requiresAdmin） | `dashboard`、`files`(+`files/:pathMatch(.*)*`)、`uploads`、`tasks`、`openapi`、`duplicates`、`records`、`online`、`resource`、`behavior-analytics`、`users`、`security`、`logs`、`api-keys`、`webhook`、`mime`、`settings` |

重定向：`/private → /user/files`、`/settings → /admin/settings`、`/admin/api-keys → /admin/openapi`；末尾 catch-all。

视图目录：`views/*.vue`（Home/Login/Setup/Temp/Recent/Hot/User/PrivateStorage/Settings/Admin* 等）+ `views/admin/*.js`（表格列、上传、文件操作、搜索、URL 任务、僵尸会话等 composable）+ `views/home/*.js` + `views/settings/*`（各配置分区组件与 diff 工具）。

### 10.3 API 层（`src/api/index.js`）

- axios 实例 `baseURL='/api/v1/web'`、`withCredentials:true`（HttpOnly Cookie 承载登录态，前端不存真实 token，`localStorage.token` 仅作已登录哨兵 `__cookie__`）。
- 请求头带 `X-Anonymous-ID`；`skipSuccessToast` 可用；写操作统一成功提示（去重 1.6s）；401 清本地态并派发 `auth:changed`，403 回 `#/files`。
- 导出 API 对象：`AuthApi`、`ConfigApi`、`ResourceApi`、`FileApi`、`UploadApi`、`IndexApi`、`DependencyApi`、`SearchApi`、`FileRecordApi`、`AdminApi`、`PrivateApi`、`TempApi`、`SetupApi`、`SystemApi`、`MonitorApi`、`BehaviorAnalyticsApi`、`NotificationApi`、`ApiKeyApi`、`PersonalApi`、`SecurityApi`、`AlertApi`。

---

## 11. 构建、运行、测试

### 11.1 构建（`build.ps1`）

```
.\build.ps1                 # 先 ui 后 server（windows/amd64 + linux/amd64）
.\build.ps1 ui              # 仅前端：yarn build → server/web；scalar.html 缺失时补 build:scalar
.\build.ps1 server w|l      # 仅后端；-p 打包 fuzhan-<version>-<os>-<arch>.zip
.\build.ps1 -v 2026.11.08   # 指定版本（默认当天日期）
```

- 后端命令：`go build -ldflags "-s -w -X fuzhan/internal/version.Version=<version>" -o bin/fuzhan[.exe] .`（`cwd=server`）；版本号由 `server/internal/version` 提供，`/health`、`/ready`、OpenAPI `info.version` 共用。
- 正式发布的完整流程（SemVer 版本号、CHANGELOG 定稿、打包、打标签、Gitee/GitHub Releases、回验）见 `docs/development/development-guide.md#版本发布`。
- 前端失败会中止后端构建（显式检查 `$LASTEXITCODE`）。
- `assets.go` 的 `//go:embed` 依赖 `server/web/index.html`、`web/assets`、`fuzhan.sample.yaml`、`scalar/scalar.html`、`scalar/assets`；**缺失任一目录会导致编译失败**，需先构建前端。

### 11.2 本地开发

```powershell
# 终端1：后端（默认 8888，配置文件 server/fuzhan.yaml）
cd server; go run .
# 终端2：前端（3000，代理到 8888）
cd ui; yarn dev
```

常用参数：`go run . -c ./fuzhan.yaml`、`go run . -create-admin admin:pass`、`fuzhan -extract-web`。

### 11.3 测试

- Go：`cd server; go test ./...`；现有测试含 `main_routes_full_test.go`（复刻整棵路由树的**冲突**回归测试 `TestMainRouteTreeNoConflict`，外加 `/cli` 重定向环、IE UA 判断用例）、`route_conflict_test.go`、middleware 单测。**新增 `*path` / `:param` 路由时注意与之冲突，必要时补充该测试**（它不含逐条路由快照，新增路由不会自动失败）。
- 前端：`playwright.config.js`、`playwright.smoke.config.js`（`ui/` 根目录），无 npm test 脚本。

---

## 12. 约定与陷阱

1. **路由注册集中在 3 个文件**：`routes_web.go` / `routes_admin.go` / `routes_top.go`。`/api/v1/web` 之外的路径全部在 `routes_top.go`。
2. **强制登录（`account.require_login`）优先级最高**：LoginGate 白名单极小（auth/setup/health/options），临时分享也会被拦。
3. **热重启语义**：配置保存 → 停全部 worker → 重建 Engine/FTP → 可选 DB 热切换。任何持有跨轮状态的资源必须走 `AddOnSaveHook` 或显式 Stop（如 Watcher、检索索引 writer、各批量写入器）。
4. **初始化前只监听 127.0.0.1**（`LocalhostWhenSetup`），避免 setup 窗口被抢注。
5. **时间统一 UTC**；软删除用 `gorm.DeletedAt`；索引名统一 `idx__{table}__{cols}`。
6. **大小/配额字符串**用 `ParseQuotaString`（B/K/M/G/T，含 KB 等写法）。
7. **路径**：所有用户可配置路径走 `${work_dir}` 展开；数据/日志固定在 `<work_dir>/data`、`<work_dir>/logs`。
8. **前端构建产物不提交语义**：产物写入 `server/web`、`server/scalar`，属于 build 输出；改动前端后必须重新 `yarn build` 才能被后端 embed 到。
9. **限流分层**：Captcha / Upload（仅控制面：session、finalize、url-tasks）/ Ticket / Download；分片 `/uploads/chunks` 不限流。
10. **cli 脚本白名单**：`/cli/install.sh`、`/cli/fuzhan.sh` 恒匿名可下载。
11. **IE 兜底**：SPA 导航与 NoRoute 时 302 到 `/lite`。
12. **config 保存钩子**：`server.go`（JWT/日志/重启信号）与 `runScanTimerWorker`（扫描定时器/监听器重启）两处，新增热配置项时确认是否需要钩子。
13. **命名不一致是静默失败的最大来源。** `ShouldBindJSON` 对不上 tag 的字段只会留空，不会报错；`c.PostForm("x")` / `c.Query("x")` 写错 key 同样静默返回空串。历史上「无效的会话ID」、验证码校验失效、文件校验值失败都属此类。新增读参一律用手写 snake key，并跑 `go test ./internal/naming/`。
14. **`appconfig/api/handler.go` 的 `SaveConfig` 用裸字符串判断节点是否提交**（`present("root_dirs")`、`raw["storage_backend"]`、`sbFields["alert"]`）。改 DTO 的 `json` tag 时必须同步改这些字符串，否则配置保存会静默丢字段。

---

## 13. 文档索引（深入阅读入口）

- `docs/index.md`：文档总索引（需求 / 设计 / 用户 / 开发）
- `docs/design/architecture.md`、`SYSTEM_DESIGN.md`、`source-tree-analysis.md`、`data-models.md`、`component-inventory.md`、`TEMPORARY_FILE_MANAGEMENT.md`
- `docs/design/api/api-contracts.md`、`API_INTERFACE.md`
- `docs/requirements/00~08`：功能模块规格与测试计划
- `docs/user/user-guide.md`、`FTP_WEBDAV_GUIDE.md`、`系统配置与资源规划.md`
- `docs/development/development-guide.md`
- `.trae/documents/`：安全事件覆盖、Webhook 多端点等专项记录
