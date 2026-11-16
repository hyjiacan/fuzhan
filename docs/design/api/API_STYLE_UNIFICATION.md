# HTTP API 风格统一方案

> 状态：规范已定，代码已迁移。规范正文见 [API_STYLE_GUIDE.md](./API_STYLE_GUIDE.md)，本文件记录存量接口的迁移对照与执行进度。
>
> 已确认的决策：
> 1. 命名空间：`/api/v1/web`、`/api/v1/web/admin`、`/api/v1/open`。
> 2. 文件路径参数：只有 GET 用 URL 路径段（`*path`）；POST/PUT/DELETE 用参数（`path`）。
> 3. 正式路径与历史别名并行保留：统一风格的正式路径与既有别名（`/download`、`/cli`、`/webdav`）指向同一 handler、同一鉴权；不删除重复入口，也不新增第二个别名。
> 4. 请求参数与响应字段命名：一律 `snake_case`。
> 5. 唯一例外：开放 API 旧前缀 `/api/open/v1/*` 不保留别名，切换后直接失效。

---

## 1. 统一规范

### 1.1 命名空间

| 命名空间 | 用途 | 鉴权 |
|---|---|---|
| `/api/v1/web/*` | Web 界面（含匿名可访问的公开接口） | LoginGate / AuthRequired |
| `/api/v1/web/admin/*` | 管理后台 | AuthRequired + RequireAdmin |
| `/api/v1/open/*` | 开放 API（原 `/api/open/v1/*`） | API Key + scope |
| `/api/v1/webdav/*`、`/webdav/*` | WebDAV 协议端点（正式路径 + 别名） | Basic Auth / 可选匿名 |
| `/api/v1/cli/*`、`/cli/*`、`/download/*`、`/share/{code}`、`/lite/*` | CLI / 公开入口 / 历史别名 | LoginGate 白名单 |
| `/assets/*`、`/scalar.html`、`/scalar/assets/*` | 静态资源与文档页 | 不变 |

规则：

- 版本号只出现在 `/api/v1` 一处，不再有 `/api/open/v1` 这种版本在末尾的写法。
- 同一能力允许“正式路径 + 别名”并存：正式路径遵循本规范，历史别名保留且行为一致，不再新增第二个别名。
- 正式路径与别名见 1.1.1；规范正文见 [API_STYLE_GUIDE.md](./API_STYLE_GUIDE.md)。
- 开放 API 的旧前缀 `/api/open/v1/*` 不在别名之列：正式路径切换为 `/api/v1/open/*` 后旧前缀直接失效。

#### 1.1.1 正式路径与别名

| 能力 | 正式路径（统一风格） | 别名（保留） |
|---|---|---|
| 文件下载 | `/api/v1/web/download/*path` | `/download/*path` |
| CLI | `/api/v1/cli/*` | `/cli/*` |
| WebDAV | `/api/v1/webdav/*path` | `/webdav/*path` |
| 分享短链 | `/share/{code}` | 无 |
| Lite | `/lite/*` | 无 |

### 1.2 URL 结构

- 全小写；路径段用 `kebab-case`（禁止下划线）：`url_info` → `url-info`。
- 集合资源用复数：`files`、`users`、`sessions`、`tasks`、`api-keys`、`url-tasks`。
- 单资源主键统一写 `{id}`，不再出现 `{uuid}`、`{taskId}`、`{uploadId}`。
- 短码/分享码是业务令牌而非主键，保留 `{code}`。
- 只有 GET 用路径段定位文件/目录：`GET /files/info/*path`、`GET /files/download/*path`、`GET /files/list/*path`。
  - `*path` 的值始终以 `/` 开头，包含根目录名：`/files/info/docs/a.txt` → `path=/docs/a.txt`。
  - 根目录列举用结尾斜杠：`/files/list/`。
- POST/PUT/DELETE 一律用参数传路径，不写进 URL：参数名固定 `path`（完整路径，带前导 `/`），目标路径用 `new_path` / `target_path`。
  - 上传：表单/body 的 `path` 表示目标目录。
  - 重命名：body 的 `path` 表示源，`new_name`/`new_path` 表示目标。
  - 删除：`DELETE /files?path=/docs/a.txt`。
