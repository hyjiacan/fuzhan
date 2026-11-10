# HTTP API 风格规范（v1）

> 状态：已确认并生效。新增/修改接口必须遵守；存量接口按 [API_STYLE_UNIFICATION.md](./API_STYLE_UNIFICATION.md) 迁移。
>
> 适用对象：`web`、`admin`、`open`、`cli`、`download` 以及 WebDAV 的挂载与鉴权部分。

## 1. 命名空间

| 命名空间 | 用途 | 鉴权 |
|---|---|---|
| `/api/v1/web/*` | Web 界面（含公开接口） | LoginGate / AuthRequired |
| `/api/v1/web/admin/*` | 管理后台 | AuthRequired + RequireAdmin |
| `/api/v1/open/*` | 开放 API | API Key + scope |
| `/api/v1/webdav/*` | WebDAV | Basic Auth / 可选匿名 |
| `/api/v1/cli/*` | CLI 数据接口 | LoginGate 白名单 |
| `/webdav/*`、`/cli/*`、`/download/*`、`/share/{code}`、`/lite/*` | 公开入口 / 历史别名 | 与正式路径一致 |

规则：

- 版本号只出现在 `/api/v1`，禁止 `/api/open/v1` 这类版本后置写法。
- 正式路径遵循本规范；历史别名保留且必须指向同一 handler，行为与鉴权一致。
- 同一能力最多一个别名，禁止继续新增别名。
- 唯一不保留别名的例外：开放 API 旧前缀 `/api/open/v1/*`，迁移到 `/api/v1/open/*` 后直接失效。

### 1.1 正式路径与别名对照

| 能力 | 正式路径 | 别名 |
|---|---|---|
| 文件下载 | `/api/v1/web/download/*path` | `/download/*path` |
| CLI | `/api/v1/cli/*` | `/cli/*` |
| WebDAV | `/api/v1/webdav/*path` | `/webdav/*path` |
| 开放 API | `/api/v1/open/*` | 无（旧前缀 `/api/open/v1/*` 直接失效） |
| 分享短链 | `/share/{code}` | 无 |
| Lite | `/lite/*` | 无 |

## 2. URL 结构

- 全小写，路径段用 `kebab-case`，禁止下划线：`files/info`、`url-info`、`api-keys`、`url-tasks`。
- 集合资源用复数：`files`、`users`、`sessions`、`tasks`、`dependencies`。
- 单资源主键统一 `{id}`，禁止 `{uuid}`、`{taskId}`、`{uploadId}`。
- 分享码/临时码是业务令牌，保留 `{code}`。
- 文本检索词允许作为路径段（`/search/*query`），其余查询条件一律放 query。

### 2.1 文件路径

| 操作 | 位置 | 示例 |
|---|---|---|
| GET 详情/列表/下载/预览 | URL 路径段 `*path` | `GET /api/v1/open/files/info/docs/a.txt` |
| POST 上传/重命名/移动 | 参数 `path` | `POST /api/v1/open/files/rename` + `path=/docs/a.txt` |
| PUT 更新（涉及路径时） | 参数 `path` | — |
| DELETE 删除 | query `path` | `DELETE /api/v1/open/files?path=/docs/a.txt` |

- `*path` 捕获值以 `/` 开头并包含根目录名：请求 `/files/info/docs/a.txt` 时捕获为 `/docs/a.txt`；URL 中的路径段本身不要再加前导斜杠（避免出现 `//` 与误导示例）。
- 根目录列举用结尾斜杠：`GET /files/list/`。
- 写操作的目标路径用 `new_path` / `target_path`，不得复用 `path`。
- 禁止新增 `?path=` 形式的 GET 文件定位参数。

### 2.2 通配路由约束（gin）

- 同一 HTTP 方法下，`*path` 不能与静态段/参数段互为兄弟，否则启动 panic。
- GET 路径类接口使用 `/{动作}/*path`，禁止注册资源级 `/files/*path`。
- POST/PUT/DELETE 不使用通配。
- 新增通配路由必须同步 `server/main_routes_full_test.go`。

