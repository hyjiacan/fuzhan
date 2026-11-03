# Webhook 多端点配置与事件覆盖补全

## Context

当前 Webhook 只支持一个 `urls string[]` 列表，所有地址共用全局 `secret/timeout/max_retries`，前端也只能用一个 textarea 维护多行地址、用一个按钮统一测试。用户要求：

1. **每个回调地址一条记录**，可单独启用、命名、配置（密钥/超时/重试/事件过滤）并单独测试；
2. **补全事件覆盖度**：当前仅 `task.*`、`security.event`、`config.saved`，文件、用户、认证、私有文件、临时文件、URL 任务、会话、系统证书等关键操作都没有事件。

本次改造保持旧配置 `webhook.urls` 可用（读取时自动视为端点），新配置统一写入 `webhook.endpoints`。

## 方案

### 1. 后端配置模型（`server/internal/appconfig/config.go`）

新增：

```go
type WebhookEndpoint struct {
    Name           string            `yaml:"name,omitempty"`
    URL            string            `yaml:"url"`
    Enabled        *bool             `yaml:"enabled,omitempty"` // nil 视为启用
    Secret         string            `yaml:"secret,omitempty"`  // 空继承全局
    TimeoutSeconds int               `yaml:"timeout_seconds,omitempty"` // 0 继承全局
    MaxRetries     int               `yaml:"max_retries,omitempty"`      // 0 继承全局
    Events         []string          `yaml:"events,omitempty"`           // 空=全部；支持 task.* / *
    Headers        map[string]string `yaml:"headers,omitempty"`          // 接收方鉴权请求头
    Query          map[string]string `yaml:"query,omitempty"`            // 追加到回调 URL 的查询参数
}
```

认证分两层：

- 发送方身份：沿用全局/端点 `secret` 的 `X-Fuzhan-Signature` HMAC-SHA256；
- 接收方鉴权：通过端点 `headers`（如 `Authorization: Bearer xxx`、`X-API-Key`）
  或 `query`（如 `?token=xxx`）完成，两类可同时使用。

密钥展示与日志策略：配置接口向管理员返回明文（与现有 `secret` 一致）；
发送日志与错误信息对 URL query 中 `token/key/secret/access_token/signature`
等参数值及自定义鉴权头的值做脱敏，仅输出 `***`。

`WebhookConfig` 增加 `Endpoints []WebhookEndpoint`，保留 `URLs` 兼容旧配置，并新增：

- `EffectiveEndpoints() []WebhookEndpoint`：`Endpoints` 非空时返回副本；否则把旧 `URLs` 转为启用端点；
- `(WebhookEndpoint) IsEnabled() bool`：`Enabled == nil || *Enabled`；
- `(WebhookEndpoint) MatchesEvent(name string) bool`：空列表匹配全部；支持精确、`*`、前缀通配 `task.*`。

`config_load.go` 默认值处同时检查 `Endpoints`，启用但没有任何地址时告警。

### 2. DTO 与保存链路

`server/internal/appconfig/dto.go`：

- 新增 `WebhookEndpointDTO`（camelCase：`name/url/enabled/secret/timeoutSeconds/maxRetries/events/headers/query`）；
- `WebhookConfigDTO` 增加 `endpoints`；
- GET 配置输出 `EffectiveEndpoints()`（旧 `urls` 会在界面上呈现为端点记录）；
- 增加 DTO ↔ 配置转换辅助函数，供 `ToDTO`、`ApplyWebhookDTO`、测试接口共用。

`server/internal/appconfig/api/handler.go`：

- 保存 Webhook 时写入 `Endpoints`，并把旧 `URLs` 置空（完成迁移）；`updates.Add("webhook.endpoints", ...)` 使用 `[]interface{}{map[string]interface{}{...}}`；
- `TestWebhookConfig` 改为返回结构化结果，body 为 `WebhookConfigDTO` 时可只传单个端点（前端「单条测试」）；
- 新增 `ListWebhookEvents`（`GET /api/v1/web/config/webhook/events`），返回事件目录，路由在 `server/routes_web.go` 注册。

**关键前置修复**：`server/internal/appconfig/writer.go` 的 `setNodeValue` 目前对嵌套 `[]string`/`map[string]interface{}` 会写成 `[a b]` 字符串，无法保存端点列表。需改为递归构建 YAML 节点，正确处理 `[]string`、`[]interface{}`、`map[string]interface{}` 及其中的 bool/int/string；并在 `writer_test.go` 增加端点嵌套结构的保存回读测试。

### 3. 派发器（`server/internal/webhook/dispatcher.go`、`test_push.go`、`models.go`）