- 动作词表固定为：`list`、`info`、`download`、`preview`、`preview-chunk`、`upload`、`rename`、`delete`、`move`、`chunks`、`finalize`、`checksum`、`duplicates`、`exists`。

### 1.3 路由树约束（gin 通配符）

gin 同一 HTTP 方法下，`*path` 不能与静态段/参数段成为兄弟节点，否则启动 panic。由于只有 GET 使用通配，冲突面大幅缩小：

- GET 路径类接口使用 `/{动作}/*path`（如 `/files/info/*path`），不要注册资源级通配 `/files/*path`。
- 同一 GET 动作下不再挂静态子路由；需要时新增独立动作段。
- POST/PUT/DELETE 不使用通配，上传、重命名、移动、删除保持原有静态路径，只把路径移到参数里。
- 新增通配路由必须同步 `server/main_routes_full_test.go`。

### 1.4 请求参数

| 项 | 规范 |
|---|---|
| 命名 | 全部 `snake_case`：`page_size`、`upload_id`、`chunk_index`、`new_name`、`new_path`、`min_size`、`unread_only`、`source_type`、`client_ip` |
| 分页 | `page`（从 1 开始）+ `page_size`；响应 `pagination.page/page_size/total` |
| 排序 | `sort` + `order=asc|desc` |
| 布尔 | `true` / `false`，不再接受 `1/0` 或“存在即真” |
| 时间 | 查询参数统一 `from` / `to`（Unix 毫秒整数，沿用现有解析器） |
| 文件路径 | GET 放 URL 路径段；POST/PUT/DELETE 用 `path` 参数（body/form/query），不写进 URL |
| 文本检索 | `query`（原 `q`/`query` 混用统一为 `query`） |
| 鉴权 | `api_key` 查询参数保留（兼容 API Key 传递方式） |

响应字段同样统一为 `snake_case`，与请求参数一致（见 1.6）。

### 1.5 方法与状态码

- `GET` 读取（幂等、无副作用），`HEAD` 用于下载/预览探活。
- `POST` 创建或执行动作（上传、重命名、扫描、校验）。
- `PUT` 幂等设置状态/整体更新（用户禁用、备注、配置保存）。
- `DELETE` 删除资源。
- 不引入 `PATCH`；部分更新沿用 `PUT`。
- 状态码固定：`200` 成功、`201` 创建、`204` 无内容、`400` 参数错误、`401` 未认证、`403` 无权限、`404` 不存在、`409` 冲突、`429` 限流、`500` 服务端错误。

### 1.6 响应体

- 所有 JSON 接口统一信封：`{success, code, message, data, errors}`。
- 响应字段全部 `snake_case`，与请求参数一致：结构体 `json` tag 与 `gin.H`/map 字面量 key 同步改名（`fullPath`→`full_path`、`fileSize`→`file_size`、`pageSize`→`page_size`、`uploadId`→`upload_id`、`createdAt`→`created_at`）。截至本计划执行完成，上述「现状」列已不存在，全仓仅剩 snake_case。
- 例外（不套信封）：文件下载/预览二进制流、SSE（`text/event-stream`）、WebDAV、Lite HTML、安装脚本。
- `download_url` 等返回的 URL 字段同步改为新路径。

## 2. 例外清单（不参与 URL 统一）

| 对象 | 原因 |
|---|---|
| WebDAV 的 `PROPFIND/MKCOL/MOVE/PROPPATCH/LOCK/...` 与 `*path` 语义 | RFC 4918 协议固定，客户端不可改；只统一挂载点为 `/webdav` |
| `/share/{code}`、`/lite/*path`、`/cli/install.sh`、`/cli/fuzhan.sh` | 对外暴露的短链/脚本入口，保持稳定 |
| OAuth2 回调参数 `code/state/error_description` | 外部协议参数 |
| `/assets/*`、`/scalar.html`、`/scalar/assets/*` | 静态资源，不属于 API |
| 文件流、SSE、HTML 响应体 | 无法套 JSON 信封 |

## 3. 接口清单（现状 → 目标）

> 说明：所有接口的请求参数按 1.4、响应字段按 1.6 做 `camelCase → snake_case` 全量替换，表中只标注额外变化。
> “不变”指路径与放置方式不变，仅参数/响应字段命名随规范调整。