## 3. HTTP 方法与状态码

- `GET` 读取，无副作用；`HEAD` 用于下载/预览探活。
- `POST` 创建或执行动作。
- `PUT` 幂等设置状态或整体更新。
- `DELETE` 删除资源。
- 不引入 `PATCH`。
- 状态码：`200` 成功、`201` 创建、`204` 无内容、`400` 参数错误、`401` 未认证、`403` 无权限、`404` 不存在、`409` 冲突、`429` 限流、`500` 服务端错误。

## 4. 请求参数

- 全部 `snake_case`，包括 query、JSON body、multipart 表单字段：`page_size`、`upload_id`、`chunk_index`、`new_name`、`new_path`、`min_size`、`unread_only`、`client_ip`、`source_type`、`target_type`。
- 分页：`page`（从 1 开始）+ `page_size`。
- 排序：`sort` + `order=asc|desc`。
- 布尔：只接受 `true` / `false`。
- 时间：`from` / `to`，Unix 毫秒整数。
- 文本检索：`query`。
- 兼容参数：`api_key` 保留。

响应字段命名保持现有 JSON 风格，不随本规范调整。

## 5. 响应

- 所有 JSON 接口统一信封：`{success, code, message, data, errors}`。
- 分页数据统一放在 `data.pagination`：`{page, page_size, total}`。
- 例外（不套信封）：文件流、预览流、SSE、WebDAV、Lite HTML、安装脚本。
- 文件下载响应头：`Content-Disposition`（含 RFC 5987 `filename*`）、`Content-Type`。
- 返回给客户端的 URL 字段（如 `download_url`）必须使用正式路径。

## 6. 鉴权

| 命名空间/入口 | 规则 |
|---|---|
| `/api/v1/open/*` | API Key + scope，路径权限在查询前校验 |
| `/api/v1/web/admin/*` | 登录 + 管理员 |
| `/api/v1/web/*` | 按接口挂 AuthOptional / AuthRequired / LoginGate |
| `/webdav/*`、`/api/v1/webdav/*` | WebDAV Basic Auth，行为一致 |
| `/share/{code}`、`/lite/*`、`/cli/install.sh`、`/cli/fuzhan.sh` | 保持现有白名单 |

强制登录（`account.require_login`）优先级最高。

## 7. 例外

- WebDAV 的 `PROPFIND/MKCOL/MOVE/PROPPATCH/LOCK/UNLOCK` 等方法与路径语义由 RFC 4918 固定，不按本规范改名。
- OAuth2 回调参数 `code/state/error_description` 属于外部协议。
- `/config`、`/setup` 的读写 body 与配置文件结构保持一致，沿用配置文件中的字段名，不做 `snake_case` 迁移。
- `/assets/*`、`/scalar.html`、`/scalar/assets/*` 属于静态资源。

## 8. 新增/修改接口检查清单

1. 命名空间正确：`web` / `web/admin` / `open`。
2. 路径全小写 kebab-case，集合用复数。
3. 文件路径：GET 用 `*path`，写操作用 `path` 参数。
4. 主键参数写 `{id}`，短码写 `{code}`。
5. 请求参数全部 snake_case，布尔只接受 `true/false`。
6. 返回统一信封（流/SSE 除外）。
7. 鉴权与 scope 正确，路径权限在操作前校验。
8. 通配路由已检查冲突并同步 `main_routes_full_test.go`。
9. 同步 `server/internal/openapi/spec.go`、`docs/design/api/API_INTERFACE.md`、`docs/development/project-context.md` §7。
10. 新增别名需评审，且只能指向同一 handler。

## 9. 文档维护

本规范变更时，同步更新：

- [API_STYLE_UNIFICATION.md](./API_STYLE_UNIFICATION.md)（存量迁移清单）
- [API_INTERFACE.md](./API_INTERFACE.md)（接口汇总）
- [project-context.md](../../development/project-context.md) §7（全量路由）