- `dispatch` 改为遍历 `cfg.EffectiveEndpoints()`：跳过禁用/空 URL/不匹配事件；单端点 `secret/timeout/maxRetries` 为 0 或空时回退全局；
- `sender` 支持端点级 `headers/query`：请求前设置自定义头，并把 query 参数合并进 URL（保留 URL 已有参数）；
- 新增日志脱敏函数，对 URL query 中敏感键值及鉴权头值输出 `***`，失败日志统一使用脱敏后的 URL；
- `TestPush(cfg)` 返回结构化结果 `[]TestResult{name,url,success,statusCode,elapsedMs,error}`；测试时忽略端点启用状态与事件过滤（保证未启用的行也能测试），并完整应用 headers/query 鉴权；
- 为返回真实 HTTP 状态码，`sender.send` 改为返回 `(statusCode int, err error)`，派发器忽略状态码，测试接口用于展示 401/403 等鉴权结果；
- `models.go` 扩展事件常量，并提供 `EventCatalog()`（`[{event,desc,category}]`）供前端动态渲染，避免前后端列表漂移。

事件目录共 33 个（6 个现有 + 27 个新增），均有真实触发点。审计来源事件统一 payload：
`{operator, clientIp, operation, target, result, statusCode, durationMs}`。

#### 作业、安全与配置（现有）

| 事件 | 说明 | 触发点 | 派发条件 |
|---|---|---|---|
| `task.started` | 系统作业开始 | `task_service.dispatchTaskEvent` | 作业启动 |
| `task.finished` | 系统作业完成 | 同上 | 作业完成 |
| `task.failed` | 系统作业失败 | 同上 | 作业失败 |
| `task.canceled` | 系统作业被取消 | 同上 | 作业取消 |
| `security.event` | 安全事件 | `security.writer` | 写入安全事件库 |
| `config.saved` | 配置保存成功 | 配置保存接口 | 保存成功 |

#### 文件

| 事件 | 说明 | 触发 operation | 派发条件 |
|---|---|---|---|
| `file.upload_started` | 创建上传会话，开始上传 | `upload.session.create` | 创建成功 |
| `file.upload_canceled` | 取消上传会话 | `upload.session.cancel` | 取消成功 |
| `file.uploaded` | 上传会话完成，文件写入最终路径 | `upload.session.finalize` | finalize 成功 |
| `file.renamed` | 文件/目录重命名 | `file.rename` | 成功 |
| `file.moved` | 文件/目录移动 | `file.move` | 成功 |
| `file.deleted` | 文件/目录删除 | `file.delete` | 成功 |

> `file.uploaded` 不使用「合并」措辞：finalize 负责校验分片是否完整、把文件写入最终路径并收尾；
> 本地存储是临时文件重命名，只有远端存储内部才会按序拼接分片对象。

#### 私有文件、临时文件、URL 下载任务

| 事件 | 说明 | 触发 operation | 派发条件 |
|---|---|---|---|
| `private.downloaded` | 私有文件被下载 | `private.download` | 成功 |
| `private.deleted` | 私有文件被删除 | `private.delete` | 成功 |
| `temp.downloaded` | 临时文件被下载 | `temp.download` | 成功 |
| `temp.deleted` | 临时文件被删除 | `temp.delete` | 成功 |
| `temp.upload_canceled` | 临时文件上传取消 | `temp.upload.session.cancel` | 成功 |
| `url_task.canceled` | URL 下载任务取消 | `upload.url.cancel` | 成功 |
| `url_task.retried` | URL 下载任务重试 | `upload.url.retry` | 成功 |
| `url_task.deleted` | URL 下载任务删除 | `upload.url.delete` | 成功 |
| `url_task.checksummed` | URL 下载任务校验和更新 | `upload.url.checksum` | 成功 |

#### 用户、认证、会话与系统

| 事件 | 说明 | 触发 operation | 派发条件 |
|---|---|---|---|
| `user.registered` | 新用户注册 | `auth.register` | 成功 |
| `user.created` | 管理员创建用户 | `admin.user.create` | 成功 |
| `user.deleted` | 管理员删除用户 | `admin.user.delete` | 成功 |
| `user.status_changed` | 启用/禁用用户 | `admin.user.set_disabled` | 成功 |
| `user.password_changed` | 密码修改/重置 | `auth.change_password`、`admin.user.reset_password` | 成功 |
| `auth.login_success` | 本地/OAuth2 登录成功 | `auth.login`、`auth.oauth2.login` | `err == nil` |
| `auth.login_failed` | 本地/OAuth2 登录失败 | 同上 | `err != nil`（含限流锁定） |
| `session.cleaned` | 清理在线会话 | `admin.session.cleanup`、`admin.session.cleanup_all` | 成功 |
| `system.cert_uploaded` | 上传 TLS 证书 | `admin.cert.upload` | 成功 |
| `system.key_uploaded` | 上传 TLS 私钥 | `admin.key.upload` | 成功 |
| `system.records_cleared` | 清理记录 | `admin.records.clear` | 成功 |
| `system.records_deleted` | 删除记录 | `admin.records.delete` | 成功 |