### 3.1 开放 API（原 `/api/open/v1` → `/api/v1/open`）

> 旧前缀 `/api/open/v1/*` 不保留别名，实施后直接失效（已确认）。

| 方法 | 现路径 | 新路径 | 备注 |
|---|---|---|---|
| GET | `/api/open/v1/files/list?path=` | `/api/v1/open/files/list/*path` | 根目录 `/files/list/` |
| GET | `/api/open/v1/files/search` | `/api/v1/open/files/search` | `query/ext/root` 保留 query |
| GET | `/api/open/v1/files/download?path=` | `/api/v1/open/files/download/*path` | URL 路径段不带前导 `/`，捕获值以 `/` 开头 |
| GET | `/api/open/v1/files/info?path=` | `/api/v1/open/files/info/*path` | 取消上一版 `type/value` |
| POST | `/api/open/v1/files/exists` | `/api/v1/open/files/exists` | body `hashes` 不变 |
| POST | `/api/open/v1/files/upload` | `/api/v1/open/files/upload` | 表单 `file/path/overwrite`，`path` 为目标目录 |
| POST | `/api/open/v1/files/upload/init` | `/api/v1/open/files/upload/init` | body `path/filename/file_size/overwrite` |
| POST | `/api/open/v1/files/upload/chunk` | `/api/v1/open/files/upload/chunk` | 表单 `upload_id/chunk_index/checksum` |
| GET | `/api/open/v1/files/upload/:id` | `/api/v1/open/files/upload/{id}` | 参数名统一 `{id}`，仅 GET 用路径 |
| POST | `/api/open/v1/files/upload/finalize` | `/api/v1/open/files/upload/finalize` | body `upload_id/overwrite` |
| GET | `/api/open/v1/files/duplicates` | `/api/v1/open/files/duplicates` | `min_size/page/page_size` |
| POST | `/api/open/v1/files/rename` | `/api/v1/open/files/rename` | body `path/new_name/new_path` |
| DELETE | `/api/open/v1/files?path=` | `/api/v1/open/files?path=` | 路径走 query 参数 |
| GET | `/api/open/v1/docs` | `/api/v1/open/docs` | 301 到 `/scalar.html` |
| GET | `/api/open/v1/openapi.json` | `/api/v1/open/openapi.json` | Scalar 数据源 |

### 3.2 Web 公共接口（`/api/v1/web`）

#### 健康、监控、选项

| 方法 | 现路径 | 新路径 |
|---|---|---|
| GET | `/api/v1/web/health` | 不变 |
| GET | `/api/v1/web/ready` | 不变 |
| GET | `/api/v1/metrics` | `/api/v1/web/monitor/metrics`（已实施） |
| GET | `/monitor/storage` | 不变 |
| GET | `/monitor/access` | 不变 |
| GET | `/monitor/keywords` | 不变 |
| GET | `/monitor/recent` | 不变 |
| GET | `/monitor/rankings` | 不变 |
| GET | `/monitor/hot-downloads` | 不变 |
| GET | `/options` | 不变 |
| GET | `/runtime-config` | 不变 |

#### 认证与分享

| 方法 | 现路径 | 新路径 |
|---|---|---|
| GET | `/auth/captcha` | 不变 |
| POST | `/auth/register` | 不变 |
| POST | `/auth/login` | 不变 |
| POST | `/auth/logout` | 不变 |
| GET | `/auth/oauth2/status` | 不变 |
| GET | `/auth/oauth2/authorize` | 不变 |
| GET | `/auth/oauth2/callback` | 不变（协议参数例外） |
| GET | `/share/{code}` | 不变（顶层公开入口） |
| HEAD | `/share/{code}` | 不变 |

#### 私有存储

| 方法 | 现路径 | 新路径 | 备注 |
|---|---|---|---|
| GET | `/private/files?path=` | `/private/files/*path` |
| GET | `/private/quota` | 不变 |
| DELETE | `/private/files/{code}` | 不变（短码） |
| POST | `/private/uploads/session` | `/private/uploads/sessions` | body `path` 为目标目录 |
| GET | `/private/uploads/session/{id}` | `/private/uploads/sessions/{id}` | 仅 GET 用路径参数 |
| POST | `/private/uploads/session/{id}/resume` | `/private/uploads/sessions/{id}/resume` | |
| DELETE | `/private/uploads/session/{id}` | `/private/uploads/sessions/{id}` | |
| POST | `/private/uploads/chunk` | `/private/uploads/chunks` | |
| POST | `/private/uploads/finalize` | 不变 | |

