# 插件化扩展方案（Plugin Architecture Proposal）

> 目标：在不改动核心的前提下，把 Fuzhan 的业务能力（路由、后台任务、认证通道、存储后端等）拆分为可独立启用/停用的插件，实现「按需裁剪、增量演进」。

## 1. 背景与动机

当前服务端在 `bootstrap.go` 中一次性装配全部服务与 Handler，随后经由 `runCtx → newRouter → run` 注册路由与启动后台任务。随着功能不断叠加，暴露出的问题：

- **装配耦合**：[bootstrap.go](../../server/bootstrap.go) 里 30+ 个依赖逐一手写进 `runCtx`，新增功能必然触碰主装配流程。
- **全量生效**：FTP / OpenAPI / WebDAV / 行为分析 / 资源监控 / GitHub Release 同步等能力各自维护 `Enabled` 开关，但代码始终被编译、初始化、注册，无法真正按需裁剪。
- **生命周期散落**：每个 worker 在 [server.go](../../server/server.go) 的 `run()` 里用 `workerStart(wm, name, fn)` 手工登记，新增后台任务要改主循环。
- **路由注册分散**：[routes_web.go](../../server/routes_web.go) / [routes_admin.go](../../server/routes_admin.go) / [routes_top.go](../../server/routes_top.go) 手工分组，插件路由无统一挂载点。

本方案引入一套轻量的**插件框架**，把「核心骨架」与「可选能力」解耦。

## 2. 设计原则

1. **核心最小化**：核心只负责 配置加载、DB、JWT、鉴权中间件、静态资源、插件调度、路由骨架（登录闸门/响应包装）。
2. **能力即插件**：一切可选能力（FTP、WebDAV、OpenAPI、行为分析、资源监控、GitHub Release、通知、Webhook、外部存储……）都实现为插件。
3. **编译期内建 + 运行期裁剪**：插件随主二进制编译（避免动态加载的版本/签名复杂度），但**是否启用、启用哪些，由配置决定**。未来若需真正的二进制级插件，可在该框架上叠加 go-plugin（见 §8）。
4. **零侵入热重载**：插件生命周期与现有 worker 热重启机制对齐，配置变更走既有的 `appconfig.AddOnSaveHook` + worker 重启通道，无需重新编译。
5. **依赖显式化**：插件通过统一的 `Host`（宿主接口）获取 DB、配置、日志、鉴权、索引等服务，禁止插件反向 import `package main`。

## 3. 总体架构

```
┌─────────────────────────────────────────────────────────┐
│                    main / bootstrap                       │
│   加载配置 → 基础设施(DB/JWT/日志) → 初始化插件注册表       │
└───────────────────────────┬─────────────────────────────┘
                            │ 注册内置插件（编译期）
┌───────────────────────────▼─────────────────────────────┐
│                  plugin.Registry                          │
│  Register(name, desc) → Activate(config) → Build(ctx)    │
│  ┌──────────┬──────────┬──────────┬───────────┬─────────┐│
│  │ ftp      │ webdav   │ openapi  │ githubRel │  ...    ││
│  │ (插件)   │ (插件)   │ (插件)   │ (插件)    │         ││
│  └──────────┴──────────┴──────────┴───────────┴─────────┘│
└───────────────────────────┬─────────────────────────────┘
                            │ 每个插件可向宿主声明
                            ▼
   ┌─────────────┬───────────────┬──────────────┬────────────┐
   │ Router 扩展  │ Worker 扩展    │ 中间件扩展    │ 事件/钩子    │
   │(gin routes) │(ctx 后台任务)  │(鉴权链)      │(webhook…)   │
   └─────────────┴───────────────┴──────────────┴────────────┘
```

**关键点**：`Registry` 是编排层，只做「按配置求出应激活的插件清单 → 依声明收集路由/worker/中间件 → 交给宿主装配」。插件本身不感知 gin 主循环细节。

## 4. 插件接口定义

### 4.1 宿主接口 Host（宿主向插件暴露的能力）

放在 `internal/plugin/host.go`，核心只依赖 `appconfig / gorm / gin / utils`，不依赖任何具体业务包：

```go
// Host 宿主提供给插件的运行能力集合。
// 插件通过它访问基础设施，从而避免反向依赖 package main。
type Host interface {
    DB() *gorm.DB
    Config() *appconfig.Config
    // 配置热保存钩子注册（复用既有 AddOnSaveHook 语义）
    OnConfigSave(func())
    // 路由挂载点（宿主会在正确时机调用插件收集到的路由）
    // Web 组已带 登录闸门/认证/响应包装 等基础中间件，插件无需重复处理
    Logger() *zap.Logger
    // 供插件读取/复用核心服务
    Auth() *services.AuthService
    Index() *index.Service
    Record() repositories.AuditStore
    Task() *services.TaskService
    // 根目录名称映射（FTP/WebDAV/OpenAPI 共用）
    RootNames() map[string]string
    // 健康检查注册：插件可向 /health 追加自检项
    RegisterHealthCheck(name string, fn func() error)
}
```

