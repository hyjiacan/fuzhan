# 浮栈 (Fuzhan)

浮栈 (Fuzhan)｜一座文件客栈，纳四方文件，可暂歇，可长驻。

基于 Go 的轻量级文件共享与存储工具，无需复杂数据库或服务器配置，直接启动即可通过浏览器访问。支持公开分享、临时分享、私有存储、FTP/WebDAV、全文搜索、管理后台等能力。

## 仓库地址

- Gitee: https://gitee.com/hyjiacan/fuzhan
- GitHub: https://github.com/hyjiacan/fuzhan

## 功能特性

### 文件能力

- 多根目录共享：可配置多个共享根目录，每个目录独立配额，逐目录控制是否允许上传
- 文件浏览 / 下载 / 预览（文本、图片、PDF 内联渲染）
- 全文搜索：内建索引引擎（Bluge + 中文分词），实时搜索，单次最多返回 1000 条结果
- 大文件分片上传：10MB 分片、断点续传、进度展示
- 从 URL 直接下载文件到服务器（内置 SSRF 防护，默认拒绝内网/回环/保留地址）
- 文件操作：重命名、移动、删除（按权限控制）

### 分享与存储

- 临时分享：无需登录，按 IP 识别用户，支持过期时间、下载后删除、允许重复下载
- 私有存储：需注册登录，个人独立配额空间，私密分享
- 最近上传 / 热门文件榜单

### 认证与安全

- JWT 认证（bcrypt 密码），支持用户注册、登录、重置密码
- API Key 认证：外部集成接入，支持 IP 限制
- LDAP 认证：企业目录集成
- RBAC 角色权限：admin / user 等细粒度权限管理
- 文件类型白名单控制、僵尸上传清理、安全事件审计（可查看 IP 访问控制与风险记录）

### 协议与集成

- FTP 服务器：匿名/认证访问，可选 TLS/FTPS 加密
- WebDAV 服务器：支持外部应用（如系统文件管理器）挂载访问
- OpenAPI：scalar 在线 API 文档，支持 API Key 鉴权调用与限流
- CLI 接口：通过 HTTP 或命令行搜索、浏览、上传（需登录）

### 管理与可观测

- 管理后台：控制台看板、用户管理、文件管理、上传管理、任务管理、重复文件识别、操作记录
- 在线 IP 统计、资源监控（CPU/内存/磁盘/网络）、行为分析（趋势、时段热度、热门文件、来源分布、失败统计）
- 通知系统、系统设置（WEB/FTP/FTPS/WebDAV/TLS 证书、存储、预览、数据库、访问控制）
- 配置热重载：修改 `fuzhan.yaml` 自动检测并热重启

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.26 + Gin v1.11 |
| 数据库 | SQLite（内置）/ MySQL / PostgreSQL（GORM v1.30） |
| 索引 | Bluge + gse 中文分词 |
| 认证 | JWT (HMAC) / API Key / LDAP / RBAC |
| 监控 | Prometheus 指标 |
| 日志 | Zap + Lumberjack |
| FTP | ftpserverlib |
| 前端 | Vue 3 + Element Plus + Vite |
| 前端工具 | Vue Router / Axios / ECharts / hash-wasm / Less |

## 文档导航