#### 票据、管理端搜索下载、记录搜索

| 方法 | 现路径 | 新路径 |
|---|---|---|
| POST | `/download/ticket` | 不变 |
| GET | `/admin/search/*query` | 不变 |
| GET | `/admin/download/*path` | 不变 |
| HEAD | `/admin/download/*path` | 不变 |
| GET | `/files/search-records` | 不变（参数 `query`） |

#### 扫描状态

| 方法 | 现路径 | 新路径 |
|---|---|---|
| GET | `/admin/index/scan/progress` | 不变 |
| GET | `/admin/index/scan/status` | 不变 |

#### 初始化、通知、系统配置

| 方法 | 现路径 | 新路径 |
|---|---|---|
| GET | `/setup/status` | 不变 |
| POST | `/setup/save` | 不变 |
| POST | `/setup/validate-dir` | 不变 |
| GET | `/setup/network/interfaces` | 不变 |
| GET | `/setup/default-config` | 不变 |
| GET | `/notifications` | 不变 |
| POST | `/notifications/read` | 不变 |
| POST | `/notifications/merge` | 不变 |
| GET | `/config` | 不变 |
| POST | `/config` | 不变 |
| POST | `/config/ldap/test` | 不变 |
| POST | `/config/oauth2/test` | 不变 |
| POST | `/config/webhook/test` | 不变 |
| GET | `/config/webhook/events` | 不变 |
| GET | `/config/partitions` | 不变 |

#### 临时文件

| 方法 | 现路径 | 新路径 | 备注 |
|---|---|---|---|
| GET | `/temp/list?path=` | `/temp/list/*path` |
| GET | `/temp/quota` | 不变 |
| GET | `/temp/client-ip` | 不变 |
| GET | `/temp/{code}` | 不变（短码） |
| GET | `/temp/{code}/download` | 不变 |
| HEAD | `/temp/{code}/download` | 不变 |
| DELETE | `/temp/{code}` | 不变 |
| POST | `/temp/upload/session` | `/temp/uploads/sessions` | body `path` 为目标目录 |
| GET | `/temp/upload/session/{uploadId}` | `/temp/uploads/sessions/{id}` | |
| POST | `/temp/upload/session/{uploadId}/resume` | `/temp/uploads/sessions/{id}/resume` | |
| DELETE | `/temp/upload/session/{uploadId}` | `/temp/uploads/sessions/{id}` | |
| POST | `/temp/upload/chunk` | `/temp/uploads/chunks` | |
| POST | `/temp/upload/finalize` | 不变 | |

#### 上传会话与 URL 任务

| 方法 | 现路径 | 新路径 | 备注 |
|---|---|---|---|
| POST | `/uploads/session` | `/uploads/sessions` | body `path` 为目标目录 |
| GET | `/uploads/session/{id}` | `/uploads/sessions/{id}` | |
| POST | `/uploads/session/{id}/resume` | `/uploads/sessions/{id}/resume` | |
| DELETE | `/uploads/session/{id}` | `/uploads/sessions/{id}` | |
| GET | `/uploads/sessions` | 不变 | |
| POST | `/uploads/chunk` | `/uploads/chunks` | |
| POST | `/uploads/finalize` | 不变 | |
| POST | `/uploads/url` | `/uploads/url-tasks` |
| POST | `/uploads/url-probe` | `/uploads/url-tasks/probe` |
| POST | `/uploads/url_info` | `/uploads/url-info` |
| GET | `/uploads/url-task/{taskId}` | `/uploads/url-tasks/{id}` |
| GET | `/uploads/url-tasks` | 不变 |
| GET | `/uploads/url-tasks/stream` | 不变（SSE） |
| POST | `/uploads/url-tasks/{taskId}/cancel` | `/uploads/url-tasks/{id}/cancel` |
| POST | `/uploads/url-tasks/{taskId}/retry` | `/uploads/url-tasks/{id}/retry` |
| POST | `/uploads/url-tasks/{taskId}/checksum` | `/uploads/url-tasks/{id}/checksum` |
| DELETE | `/uploads/url-tasks/{taskId}` | `/uploads/url-tasks/{id}` |