> 宿主可以是一个结构体（持有 runCtx 里的引用），`Host` 接口仅用于切断 import 环、约束插件可访问面。

### 4.2 插件生命周期接口

```go
package plugin

// Plugin 是所有插件的统一契约。实现方式为「注册闭包」，不强制插件自定义类型名。
type Plugin interface {
    // ID 全局唯一插件标识，如 "ftp" "webdav" "openapi" "github_release"
    ID() string
    // Name 人类可读名称（用于管理界面展示）
    Name() string
    // Depends 依赖的其他插件 ID（激活前先激活依赖，按依赖序启动）
    Depends() []string
    // Active 由配置解析结果决定插件是否启用（不启用则不 Build）
    DefaultEnabled() bool
    // Provide 返回插件能力声明：路由、worker、中间件、事件监听
    Provide(host Host, cfg json.RawMessage) (ProvideResult, error)
}

// ProvideResult 描述该插件向宿主贡献的扩展点集合（可全部为空）。
type ProvideResult struct {
    Routes       func(web, admin, top *gin.RouterGroup)  // 路由组挂载
    Workers      []Worker                                // 后台任务
    Middlewares  []func(*gin.Context)                    // 全局中间件（随 newRouter 注入）
    Handlers     []HandlerDeferred                        // 需在 newRouter 内联的处理器（如 FTP Handler）
}
```

一个插件实例化后既可注入路由，也可启动后台 worker，还能注册健康检查——职责由 `ProvideResult` 统一承载。

### 4.3 注册与激活

```go
// registry.go
type Registry struct {
    mu       sync.Mutex
    host     Host
    builders map[string]Builder  // id -> 插件构造器
    active   []*runtimePlugin    // 当前激活的插件实例
}

// Register 在 init() 或 bootstrap 早期注册插件构造器（编译期内建）。
func (r *Registry) Register(b Builder)

// Activate 依据配置解析所有插件开关与参数，构建激活集合。
// 应严格按 Depends 拓扑排序后逐个 Provide；失败插件仅告警并跳过，不影响核心启动。
func (r *Registry) Activate(raw map[string]json.RawMessage)
```

配置形态（`fuzhan.yaml`）：

```yaml
plugins:
  ftp:
    enabled: false
    port: 21            # 兼容旧配置：也可直接读 server.ftp
  webdav:
    enabled: true
  openapi:
    enabled: false
    whitelist: [...]
  github_release:
    enabled: false
    repo: owner/name
```

> **兼容策略**：插件优先读取自己命名空间下的 `plugins.<id>`；未配置时回退读取旧的顶层配置节（如 `server.ftp`），保证老配置文件不改即用。

## 5. 与现有热重载机制的衔接

现有主循环（[server.go](../../server/server.go) `run()`）每轮迭代会：

1. `hotSwapDB()` —— 数据库驱动/DSN 变更时切换连接
2. `newRouter()` —— 重建 gin Engine 与 FTP Handler
3. `workerStart(wm, name, fn)` —— 登记并启动 worker

插件化后主循环**几乎不变**，仅将「启动所有 worker」的硬编码序列替换为：

```go
// 1. 每轮迭代先从注册表收集当前激活插件的 ProvideResult
results := rt.pluginRegistry.BuildResults()   // 依赖已按拓扑激活

// 2. 路由：newRouter 内部改为
for _, pr := range results {
    if pr.Routes != nil {
        pr.Routes(api.Group("/web"), adminGroup, top)
    }
}
// FTP Handler 等「需先于路由构造」的处理器由 Handlers 提供

// 3. worker：主循环把插件的 Workers 并入 workerStart 序列
for _, w := range results.Workers {
    workerStart(wm, w.Name, w.Run)
}
```

**热更语义保持不变**：配置保存 → `appconfig.AddOnSaveHook` 的被注册回调 → 插件内联处理（如重读限流参数）+ 宿主向 `configCh` 发信号 → 主循环停止旧 worker、热切换 DB、`newRouter` 重建 → 新配置下的插件集合重新激活。

插件开关从 `enabled: true` 改为 `false`，热重启后该插件的路由/worker 自动消隐，无需重启进程。

## 6. 插件化改造清单（现状模块 → 插件映射）

