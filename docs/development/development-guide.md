# 开发指南

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-22

---

## 目录

- [环境要求](#环境要求)
- [项目结构](#项目结构)
- [快速开始](#快速开始)
- [开发命令](#开发命令)
- [构建部署](#构建部署)
- [配置说明](#配置说明)
- [常见任务](#常见任务)
- [测试](#测试)
- [代码规范](#代码规范)

---

## 环境要求

### 后端 (Go)

| 要求 | 版本 | 说明 |
|------|------|------|
| Go | 1.26.0+ | 后端语言 |
| SQLite | - | 开发环境数据库 (内置) |
| MySQL | 8.0+ | 生产环境 (可选) |
| PostgreSQL | 14+ | 生产环境 (可选) |

### 前端 (Vue)

| 要求 | 版本 | 说明 |
|------|------|------|
| Node.js | 18+ | JavaScript 运行时 |
| Yarn | 1.22+ | 包管理器 |

### 构建工具

| 工具 | 说明 |
|------|------|
| make | Linux/macOS 构建 |
| powershell | Windows 构建 (build.ps1) |

---

## 项目结构

```
fuzhan/
├── server/                  # Go 后端
│   ├── main.go              # 程序入口（命令行 + 生命周期 + 热重启）
│   ├── bootstrap.go         # 依赖装配与启动引导
│   ├── router.go            # 路由装配
│   ├── routes_top.go        # 顶层/公开路由
│   ├── routes_web.go        # 业务接口路由
│   ├── routes_admin.go      # 管理接口路由
│   ├── server.go            # HTTP/HTTPS/FTP/WebDAV 服务生命周期
│   ├── workers.go           # 后台任务调度
│   ├── runctx.go            # 运行上下文
│   ├── internal/            # 核心应用代码（按领域分包）
│   │   ├── access/          # 访问协议（FTP/WebDAV）虚拟文件系统
│   │   ├── accessguard/     # 访问控制与 IP/域名白名单
│   │   ├── admin/           # 管理后台
│   │   ├── appconfig/       # 配置定义、加载、保存与接口
│   │   ├── auth/            # 认证（JWT/API Key/LDAP/OAuth2/RBAC）
│   │   ├── behavioranalytics/ # 行为分析
│   │   ├── cli/             # CLI 接口
│   │   ├── downloadticket/  # 下载票据
│   │   ├── file/            # 文件操作
│   │   ├── filedir/         # 目录与配额
│   │   ├── ftp/             # FTP/FTPS 服务器
│   │   ├── githubReleaseGetter/ # GitHub Release 同步
│   │   ├── index/           # 文件索引
│   │   ├── middleware/      # Gin 中间件
│   │   ├── models/          # 数据模型
│   │   ├── monitor/         # 系统监控
│   │   ├── notification/    # 通知系统
│   │   ├── online/          # 在线用户
│   │   ├── openapi/         # OpenAPI 支持
│   │   ├── repositories/    # 数据访问层
│   │   ├── resource/        # 资源监控
│   │   ├── search/          # 搜索与联想
│   │   ├── security/        # 安全审计
│   │   ├── services/        # 跨领域业务服务
│   │   ├── setup/           # 初始化向导
│   │   ├── storage/         # 存储后端（local/s3/oss/webdav）
│   │   ├── temp/            # 临时文件
│   │   ├── utils/           # 日志与通用工具
│   │   ├── webdav/          # WebDAV 服务器
│   │   └── webhook/         # Webhook 推送
│   ├── integration/         # 集成测试
│   ├── web/                 # 嵌入的前端资源
│   └── scalar/              # 嵌入的 OpenAPI 文档页
│
├── ui/                      # Vue 前端
│   ├── src/
│   │   ├── views/           # 页面
│   │   ├── components/      # 组件
│   │   ├── api/             # API 客户端
│   │   ├── store/           # 状态管理
│   │   ├── router/          # 路由
│   │   └── plugins/         # Vue 插件
│   └── package.json
│
├── docs/                    # 文档（requirements/design/user/development）
└── README.md
```

---

## 快速开始

### 1. 克隆项目

```bash
git clone <repo-url>
cd fuzhan
```

### 2. 启动后端

```bash
# 进入后端目录
cd server

# 直接运行
go run .

# 或使用 Air 热重载开发
air

# 或使用构建版本
./fuzhan  # Linux/macOS
./fuzhan.exe  # Windows
```

### 3. 启动前端

```bash
cd ui

# 安装依赖
yarn install

# 开发模式
yarn dev

# 一键生产构建（主应用 + 后端；scalar 产物缺失时自动构建）
.\build.ps1      # Windows；Linux/macOS 用 ./build.sh
```

### 4. 访问应用

打开浏览器访问: `http://localhost:5173`

---

## 开发命令

### 后端 (server/)

| 命令 | 说明 |
|------|------|
| `go run .` | 直接运行 |
| `cd server && air` | 热重载开发 |
| `go build -ldflags "-s -w" -o fuzhan` | 生产构建 |
| `go test ./...` | 运行所有测试 |
| `go test ./integration/...` | 集成测试 |
| `go test -v ./...` | 详细输出 |
| `go test -cover ./...` | 测试覆盖率 |

### 前端 (ui/)

| 命令 | 说明 |
|------|------|
| `yarn dev` | 开发服务器 (localhost:5173) |
| `yarn build` | 生产构建（仅主应用，输出到 `server/web`） |
| `yarn build:scalar` | 强制重建 scalar 文档页（输出到 `server/scalar`） |
| `yarn build:all` | 主应用 + scalar 强制全量重建 |
| `yarn preview` | 预览构建 |
| `yarn test` | Playwright 测试 |

> **scalar 按需构建逻辑**
>
> scalar（OpenAPI 文档页）是独立构建，产物输出到 `server/scalar`，与主应用解耦。它是稳定产物、可长期复用，因此默认**不随每次构建全量重建**：
>
> - 一键流程（`build.ps1`/`build.sh`）先执行 `yarn build` 构建主应用，再检测 `server/scalar/scalar.html` 是否已存在——存在即跳过、缺失才自动执行 `yarn build:scalar` 生成（存在性判定在脚本层用 `Test-Path`/`[[ -f ]]`，不引入 node/js）。
> - 后端 `go build` 通过 `//go:embed` 将 `server/web` 与 `server/scalar` 一起打进二进制，因此编译后端前必须先保证前端产物齐全。
> - 当 scalar 源码更新后需要重建时，请使用强制命令 `yarn build:scalar` 或 `yarn build:all`。

---

## 构建部署

### 后端构建

**构建脚本:**
```powershell
# Windows
.\build.ps1 [ui|server]
.\build.ps1      # 构建前端和后端

# Linux/macOS
chmod +x build.sh
./build.sh [ui|server]
./build.sh       # 构建前端和后端
```

一键打包的前端步骤由 `build.ps1`/`build.sh` 编排：主应用（`yarn build`）总会重建，scalar（`server/scalar`）仅在产物缺失时自动构建、已存在则跳过，以避免不必要的全量重建。若更新了 scalar 源码，请先手动执行 `yarn build:scalar` 或 `yarn build:all` 重建后再打包。

**输出文件名格式:**
```
{project}-{YYYY.MM.DD}-{os}-{arch}
```

示例:
- `fuzhan-2026.07.22-windows-amd64.exe`
- `fuzhan-2026.07.22-linux-amd64`
- `fuzhan-2026.07.22-darwin-arm64`

**支持平台:**
- Windows: amd64, 386
- Linux: amd64, 386, arm64
- macOS: amd64, arm64

**输出目录:** `bin/`

### 服务安装

安装为系统服务的参数与平台支持见[用户手册 - 安装为系统服务](../user/user-guide.md#安装为系统服务)。

### 前端构建

```bash
cd ui
yarn build
```

构建产物输出到 `ui/dist/`，会被嵌入到后端二进制文件中。

---

## 配置说明

配置文件 `fuzhan.yaml` 的完整字段说明、示例与热更新行为见[用户手册 - 配置指南](../user/user-guide.md#配置指南)。

### 环境变量

| 变量 | 说明 |
|------|------|
| `FUZHAN_CONFIG` | 配置文件路径 |
| `FUZHAN_PORT` | 服务端口 |

### 配置热重载

修改 `fuzhan.yaml` 后，程序自动检测变更并热重启：
- 文件系统监听配置变更，自动触发 worker 热重启；
- 日志级别等可热更新配置即时生效；
- 存储后端、HTTP/HTTPS/FTP 端口、数据库等底层配置需重启进程生效。

### FTP / WebDAV

协议启用方式、认证与客户端连接见[FTP/WebDAV 使用指南](../user/FTP_WEBDAV_GUIDE.md)。

---

## 常见任务

### 添加新的 API 端点

1. 在 `server/internal/{module}/` 创建或编辑 handler 文件
2. 定义请求/响应结构体
3. 在 `server/routes_web.go`（或对应领域路由文件）注册路由
4. 在 `server/internal/services/` 实现业务逻辑（如需要）
5. 添加单元测试

示例:
```go
// server/internal/example/handler.go
func NewHandler(c *gin.Context) {
    var req NewRequest
    if err := c.ShouldBind(&req); err != nil {
        utils.HandleBadRequest(c, "参数错误", err.Error())
        return
    }
    // 业务逻辑
    utils.HandleSuccess(c, http.StatusOK, "成功", result)
}

// server/routes_web.go - 在对应路由组中注册
api.POST("/new-endpoint", deps.NewHandler)
```

### 添加新的前端页面

1. 在 `ui/src/views/` 创建页面组件
2. 在 `ui/src/router/index.js` 注册路由
3. 如需要，在 `ui/src/api/index.js` 添加 API 调用

### 添加新的数据模型

1. 编辑 `server/internal/models/` 中的模型文件
2. 数据库会自动迁移（GORM AutoMigrate）

### 调试技巧

**后端日志:**
```bash
# 开启 DEBUG 日志
# 在 fuzhan.yaml 中设置
log:
  level: "debug"

# 查看日志文件
tail -f logs/*.log
```

**关闭服务:**
```bash
# 发送 SIGINT 信号（Linux/macOS）
kill -SIGINT <pid>

# 使用 --force-stop 参数
fuzhan --force-stop
```

---

## 测试

### 后端测试

```bash
cd server

# 运行所有测试
go test ./...

# 运行特定包测试
go test ./internal/auth

# 显示覆盖率
go test -cover ./...

# 集成测试
go test ./integration/...

# 详细输出
go test -v ./...
```

### 前端测试

```bash
cd ui

# 运行 Playwright 测试
yarn test
```

### 集成测试覆盖

集成测试文件位于 `server/integration/`：

| 测试文件 | 说明 |
|----------|------|
| `setup_handler_test.go` | 初始化向导测试 |
| `health_handler_test.go` | 健康检查测试 |
| `recent_handler_test.go` | 最近上传测试 |
| `monitor_handler_test.go` | 监控指标测试 |

---

## 代码规范

### Go 代码规范

- 使用 `gofmt -w` 格式化（Go 语言官方约定 tab 缩进，提交前校验 `gofmt -l` 应为空）
- 遵循 `Effective Go` 规范
- 错误处理: `if err != nil { return err }`
- import 分组：标准库 / 第三方 / 本地包（`fuzhan/` 前缀）
- 使用 `internal/` 目录组织核心代码

### 数据库交互规范

项目支持 **SQLite / MySQL / PostgreSQL** 三种数据库，数据库代码必须同时兼容三者。

**1. 一律通过 ORM（GORM）完成**

- 所有数据库读写必须使用 GORM 的模型与条件构造器（`Model` / `Where` / `Select` / `Joins` / `Create` / `Save` / `Updates` / `Delete` 等）；模型必须定义 `TableName()`。
- 新增/变更表结构与索引统一通过模型定义 + `AutoMigrate` 完成；确需手工处理索引时，使用 `db.Migrator().CreateIndex()` / `HasIndex()` / `DropIndex()`，不得手写 `CREATE INDEX` DDL。

**2. 确需原生 SQL 时（特殊情况）**

复杂聚合、批量更新等确需 `db.Exec` / `db.Raw` / `gorm.Expr` 的场景，必须同时满足以下条件：

- **兼容三种数据库**，不得使用任一引擎专有语法/函数/类型，例如：
  - `||` 字符串拼接（MySQL 默认按逻辑 OR 解析）→ 改用 `CONCAT()` 或 ORM 表达式；
  - 两参数 `TRIM(str, chars)`（MySQL 不支持）→ 改用 `TRIM(BOTH chars FROM str)` 或 ORM 表达式；
  - `CAST(x AS TEXT)`（MySQL 应使用 `CHAR`）；
  - `CREATE INDEX IF NOT EXISTS`（MySQL 不支持）→ 改用 Migrator；
  - 反引号标识符 `` `col` ``（PostgreSQL 不支持）→ 直接写列名或交由 ORM 处理；
  - 其它引擎专有函数（如 `IFNULL` / `strftime` / `ON CONFLICT` 等）。
- **参数化绑定**：值一律使用 `?` 占位符，禁止拼接用户输入；标识符（表名/列名/排序字段）只能来自白名单，禁止拼接外部输入。
- 就近注释说明为何无法用 ORM 表达，以及兼容性依据。

**3. 表名与标识符**

- 表名统一取自模型 `TableName()`（如 `db.Table(models.FileRecordPublic{}.TableName())`），禁止在 SQL 中硬编码表名字符串。
- 动态列名/排序字段必须先经白名单映射再使用。

### Vue 代码规范

- 组件名: PascalCase
- 组件文件: PascalCase.vue
- 样式: 使用 Less
- 遵循 Vue 3 组合式 API

---

## 常见问题

### 端口被占用

```bash
# Windows
netstat -ano | findstr :8080
taskkill /PID <pid> /F

# Linux/macOS
lsof -i :8080
kill -9 <pid>
```

### 数据库锁定

```bash
# SQLite 锁定时
rm fuzhan.db-journal
```

### 前端依赖问题

```bash
cd ui
rm -rf node_modules
yarn install
```

---

## 目录权限

运行时需要以下目录的写权限：

| 目录 | 用途 |
|------|------|
| `./data/` | 共享文件存储 |
| `./chunks/` | 上传分片临时存储 |
| `./temp_files/` | 临时文件存储 |
| `./private_files/` | 私有文件存储 |
| `./logs/` | 日志文件 |
| `./fuzhan.db` | SQLite 数据库 |