#### 文件浏览、下载、预览、搜索

| 方法 | 现路径 | 新路径 |
|---|---|---|
| GET | `/files/list?path=` | `/files/list/*path` |
| GET | `/files/depends/{id}` | 不变 |
| GET | `/download/*path` | 不变 |
| HEAD | `/download/*path` | 不变 |
| GET | `/files/recent` | 不变 |
| GET | `/files/recent/carousel` | 不变 |
| GET | `/files/preview/*path` | 不变 |
| GET | `/files/preview-chunk/*path` | 不变 |
| POST | `/files/checksum` | 不变（body `path`） |
| PUT | `/files/records/{id}/notes` | 不变 |
| GET | `/files/records/{id}/notes` | 不变 |
| POST | `/files/dependencies` | 不变（body 为记录 ID，仅改 `snake_case`） |
| DELETE | `/files/dependencies/{id}` | 不变 |
| GET | `/files/record?path=` | `/files/record/*path` |
| GET | `/files/scan/status` | 不变 |
| GET | `/search/*query` | 不变（文本检索，保留路径段） |
| GET | `/search-suggest` | 不变 |
| GET | `/search-spellcheck` | 不变 |
| GET | `/search-related` | 不变 |

#### 登录后个人接口

| 方法 | 现路径 | 新路径 |
|---|---|---|
| POST | `/auth/refresh` | 不变 |
| GET | `/auth/user` | 不变 |
| PUT | `/auth/password` | 不变 |
| PUT | `/auth/profile` | 不变 |
| DELETE | `/auth/account` | 不变 |
| GET | `/user/login-logs` | 不变 |
| GET | `/user/records` | 不变 |
| GET | `/user/stats` | 不变 |
| POST | `/get_file_info` | `/files/url-info`（body 传 `url`，探测远端文件信息） |
| POST | `/examples/upload` | 不变 |
| POST | `/examples/rename` | 不变 |
| POST | `/examples/validation/upload` | 不变 |

### 3.3 管理后台（`/api/v1/web/admin`）

除下列变化外，路径保持不变：

| 方法 | 现路径 | 新路径 |
|---|---|---|
| PUT | `/users/:uuid/reset-password` | `/users/{id}/reset-password` |
| PUT | `/users/:uuid/disabled` | `/users/{id}/disabled` |
| DELETE | `/users/:uuid` | `/users/{id}` |
| POST | `/upload-cert` | `/tls/certificate`（建议） |
| POST | `/upload-key` | `/tls/private-key`（建议） |
| GET | `/files/list?path=` | `/files/list/*path` |
| POST | `/files/move` | 不变（body `path` + `target_path`） |
| DELETE | `/files` | 不变（query `path`） |
| GET/POST/PUT/DELETE | `/api-keys`、`/api-keys/{id}` | 不变（`{id}`） |
| POST | `/url-tasks/{id}/retry` | 不变 |
| DELETE | `/url-tasks/{id}` | 不变 |
| GET/POST | `/tasks`、`/tasks/{id}`、`/tasks/{id}/cancel`、`/tasks/history` | 不变 |
| POST | `/index/scan`、`/index/scan/trigger` | 不变 |
| GET | `/index/records`、`/index/stats`、`/index/duplicates` | 不变 |
| POST | `/index/check`、`/index/duplicates/{id}/keep` | 不变 |
| PUT/GET/DELETE | `/index/records/{id}`、`/index/records/{id}/notes`、`/index/records/{id}/dependencies` | 不变 |
| GET/POST/DELETE | `/index/dependencies`、`/index/dependencies/{id}` | 不变 |
| GET | `/open-api/stats`、`/open-api/scopes` | 不变 |
| GET | `/security/events*`、`/alert-messages*`、`/behavior-analytics/*` | 不变（参数 snake_case） |
| GET | `/logs/files`、`/logs/tail`、`/logs/stream` | 不变 |
| GET | `/resource/snapshot`、`/resource/history`、`/resource/stream` | 不变 |
| GET | `/network-interfaces`、`/online-ips`、`/sessions` | 不变 |
| POST | `/sessions/cleanup`、`/sessions/cleanup-all`、`/records/clear`、`/records/delete` | 不变 |
| POST | `/api-keys` 创建 | 不变（body 改 `user_id` / `allowed_paths` / `expires_in`） |

