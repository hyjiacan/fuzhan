# 项目上下文入口

**动手前先读 [`docs/development/project-context.md`](docs/development/project-context.md)**（启动链路、配置体系、后端分层、数据模型、全量路由、worker、前端、构建、约定与陷阱都在里面）。

**新增/修改 HTTP API 时，同时遵守 [`docs/design/api/API_STYLE_GUIDE.md`](docs/design/api/API_STYLE_GUIDE.md)**（命名空间、URL、参数、响应、别名规则）。

除非该文件明确未覆盖，否则不要为"了解项目结构"而通读 `server/`、`ui/`。

## 工作方式（探索预算）

小改动（1~3 个文件）的浏览上限是 **8 个文件**。超过就停下核对是否偏离目标，并向用户说明原因。

1. **先定位，再读**：从 `project-context.md`（§7 路由、§6 数据模型、§10 前端）圈出目标文件，再按需读行区间（用 `offset`/`limit`），不要整文件通读。
2. **先证伪"已经有了"**：动手前用 1 次精准检索确认字段/接口/组件是否已存在（例如 `Grep "download_count"`、`Grep "admin/files"`），能复用就复用，不要新造接口。
3. **不做撒网式检索**：同一个关键词的空结果搜索只允许 1 次；再空就换关键词，或改读上下文文档 / 换 `Glob`，不要连续多轮 `Grep` 同一概念的不同变体。
4. **同一文件不重复通读**：一次任务内对同一文件只读需要的区段；工具结果被截断/清理后沿用已确认结论，不要重读原文。
5. **一次只答一个问题**：把"这个功能涉及哪些文件"拆成"入口在哪 → 数据从哪来 → 落点在哪"，每条结论用一次检索验证，而不是靠反复遍历兜底。

## 修改后必须同步更新上下文文件

新增/修改以下内容时，同步更新 `docs/development/project-context.md` 对应章节：

| 改动 | 需更新章节 |
|---|---|
| 路由（新增/删除/换中间件） | §7 |
| worker / cron | §3.3、§8 |
| 数据模型（AutoMigrate） | §6 |
| 配置项或路径约定 | §4 |
| 前端路由 / 构建产物路径 | §10、§11 |

## 硬规则（易踩坑）

1. 路由只注册在三个文件：`server/routes_web.go`、`server/routes_admin.go`、`server/routes_top.go`（`/api/v1/web` 之外的路径全在 `routes_top.go`）。
2. `server/main_routes_full_test.go` 是**路由冲突**回归测试（复刻路由树，检测 catch-all/静态/参数冲突），不是全量快照；新增 `*path` 或 `:param` 路由时留意与之冲突，必要时补充该测试。
3. 新增模型必须加入 `server/bootstrap.go` 的 `AutoMigrate` 列表，索引名统一 `idx__{table}__{cols}`。
4. 改前端后必须 `yarn build`（产物输出到 `server/web`）才能被 `server/assets.go` 的 `embed` 打进二进制；同理 scalar 需 `yarn build:scalar`。
5. 时间统一 UTC；大小/配额字符串用 `ParseQuotaString`；用户可配置路径走 `${work_dir}` 展开。
6. 新增热生效的配置项时，确认是否需要在 `server.go` 或 `runScanTimerWorker` 的 `AddOnSaveHook` 中处理资源重启。
7. 强制登录（`account.require_login`）优先级最高，LoginGate 白名单仅 auth/setup/health/options；`/cli/install.sh`、`/cli/fuzhan.sh` 恒匿名。

## 常用命令

```powershell
cd server; go run .            # 后端，默认 8888，配置 server/fuzhan.yaml
cd ui; yarn dev                # 前端，3000，代理到 8888
cd server; go test ./...       # 后端测试
.\build.ps1                    # 构建前端 + windows/linux 后端到 bin/
```
