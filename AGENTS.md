# 项目上下文入口

**动手前先读 [`docs/development/project-context.md`](docs/development/project-context.md)**（启动链路、配置体系、后端分层、数据模型、全量路由、worker、前端、构建、约定与陷阱都在里面）。

**新增/修改 HTTP API 时，同时遵守 [`docs/design/api/API_STYLE_GUIDE.md`](docs/design/api/API_STYLE_GUIDE.md)**（命名空间、URL、参数、响应、别名规则）。

除非该文件明确未覆盖，否则不要为"了解项目结构"而通读 `server/`、`ui/`。

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