| 现状代码 | 建议插件 ID | 贡献的扩展点 | 依存 |
|---|---|---|---|
| [internal/ftp](../../server/internal/ftp) + router.go FTP 组装 | `ftp` | Handlers(FTPHandler)、Worker(runFTPWorker) | auth, index |
| [internal/access/webdav](../../server/internal/access/webdav) | `webdav` | Routes(webdav 组)、Handlers(buildWebDAVHandler) | auth, index |
| [internal/openapi](../../server/internal/openapi)（含 ip 白名单/限流/统计） | `openapi` | Routes(open_api 组)、中间件(限流/IP) | auth(rbac), index |
| [internal/githubReleaseGetter](../../server/internal/githubReleaseGetter) | `github_release` | Worker(同步 worker)、Routes(管理/状态接口)、事件订阅 | task, notification |
| [internal/notification](../../server/internal/notification)（webhook 落地载体） | `notifications` | Routes、Worker(投递) | task |
| [internal/behavioranalytics](../../server/internal/behavioranalytics) | `behavior_analytics` | Routes | record |
| [internal/resource](../../server/internal/resource)（服务器资源监控） | `resource_monitor` | Worker(采集)、Routes | record |
| [internal/security](../../server/internal/security)（DDoS/事件中心） | `security` | 中间件(可选)、Worker | record |
| 外部存储（S3/MinIO/OSS/WebDAV 后端，t5 引入） | `storage_s3` / `storage_oss` … | 存储后端实现（Host 存储接口实现） | model |
| 日志分文件 + 统一错误日志（t7 引入） | ——（核心能力，不做插件） | —— | —— |
| 日志查看页面（t8） | ——（核心管理页面） | —— | —— |

**归类原则**：
- **核心（非插件）**：appconfig、models、auth 认证、middleware 基础链、index 索引、file 上传下载核心、services、utils、setup、monitor 基础统计、health。这些是骨架，始终保持激活。
- **可选（插件）**：上表所列能力型模块，均有独立开关与清晰边界，优先插件化。

> 插件化是**渐进式重构**，不是一次性的重写。每一批迁移都保持「路由/worker/配置形态」不变，只改变装配来源，测试可对照原行为验证。

## 7. 分阶段实施计划

### 阶段 A：建立框架（最小可用）
1. 新建 `internal/plugin`：`host.go`、`registry.go`、`types.go`（Plugin/ProvideResult/Worker）。
2. 在 `bootstrap.go` 建 `Registry` 并注入 Host（从 runCtx 提取引用）。
3. 主循环 `run()` 改为遍历 `results` 启动 worker；`newRouter()` 遍历 `results` 挂路由。
4. 空插件（无实际能力）端到端跑通，验证对现有功能零影响（全套测试绿灯）。
5. commit。

### 阶段 B：迁移两个样板插件
6. 先把 **ftp**、**webdav** 迁为插件（最具边界性：涉及 Handler/Worker/路由组装）。
7. 配置兼容层生效（`plugins.ftp` 未配置时回退 `server.ftp`）。
8. 对比迁移前后：启动日志、FTP 上传、WebDAV PROPFIND/下载 行为一致。
9. commit。

### 阶段 C：批量迁移与事件总线
10. 迁移 openapi / github_release / notifications / behavior_analytics / resource_monitor / security。
11. 引入**事件总线**（`internal/plugin/eventbus`）：插件可 `Subscribe(topic, handler)`，宿主核心在文件上传/下载/删除等时机 `Publish`；webhook、通知、行为分析、GitHub 触发改为订阅事件，替代散落的直接调用。
12. commit。

### 阶段 D：外部存储与高级形态
13. 存储后端抽象（`Host.Storage()` 接口）在 t5 完成后，把 S3/MinIO/OSS/WebDAV 后端注册为 `storage_*` 插件，共用同一套 `RootNames`/配额/索引语义。
14. 如需二进制级插件：评估 [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)（gRPC 子进程）或 Yaegi/插件仓库方案；此项为可选项，不阻塞内建插件体系。
15. commit。

## 8. 风险与注意事项

| 风险 | 应对 |
|---|---|
| **循环导入**：插件想反向用 main 的类型 | 用 `Host` 接口切断；插件只依赖 `internal/plugin` 与核心包 |
| **热重启期间插件状态丢失**（如限流器计数） | 与现状一致：worker 重建即状态重置，属可接受语义；需跨重启保持的放 DB |
| **依赖拓扑乱序** | `Registry` 按 `Depends` 做拓扑排序并检测环 |
| **配置兼容性破坏** | 插件配置读取回退旧顶层节；缺省值等价于旧行为 |
| **一次迁移过多回归难定位** | 阶段 B 只迁 2 个插件做样板并全量测试；后续每次迁移独立 commit |
| **插件内错误拖垮核心启动** | 激活/Provide 失败只告警跳过，核心始终可启动；插件可查运行日志 |

## 9. 本文档范围说明

- 本文档是**设计方案**（归属设计类文档），给出接口草案与演进路径，不含具体实现。
- 实施时将按阶段 A→D 落地，每个阶段完成后提交一次，并同步更新本文档的「已落地方案」章节。

---

相关文档：[architecture.md](../architecture.md) · [SYSTEM_DESIGN.md](../SYSTEM_DESIGN.md) · [component-inventory.md](../component-inventory.md)