| 我想... | 查看此文档 |
|---------|-----------|
| **快速部署使用** | [用户手册](./docs/user-guide.md) |
| **配置和管理** | [用户手册 - 配置指南](./docs/user-guide.md#配置指南) |
| **FTP / WebDAV** | [FTP/WebDAV 使用指南](./docs/FTP_WEBDAV_GUIDE.md) |
| **二次开发** | [开发指南](./docs/development-guide.md) |
| **查看 API** | [API 接口文档](./docs/api-contracts.md) |
| **了解架构** | [架构文档](./docs/architecture.md) |
| **源码结构** | [源码树分析](./docs/source-tree-analysis.md) |
| **系统资源规划** | [系统配置与资源规划](./docs/系统配置与资源规划.md) |

![列出目录](/docs/1.png)
![搜索文件](/docs/2.png)

## 快速开始

### 1. 下载安装

从 [Releases](https://gitee.com/hyjiacan/fuzhan/releases) 下载对应平台的二进制文件：

- Windows: `fuzhan.exe`
- Linux / macOS: `fuzhan`

将文件保存到任意目录，Linux/macOS 下先加执行权限：

```bash
chmod +x ./fuzhan
```

### 2. 首次运行

直接启动：

```bash
# Windows
.\fuzhan.exe

# Linux/macOS
./fuzhan
```

首次启动后打开浏览器访问 `http://localhost:8080`，根据初始化向导完成配置：

- 服务端口（默认 8080）
- 共享根目录（可多个，独立配额）
- 临时文件 / 私有存储（可选）
- 管理员账号

自动生成 `fuzhan.yaml` 配置文件。配置完成后即可使用。

### 3. 配置文件说明

`fuzhan.yaml` 位于程序同目录，核心结构如下：

```yaml
server:
  host: "0.0.0.0"      # 监听地址
  port: 8080           # 服务端口
  webdav:
    enabled: false     # WebDAV 开关
    port: 0            # 0 表示与 HTTP 同端口

database:
  driver: "sqlite"     # sqlite / mysql / postgres
  dsn: "./fuzhan.db"

storage:
  public:
    root_dirs:
      - path: "./data/share1"
        quota: "10GB"          # 目录配额，0 表示无限制
        allow_upload: true     # 是否允许上传
  temp:
    enabled: true
    path: "./data/temp"
    quota:
      global: "10G"            # 全局总配额
      per_ip: "100MB"          # 单 IP 配额
    default_expire_days: 7
    delete_on_download: false
  private:
    enabled: true
    path: "./data/private"
    quota:
      global: "100G"
      per_user: "1GB"

upload:
  chunk_size: 10485760         # 10MB 分片
  max_file_size: 17179869184   # 最大文件 16GB
  url_upload:
    enabled: false
    allowed_ip_ranges: []      # URL 上传源 IP 白名单（留空默认拒绝保留地址）
    max_concurrent: 2          # 全局同时进行的 URL 上传任务数（所有用户共享，超出排队）
    max_retries: 3             # 失败后自动重试次数（仅超时/5xx/408/429 等可重试错误，4xx 不重试；重试任务排到队尾）

ftp:
  enabled: true
  host: "0.0.0.0"
  port: 2121
  tls: false
  cert: ""                     # TLS 证书
  key: ""

preview:
  allow_mimes: "text/*,image/*,application/pdf,..."
  allow_exts: "txt,md,log,json,xml,csv,..."

openapi:
  enabled: false
  rate_limit: 100

log:
  level: "info"
  directory: "./logs"
  max_size: 100
  max_backups: 30
  compress: true
```

> 修改配置后程序会自动检测并热重载。生产环境可将程序安装为系统服务，见下方「服务安装」。

### 4. 开发环境

#### 构建脚本

```powershell
# Windows
.\build.ps1                      # 构建前端和后端（使用当前日期作为版本）
.\build.ps1 -v 2026.09.23        # 指定版本号
.\build.ps1 ui                   # 仅构建前端
.\build.ps1 server               # 仅构建后端
.\build.ps1 server l             # 仅构建 Linux/amd64 后端
.\build.ps1 -p                   # 打包
```

```bash
# Linux/macOS/WSL
./build.sh                       # 构建前端和后端（使用当前日期作为版本）
./build.sh -v 2026.09.23         # 指定版本号
./build.sh ui                    # 仅构建前端
./build.sh server                # 仅构建后端
```

**输出文件名格式:** `{project}-{YYYY.MM.DD}-{os}-{arch}`

示例:
- `fuzhan-2026.09.23-windows-amd64.exe`
- `fuzhan-2026.09.23-linux-amd64`
- `fuzhan-2026.09.23-darwin-arm64`

**支持平台:**
- Windows: amd64, 386
- Linux: amd64, 386, arm64
- macOS: amd64, arm64

**输出目录:** `bin/`

> 跨平台编译说明：Windows 下默认仅编译 Windows 版本，编译 Linux/macOS 版本需要 WSL2 或 Linux 环境。

#### 单独开发

后端开发（程序入口在 `server/cmd/fuzhan`）：

```bash
cd server/cmd/fuzhan
go run main.go
# 或从 server 目录使用 Air 热重载: air
```

前端开发：

```bash
cd ui
yarn install
yarn dev    # http://localhost:5173
```

> 后端构建通过 `//go:embed` 将前端产物 (`server/web`) 与 scalar 文档页 (`server/scalar`) 一起打进二进制，因此编译后端前需先构建前端。`build.ps1`/`build.sh` 会自动编排这一流程。

## 使用指南

### 1. 文件浏览与预览

- 主页显示所有共享根目录，点击进入子目录
- 文本、图片、PDF 支持内联预览
- 页面支持亮色 / 暗色 / 跟随系统主题切换

### 2. 文件上传

- 上传按钮或拖拽上传
- 支持本地文件上传和从 URL 下载到服务器
- 大文件自动分片上传、支持断点续传
- 上传进度实时展示

### 3. 文件搜索

- 顶部搜索框实时搜索
- 内建索引，支持中文关键词、文件名与扩展名匹配

### 4. 临时分享

无需登录，按 IP 识别用户：

- 上传文件生成访问链接与提取码
- 可设置过期时间
- 可设置「下载后删除」「允许重复下载」

### 5. 私有存储

注册登录后使用：

- 个人文件存储空间（独立配额）
- 私密分享
- 文件管理（重命名、移动、删除）

### 6. FTP / WebDAV

内置 FTP 与 WebDAV 服务，支持标准客户端访问：

- 匿名访问公开共享目录，认证后还可访问私有目录
- FTP 支持 TLS/FTPS 加密传输（需配置证书）

连接示例：

```bash
# FTP 匿名登录
ftp localhost 2121

# FTPS 加密连接（需服务端启用 tls）
ftps://localhost:2121

# WebDAV 端点（需在配置中启用）
# http://localhost:8080/webdav/
```

详见 [FTP/WebDAV 使用指南](./docs/FTP_WEBDAV_GUIDE.md)。

### 7. 管理后台

管理员登录后进入管理后台，包括：

- 控制台看板、用户管理、文件管理
- 上传管理、重复文件识别、任务管理、操作记录
- 在线 IP、资源监控、行为分析
- OpenAPI / API Key、安全事件、系统设置

## API 接口

所有 API 通过 `/api/v1/` 前缀访问。OpenAPI 文档（scalar）启用后可在界面查看，或阅读 [API 接口文档](./docs/api-contracts.md)。

### 公开接口（无需认证）

| 接口 | 说明 |
|------|------|
| `POST /api/v1/auth/register` | 用户注册 |
| `POST /api/v1/auth/login` | 用户登录 |
| `GET /api/v1/files/list` | 目录列表 |
| `GET /api/v1/search` | 文件搜索 |
| `GET /api/v1/download/*path` | 文件下载 |
| `GET /api/v1/files/preview/*path` | 文件预览 |
| `GET /api/v1/temp/...` | 临时文件相关 |
| `GET /api/v1/health` | 健康检查 |

### 需认证接口

| 接口 | 说明 |
|------|------|
| `GET/POST /api/v1/private/...` | 私有文件列表 / 上传 |
| `POST /api/v1/files/rename` | 重命名 |
| `POST /api/v1/files/move` | 移动 |
| `/api/v1/cli/...` | CLI 命令 |
| `/api/v1/webdav/...` | WebDAV 访问 |

### 管理接口

| 接口 | 说明 |
|------|------|
| `/api/v1/admin/users` | 用户管理 |
| `/api/v1/admin/files` | 文件管理 |
| `/api/v1/admin/...` | 任务、上传、统计、安全、OpenAPI/API Key 等 |

> 详细且最新的端点清单请以 [API 接口文档](./docs/api-contracts.md) 为准，页面内 OpenAPI 文档为权威来源。

## 服务安装

将程序安装为系统服务，实现开机自启和后台运行：

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

```bash
# 安装服务
fuzhan --install
# 创建管理员
fuzhan --create-admin admin:password123
# 查看状态 / 卸载
fuzhan --status
fuzhan --uninstall
```

| 平台 | 实现方式 | 权限要求 |
|------|----------|----------|
| Windows | sc.exe | 管理员权限 |
| Linux | systemd | sudo |
| macOS | launchd | sudo（系统级）或用户目录 |

## 技术文档

| 文档 | 说明 |
|------|------|
| [文档索引](./docs/index.md) | 项目文档总览与快速参考 |
| [用户手册](./docs/user-guide.md) | 安装部署、功能使用、常见问题 |
| [开发指南](./docs/development-guide.md) | 环境配置、开发命令、构建部署、测试、代码规范 |
| [API 接口文档](./docs/api-contracts.md) | 所有 API 端点详解 |
| [架构文档](./docs/architecture.md) | 系统架构、技术栈、设计方案 |
| [数据模型](./docs/data-models.md) | 数据库模型定义 |
| [UI 组件清单](./docs/component-inventory.md) | Vue 组件、页面、路由、API 客户端 |
| [源码分析](./docs/source-tree-analysis.md) | 目录结构、代码组织 |
| [FTP/WebDAV 指南](./docs/FTP_WEBDAV_GUIDE.md) | FTP / WebDAV 配置和使用 |
| [系统配置与资源规划](./docs/系统配置与资源规划.md) | 不同规模下的资源要求 |

## 开源协议

本项目基于 [GPLv3](https://www.gnu.org/licenses/gpl-3.0.html) 许可证发布。