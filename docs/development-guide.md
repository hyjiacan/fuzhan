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
│   ├── cmd/
│   │   ├── fuzhan/     # 程序入口
│   │   │   └── main.go     # 入口（worker 生命周期 + 热重启）
│   │   └── xxh3sum/        # xxhash 命令行工具
│   │
│   ├── internal/           # 核心应用代码
│   │   ├── access/         # WebDAV / FTP 虚拟文件系统
│   │   ├── admin/          # 管理员功能
│   │   ├── auth/           # 认证（JWT/API Key/LDAP/RBAC）
│   │   ├── cli/            # CLI 接口
│   │   ├── config/         # 配置管理
│   │   ├── file/           # 文件操作
│   │   ├── ftp/            # FTP 服务器
│   │   ├── health/         # 健康检查
│   │   ├── index/          # 文件索引
│   │   ├── middleware/     # 中间件
│   │   ├── models/         # 数据模型
│   │   ├── monitor/        # 系统监控
│   │   ├── notification/   # 通知系统
│   │   ├── openapi/        # OpenAPI 支持
│   │   ├── repositories/   # 数据访问层
│   │   ├── router/         # 路由配置
│   │   ├── services/       # 业务服务
│   │   ├── setup/          # 初始化向导
│   │   └── temp/           # 临时文件
│   │
│   ├── pkg/                # 公共包
│   │   ├── jwt/
│   │   ├── pathutils/
│   │   ├── response/
│   │   └── xxh3/
│   │
│   ├── integration/        # 集成测试
│   └── web/                # 嵌入的前端资源
│
├── ui/                     # Vue 前端
│   ├── src/                # 源码
│   │   ├── views/          # 页面（13个）
│   │   ├── components/     # 组件
│   │   ├── api/            # API 客户端
│   │   ├── store/          # 状态管理
│   │   ├── router/         # 路由
│   │   └── plugins/        # Vue 插件
│   ├── package.json
│   └── vite.config.js
│
├── docs/                   # 文档
└── _bmad-output/           # BMad 流程文档
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
# 进入程序入口目录
cd server/cmd/fuzhan

# 直接运行
go run main.go

# 或使用 Air 热重载开发（从 server 目录）
cd server
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

### 后端 (server/cmd/fuzhan/)

| 命令 | 说明 |
|------|------|
| `go run main.go` | 直接运行 |
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

将程序安装为系统服务，实现开机自启和后台运行。

**命令行参数：**
| 参数 | 说明 |
|------|------|
| `--install` | 安装服务 |
| `--uninstall` | 卸载服务 |
| `--status` | 查看服务状态 |
| `--force-stop` | 强制停止服务 |
| `--name <name>` | 指定服务名称（默认 fuzhan） |
| `--user <user>` | 指定运行用户（Linux/macOS） |
| `-c <path>` | 指定配置文件路径 |
| `--create-admin <user>:<pass>` | 创建管理员用户 |

**使用示例：**
```bash
# 安装服务（使用默认名称）
fuzhan --install

# 指定名称安装
fuzhan --install --name myservice

# 创建管理员
fuzhan --create-admin admin:password123

# 查看服务状态
fuzhan --status

# 卸载服务
fuzhan --uninstall
```

**平台支持：**
| 平台 | 实现方式 | 权限要求 |
|------|----------|----------|
| Windows | sc.exe | 管理员权限 |
| Linux | systemd | sudo |
| macOS | launchd | sudo（系统级）或用户目录 |

### 前端构建

```bash
cd ui
yarn build
```

构建产物输出到 `ui/dist/`，会被嵌入到后端二进制文件中。

---

## 配置说明

配置文件: `fuzhan.yaml`

### 主要配置项

```yaml
# 服务设置
server:
  host: "0.0.0.0"
  port: 8080
  webdav:
    enabled: false
    port: 0

# 数据库
database:
  driver: "sqlite"  # sqlite, mysql, postgres
  dsn: "./fuzhan.db"

# 存储配置
storage:
  public:
    root_dirs:
      - path: "./data/share1"
        quota: "10GB"  # 0 表示无限制

  allowed_extensions: []  # 空表示允许所有类型

  # 私有存储
  private:
    enabled: true
    path: "./private_files"
    quota:
      global: "100G"    # 全局配额
      per_user: "10G"   # 单用户配额
    # 私有文件永久保存，无过期概念（仅临时文件有过期时间）

  # 临时文件
  temp:
    enabled: true
    path: "./temp_files"
    quota:
      global: "10G"
      per_ip: "500M"    # 单IP配额
    default_expire_days: 7
    delete_on_download: true  # 下载后删除

# LDAP 配置（可选）
ldap:
  enabled: false
  server: "ldap://localhost:389"
  base_dn: "dc=example,dc=com"
  bind_dn: ""
  bind_password: ""

# 上传配置
upload:
  chunk_size: 10485760      # 10MB 分片大小
  max_file_size: 17179869184 # 16GB 最大文件
  url_upload:
    enabled: false
    allowed_ip_ranges: []   # CIDR 网段，留空时不限制公网，但会拒绝内网/回环/链路本地等保留地址（安全默认，防 SSRF）

# 预览配置
preview:
  allow_mimes: "text/*,image/*,application/pdf,..."
  allow_exts: "txt,md,log,json,xml,csv,..."
  max_inline_size: "1M"
  text_chunk_size: "100K"

# FTP 服务
ftp:
  enabled: false
  port: 2121
  tls: false

# OpenAPI 配置（可选）
openapi:
  enabled: false
  rate_limit: 100

# 日志
log:
  level: "info"
  directory: "./logs"
  max_size: 100    # MB
  max_backups: 30
  compress: true
```

### 环境变量覆盖

| 变量 | 说明 |
|------|------|
| `fuzhan_CONFIG` | 配置文件路径 |
| `fuzhan_PORT` | 服务端口 |

### 配置热重载

修改 `fuzhan.yaml` 后，程序自动检测变更并热重启：
- 文件系统监听配置变更
- 自动触发 worker 热重启
- 支持动态更新日志级别

### FTP 使用方法

**匿名访问（无需登录）：**
```
服务器: localhost
端口: 2121
用户名: anonymous
密码: (任意或留空)
```

**客户端示例：**

FileZilla:
1. 主机: `ftp://localhost`
2. 端口: `2121`
3. 协议: FTP
4. 加密: 不使用 TLS
5. 登录类型: 匿名

### WebDAV 使用方法

**启用 WebDAV:**
```yaml
server:
  webdav:
    enabled: true
    port: 0  # 0 表示与 HTTP 同端口
```

**访问端点:** `/webdav/`

---

## 常见任务

### 添加新的 API 端点

1. 在 `server/internal/{module}/` 创建或编辑 handler 文件
2. 定义请求/响应结构体
3. 在 `server/internal/router/router.go` 注册路由
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

// server/internal/router/router.go - 在 RegisterAPIRoutes 中注册
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