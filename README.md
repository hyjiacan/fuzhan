# 浮栈 (Fuzhan)

浮栈 (Fuzhan)｜一座文件客栈，纳四方文件，可暂歇，可长驻。

基于 Go 的轻量级文件共享工具，无需复杂数据库或服务器配置，直接启动即可通过浏览器访问。

## 仓库地址

- Gitee: https://gitee.com/hyjiacan/fuzhan
- GitHub: https://github.com/hyjiacan/fuzhan

## 功能特性

- 轻量级在线文件共享工具
- 支持文件上传、下载、搜索、浏览
- 多平台支持(Windows/macOS/Linux)
- 内置基础安全防护机制
- 临时文件分享功能（带过期时间，基于IP）
- 私有存储功能（需注册账号）
- 支持大文件分片上传和断点续传
- 命令行接口支持（需登录）
- 文件预览功能（文本、图片、PDF）
- 内置 FTP 服务器（支持 TLS/FTPS）
- 管理后台（用户管理、文件管理、系统设置）

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go + Gin 框架 |
| 数据库 | SQLite (Gorm) |
| 认证 | JWT (bcrypt 密码) |
| 前端 | Vue 3 + Naive UI + Vite |

## 文档导航

| 我想... | 查看此文档 |
|---------|-----------|
| **快速部署使用** | [用户手册](./docs/user-guide.md) |
| **配置和管理** | [用户手册 - 配置指南](./docs/user-guide.md#配置指南) |
| **二次开发** | [开发指南](./docs/development-guide.md) |
| **查看 API** | [API 接口文档](./docs/api-contracts.md) |
| **了解架构** | [架构文档](./docs/architecture.md) |

![列出目录](/docs/1.png)
![搜索文件](/docs/2.png)

## 快速开始

### 1. 下载安装

从 [Releases](https://gitee.com/hyjiacan/fuzhan/releases) 下载对应平台的二进制文件

### 2. 首次运行

首次运行会启动配置向导，设置：
- 共享根目录
- 服务器端口
- 管理员账号

自动生成 `fuzhan.yaml` 配置文件。

### 3. 配置文件说明

```yaml
app:
  name: 浮栈
server:
  host: 0.0.0.0
  port: 80
database:
  driver: sqlite
  dsn: ./fuzhan.db
root_dirs:
  - path: D:\_apps
    quota: 20G
temp_files:
  enabled: true
  path: D:\_temp
  quota:
    global: 10G
    per_ip: 1G
private_files:
  enabled: true
  path: D:\_private
upload:
  chunk_size: 10485760  # 10MB
ftp:
  enabled: true
  host: 0.0.0.0
  port: 2121
  tls: false
  cert: ""  # TLS 证书文件路径
  key: ""   # TLS 密钥文件路径
```

### 4. 开发环境

#### 构建脚本

```powershell
# Windows
.\build.ps1                      # 构建前端和后端（使用当前日期作为版本）
.\build.ps1 -v 2025.12.10        # 指定版本号
.\build.ps1 ui                   # 仅构建前端
.\build.ps1 server               # 仅构建后端
```

```bash
# Linux/macOS/WSL
./build.sh                       # 构建前端和后端（使用当前日期作为版本）
./build.sh -v 2025.12.10         # 指定版本号
./build.sh ui                    # 仅构建前端
./build.sh server                # 仅构建后端
```

**输出文件名格式:** `fuzhan-{YYYY.MM.DD}-{os}-{arch}`

示例:
- `fuzhan-2026.06.16-windows-amd64.exe`
- `fuzhan-2026.06.16-linux-amd64`
- `fuzhan-2026.06.16-darwin-arm64`

**跨平台编译说明:**
- Windows 只能编译 Windows 版本
- 编译 Linux/macOS 版本需要在 WSL2 或 Linux 环境中
- 输出目录: `bin/`

#### 单独开发

后端开发：
```bash
cd server
go run main.go
# 或使用 Air 热重载: air
```

前端开发：
```bash
cd ui
yarn dev
```

## 使用指南

### 1. 文件浏览

- 主页显示所有根目录，点击进入子目录
- 支持文件预览（文本、图片、PDF）

### 2. 文件上传

- 通过上传按钮上传文件
- 支持本地文件上传和从URL上传
- 大文件自动使用分片上传
- 断点续传支持

### 3. 文件搜索

- 使用顶部搜索框搜索文件
- 支持按文件名、扩展名搜索

### 4. 临时文件

无需登录，通过IP识别用户：
- 上传文件生成访问码
- 可设置过期时间
- 分享链接给他人

### 5. 私有存储

需要注册账号登录使用：
- 个人文件存储空间
- 私密分享（生成分享码）
- 文件管理（重命名、移动）

### 6. FTP 服务器

内置 FTP 服务器，支持以下特性：
- 匿名访问共享文件
- 绑定地址和端口配置
- TLS/FTPS 加密传输（需配置证书）
- 自动访问所有配置的共享目录

连接示例：
```bash
# 匿名登录
ftp localhost 2121

# FTPS 加密连接
ftps://localhost:2121
```

### 7. 管理后台

管理员可访问：
- 用户管理
- 文件管理
- 僵尸文件清理
- 系统设置

## API 接口

所有 API 通过 `/api/v1/` 前缀访问。

### 公开接口（无需认证）

| 接口 | 说明 |
|------|------|
| `POST /api/v1/auth/register` | 用户注册 |
| `POST /api/v1/auth/login` | 用户登录 |
| `GET /api/v1/files/list` | 目录列表 |
| `GET /api/v1/search/*query` | 文件搜索 |
| `GET /api/v1/download/*path` | 文件下载 |
| `GET /api/v1/files/preview/*path` | 文件预览 |
| `GET /api/v1/temp/*` | 临时文件 |
| `GET /api/v1/health` | 健康检查 |

### 需认证接口

| 接口 | 说明 |
|------|------|
| `GET /api/v1/private/files` | 私有文件 |
| `POST /api/v1/private/upload` | 上传文件 |
| `POST /api/v1/files/rename` | 重命名 |
| `POST /api/v1/files/move` | 移动 |
| `GET /api/v1/cli/*` / `GET /cli/*` | CLI 命令 |

### 管理接口

| 接口 | 说明 |
|------|------|
| `GET /api/v1/admin/users` | 用户列表 |
| `PUT /api/v1/admin/users/:uuid/disabled` | 禁用/启用用户 |
| `POST /api/v1/admin/upload-cert` | 上传 TLS 证书 |
| `POST /api/v1/admin/upload-key` | 上传 TLS 密钥 |

详细 API 文档请参考 [API_INTERFACE.md](docs/API_INTERFACE.md)

## 进阶部署

### 系统服务安装

#### Windows 服务

使用 [NSSM](https://nssm.cc/)：

```bash
nssm.exe install Fuzhan d:\path\to\fuzhan.exe
```

#### Linux 服务 (Systemd)

```ini
[Unit]
Description=Fuzhan File Sharing Service
After=network.target

[Service]
ExecStart=/usr/local/bin/fuzhan
WorkingDirectory=/var/lib/fuzhan
User=fuzhan
Restart=always

[Install]
WantedBy=multi-user.target
```

## 技术文档

| 文档 | 说明 |
|------|------|
| [用户手册](./docs/user-guide.md) | 安装部署、功能使用、常见问题 |
| [开发指南](./docs/development-guide.md) | 环境配置、开发命令、构建部署 |
| [API 接口文档](./docs/api-contracts.md) | 所有 API 端点详解 |
| [架构文档](./docs/architecture.md) | 系统架构、技术栈、设计方案 |
| [源码分析](./docs/source-tree-analysis.md) | 目录结构、代码组织 |

## 开源协议

本项目基于 [GPLv3](https://www.gnu.org/licenses/gpl-3.0.html) 许可证发布。