### 3.4 顶层公开入口与协议

| 方法 | 现路径 | 新路径 |
|---|---|---|
| GET/HEAD | `/download/*path` | 不变（别名） |
| GET/HEAD | `/api/v1/web/download/*path` | 不变（正式路径） |
| GET | `/share/{code}` | 不变 |
| GET | `/cli/*`、`/cli/search/*query`、`/cli/list/*path` | 不变（别名） |
| GET | `/api/v1/cli/*` | 不变（正式路径） |
| GET/POST | `/lite/*path` | 不变 |
| WebDAV | `/webdav/*path` | 不变（别名） |
| WebDAV | `/api/v1/webdav/*path` | 不变（正式路径） |
| GET | `/`、`/assets/*`、`/scalar.html`、`/scalar/assets/*` | 不变 |

## 4. 影响面与迁移步骤

1. 后端路由：改 `routes_web.go`、`routes_admin.go`、`routes_top.go`、`router.go` 与 `constants/paths.go`；同步 `main_routes_full_test.go`、`route_conflict_test.go`。
2. Handler 参数：query/body/form 字段统一 snake_case；GET 路径类接口改读 `c.Param("path")`（需处理编码斜杠），POST/PUT/DELETE 从 body/form/query 读取 `path`。
3. 前端：搜索 `ui/src` 中所有 `/api/v1/web/...` 与 `/api/open/v1/...` 字符串，逐个替换；上传、下载、预览、搜索调用同步改。
4. CLI/Lite：`/cli/install.sh`、`/cli/fuzhan.sh` 内的 URL 与 `lite.go` 生成的链接同步改。
5. 下载票据：`internal/downloadticket` 对 `/download/` 前缀的解析保持不变；`/api/v1/web/download/*path` 为正式路径，`/download/*path` 作为别名保留，两者同 handler、同鉴权。
6. OpenAPI：`spec.go` 的 GET 路径改为 `/api/v1/open/*` 与 `{path}` 路径参数（`allowReserved: true`）；写接口保持 `path` 参数；Scalar 示例同步。
7. 文档：`docs/design/api/API_INTERFACE.md`、`api-contracts.md`、`docs/requirements/*`、`project-context.md` §7。
8. 回归：`go test ./...`、前端构建、路由冲突测试、下载/预览/上传手工冒烟。

### 4.1 迁移进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| 0 | 命名守卫测试 `server/internal/naming/naming_test.go` | ☑ |
| 1 | 安全止血：验证码、文件校验值 | ☑ |
| 2.1 | 配置与设置（`appconfig` + Settings/Setup 前端） | ☑ |
| 2.2 | 文件、上传与下载 | ☑ |
| 2.3 | 用户与鉴权 | ☑ |
| 2.4 | 管理后台 | ☑ |
| 2.5 | 索引、监控、资源、行为分析 | ☑ |
| 2.6 | 其余模块 | ☑ |
| 3 | 文档与 OpenAPI 对齐 | ☑ |

## 5. 已确认结论

| 事项 | 结论 |
|---|---|
| `/api/v1/metrics` | 迁到 `/api/v1/web/monitor/metrics`，Prometheus 抓取地址同步修改 |
| 管理端 `upload-cert` / `upload-key` | 改为 `/admin/tls/certificate`、`/admin/tls/private-key` |
| 重复入口是否删除 | 不删除，正式路径与别名并行保留（见 1.1.1） |
| 开放 API 旧前缀 `/api/open/v1/*` | 不保留别名，直接失效 |
| 写操作路径参数 | 统一完整路径 `path`；创建上传会话由 `root_name` + `dir` 合并为 `path=/docs/reports` |
| 文件列表根目录 | 统一用 `/files/list/`（带结尾斜杠） |
| `POST /files/dependencies` | body 是 `file_record_id` / `depends_on_id`，保持 ID 方式，仅改 `snake_case` |