明确不派发的 operation：`search`（高频只读噪音）、`request.rejected`（已由
`security.event` 覆盖 401/403/429）、`config.update`（已由 `config.saved` 覆盖）。

事件过滤语法：端点 `events` 为空表示全部；支持精确匹配（`file.uploaded`）、
前缀通配（`file.*`、`task.*`）与 `*`。

### 4. 事件触发（`server/internal/middleware/audit.go`）

在 `LogOperation` 末尾集中映射并 `webhook.Dispatch(event, "audit", payload)`，无需改 70+ 个调用点：

- 映射表按 operation 前缀/名称归类；`config.update`、`search`、`request.rejected` 跳过（前者已有 `config.saved`，后者已有安全事件）；
- 除 `auth.login/auth.oauth2.login` 外仅在 `err == nil` 时派发（事件名代表已完成动作）；登录成功/失败分别派发；
- payload：`{operator, clientIp, operation, target, result, statusCode, durationMs}`；
- 先判断 `webhook.Enabled()` 短路，避免未启用时构造负载。
- `middleware -> webhook -> appconfig` 无 import cycle（`appconfig` 根包不依赖 middleware）。

**顺带修复**：`server/internal/auth/handler.go` 中被登录锁定分支调用 `LogOperation(..., nil)`，会被误判为登录成功；改为传入错误，既修正审计结果也避免误发 `auth.login_success`。

### 5. 前端

`ui/src/views/settings/SettingsWebhook.vue` 重写：

- 顶部保留全局开关、默认签名密钥、默认超时/重试、队列容量（提示队列容量需重启生效）；
- 回调地址改为端点记录卡片列表：启用开关、名称、URL、事件多选（空=全部，支持全部/分组快捷选择）、密钥/超时/重试（0 或空=继承全局）、单条「测试」按钮与行内结果、删除；
- 每个端点提供「认证方式」预设（UI 仅用于快速填充，不随配置提交）：
  - 无认证
  - Bearer Token（写入 `Authorization: Bearer xxx`）
  - API Key（自定义头名 + 值）
  - URL Token（自定义查询参数名 + 值）
  - 自定义（直接编辑 headers / query 键值对）
- 提供「添加回调地址」「测试全部」；测试全部展示结构化结果列表（含 HTTP 状态码）；
- 事件选项从 `GET /config/webhook/events` 加载，失败时回退内置列表；
- 单条测试请求只携带当前行端点 + 全局默认值。

`ui/src/views/SettingsView.vue`：默认值、加载、保存三处把 `webhook.urls` 改为 `webhook.endpoints`（仍带 `urls` 兜底映射，便于首次加载旧值）。

`ui/src/api/index.js`：`testWebhook(webhookConfig)` 保持路径不变，新增 `webhookEvents()`。

### 6. 文档

- `docs/user/user-guide.md`：新增「Webhook 通知」小节，说明端点记录、事件过滤语法、单条测试、接收方鉴权（Headers / URL Token）、签名头、队列容量重启生效及事件目录入口；
- `docs/requirements/07-系统配置与选项模块.md`：补充 `webhook.endpoints`（含 `headers/query`）配置项与事件目录说明。

## 涉及文件

后端：

- `server/internal/appconfig/config.go`
- `server/internal/appconfig/config_load.go`
- `server/internal/appconfig/dto.go`
- `server/internal/appconfig/writer.go`（+ `writer_test.go`）
- `server/internal/appconfig/api/handler.go`
- `server/routes_web.go`
- `server/internal/webhook/models.go`
- `server/internal/webhook/dispatcher.go`
- `server/internal/webhook/test_push.go`
- `server/internal/middleware/audit.go`
- `server/internal/auth/handler.go`

前端：

- `ui/src/views/settings/SettingsWebhook.vue`
- `ui/src/views/SettingsView.vue`
- `ui/src/api/index.js`

文档：

- `docs/user/user-guide.md`
- `docs/requirements/07-系统配置与选项模块.md`

## 验证

1. `cd server && go build ./... && go vet ./... && go test ./...`
2. `cd ui && npm run build`
3. 手工验证：
   - 旧 `webhook.urls` 配置启动后，设置页显示为端点记录；保存后 yaml 变为 `webhook.endpoints`；
   - 新增两个端点，分别配置不同 secret/事件过滤（一个只订阅 `file.*`），触发文件上传与登录，确认仅对应端点收到；
   - 单条测试只展示该行结果；禁用端点仍可测试；测试全部返回结构化结果；
   - 分别配置 Bearer Header、API Key Header、URL Token 端点，测试接口返回 200 与对应 HTTP 状态码；使用错误 token 时测试结果返回 401/403；
   - 确认发送日志中的 URL 敏感 query 参数与鉴权头值已脱敏为 `***`；
   - 事件目录接口返回完整列表，界面选项与后端一致；
   - 审计日志与原有行为不变，登录限流分支不再产生 `auth.login_success`。
