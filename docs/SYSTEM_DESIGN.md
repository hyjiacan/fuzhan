# 轻共享 (fuzhan) 系统设计文档

> 更新时间: 2026-07-22

## 项目概述

轻共享 (fuzhan) 是一个轻量级的文件共享工具，采用 Go + Vue 3 构建。它提供了简单的文件共享功能，支持多种数据库和多种认证方式。应用程序通过网页浏览器界面提供文件服务，支持文件上传、下载、搜索、浏览和预览。

## 核心特性

1. **跨平台支持**：支持 Windows/macOS/Linux 操作系统
2. **文件操作**：文件上传、下载、搜索和浏览
3. **临时文件共享**：支持带过期时间的临时文件分享（基于IP，无需账号）
4. **私有存储**：用户注册后可使用私有文件存储功能
5. **命令行接口**：提供终端访问的 CLI 接口
6. **分片上传**：支持大文件的分片上传和断点续传
7. **文件预览**：在浏览器中预览文本、图片、PDF文件
8. **FTP 服务器**：内置 FTP 服务器支持
9. **WebDAV 服务器**：支持外部应用通过 WebDAV 协议访问
10. **多种认证**：JWT、API Key、LDAP
11. **RBAC 权限控制**：细粒度的权限管理
12. **数据库迁移**：支持 SQLite/MySQL/PostgreSQL 互迁
13. **基本安全机制**：包含路径遍历保护、文件类型过滤、多种认证

## 系统架构

### 整体架构

```
轻共享 (fuzhan)
├── server/                         # 后端 (Go + Gin 框架)
│   ├── cmd/fuzhan/            # 程序入口
│   │   └── main.go                # 主程序入口 + worker 生命周期
│   ├── cmd/xxh3sum/               # xxh3 命令行工具
│   │
│   ├── internal/                  # 核心应用代码（重构后）
│   │   ├── access/                # 访问协议
│   │   │   ├── ftp/               # FTP 虚拟文件系统
│   │   │   └── webdav/            # WebDAV 服务器
│   │   ├── admin/                 # 管理员功能
│   │   ├── auth/                  # 认证模块
│   │   │   ├── handler.go         # 认证处理器
│   │   │   ├── api_key.go         # API Key 认证
│   │   │   ├── ldap.go            # LDAP 认证
│   │   │   └── rbac.go            # RBAC 权限
│   │   ├── cli/                   # CLI 模块
│   │   ├── config/                # 配置管理
│   │   ├── file/                  # 文件操作
│   │   │   ├── handler.go         # 文件处理器
│   │   │   ├── download.go        # 下载
│   │   │   ├── search.go          # 搜索（SSE）
│   │   │   ├── preview.go         # 预览
│   │   │   └── upload.go          # 上传
│   │   ├── ftp/                   # FTP 服务器
│   │   ├── health/                # 健康检查
│   │   ├── index/                 # 文件索引
│   │   ├── middleware/            # 中间件
│   │   │   ├── auth.go            # JWT 认证
│   │   │   ├── monitoring.go      # 监控
│   │   │   ├── audit.go           # 审计
│   │   │   └── security.go        # 安全头
│   │   ├── models/                # 数据模型
│   │   ├── monitor/               # 系统监控
│   │   ├── notification/          # 通知系统
│   │   ├── openapi/               # OpenAPI 支持
│   │   ├── repositories/          # 数据访问层
│   │   ├── router/                # 路由配置
│   │   ├── services/              # 业务服务
│   │   │   ├── auth_service.go
│   │   │   ├── file_service.go
│   │   │   ├── search_service.go
│   │   │   └── migration/         # 数据库迁移
│   │   ├── setup/                 # 初始化向导
│   │   └── temp/                  # 临时文件
│   ├── pkg/                       # 公共包
│   │   ├── jwt/                   # JWT 工具
│   │   ├── pathutils/             # 路径工具
│   │   ├── response/              # 响应工具
│   │   └── xxh3/                  # xxh3 实现
│   └── integration/               # 集成测试
│
└── ui/                            # 前端 (Vue 3 + Naive UI)
    ├── src/
    │   ├── main.js               # 入口文件
    │   ├── App.vue               # 根组件
    │   ├── router/               # 路由配置
    │   ├── store/                # 状态管理
    │   ├── api/                  # API 接口
    │   ├── views/                # 页面组件
    │   ├── components/           # 公共组件
    │   ├── composables/          # 组合式函数
    │   ├── styles/               # 样式文件
    │   └── utils/                # 工具函数
    └── package.json              # 依赖配置
```

**注意**: `server/handlers/` 目录已重构为 `server/internal/`，按功能模块组织。

### 技术栈

| 层级 | 技术 | 说明 |
|------|------|------|
| 后端语言 | Go 1.26.0 | 跨平台编译 |
| 后端框架 | Gin v1.11.0 | 高性能 HTTP 框架 |
| 数据库 | SQLite/MySQL/PostgreSQL | 可选（通过 Gorm） |
| 认证 | JWT / API Key / LDAP | 多种认证方式 |
| 权限控制 | RBAC | 基于角色的访问控制 |
| 日志 | Zap + lumberjack | 结构化日志，支持轮转 |
| 前端框架 | Vue 3.5.24 | 组合式 API |
| UI 组件库 | Naive UI 2.41.0 | Vue 3 组件库 |
| 构建工具 | Vite 7.2.4 | 快速开发构建 |
| 实时通信 | SSE | Server-Sent Events |

### 模块详细说明

#### 1. 主程序入口 (server/cmd/fuzhan/main.go)

- 程序启动点，负责初始化应用程序和设置 HTTP 路由
- Worker 生命周期管理（context cancellation + hot-restart loop）
- 配置嵌入资源文件系统
- 初始化数据库（支持 SQLite/MySQL/PostgreSQL）
- 启动 FTP 服务器
- 定期清理过期临时文件的任务调度

#### 2. 配置管理 (server/internal/config/, server/internal/appconfig/)

- `config.go` - YAML 配置解析，环境变量支持
- 数据库连接配置
- 上传分片大小配置
- LDAP 服务器配置
- WebDAV 配置
- OpenAPI 配置
- 日志级别配置

#### 3. 处理器层 (server/internal/)

按功能模块组织的处理器：

| 模块 | 职责 |
|------|------|
| `auth/handler.go` | 用户注册、登录、令牌刷新 |
| `file/handler.go` | 目录列表、文件重命名、移动 |
| `file/download.go` | 文件下载 |
| `file/search.go` | 文件搜索（SSE流式） |
| `file/preview.go` | 文件预览 |
| `temp/` | 临时文件管理（基于IP） |
| `file/upload.go` | 分片上传会话管理 |
| `admin/` | 管理员功能 |
| `cli/` | CLI 接口 |
| `health/` | 健康检查 |
| `monitor/` | 系统监控 |
| `notification/` | 通知系统 |
| `setup/` | 初始化向导 |
| `openapi/` | OpenAPI 文档 |

#### 4. 中间件层 (server/internal/middleware/)

| 中间件 | 职责 |
|--------|------|
| `auth.go` | JWT 验证、令牌刷新、API Key、用户禁用检查 |
| `monitoring.go` | Prometheus 指标收集 |
| `security.go` | 安全响应头、CORS |
| `audit.go` | 审计日志 |
| `token_refresh.go` | Token 自动刷新 |

#### 5. 认证模块 (server/internal/auth/)

| 文件 | 职责 |
|------|------|
| `handler.go` | 用户认证处理器 |
| `api_key.go` | API Key 生成和验证 |
| `ldap.go` | LDAP 认证集成 |
| `rbac.go` | RBAC 权限控制 |

#### 6. 访问协议 (server/internal/access/)

| 协议 | 职责 |
|------|------|
| `ftp/` | FTP 虚拟文件系统 |
| `webdav/` | WebDAV 服务器 |

#### 7. 前端页面 (ui/src/views/)

| 页面 | 路由 | 说明 |
|------|------|------|
| HomeView | / | 文件浏览 |
| SetupView | /setup | 初始化配置向导 |
| PrivateStorageView | /private | 私有文件存储（需登录） |
| TempView | /temp | 临时文件管理（基于IP） |
| RecentView | /recent | 最近上传 |
| UserView | /user | 用户个人中心 |
| AdminDashboardView | /admin/dashboard | 管理后台看板 |
| AdminUsersView | /admin/users | 用户管理 |
| AdminFilesView | /admin/files | 文件管理 |
| AdminZombieView | /admin/zombie | 僵尸文件清理 |
| AdminURLTaskView | /admin/url-tasks | URL 下载任务管理 |
| SettingsView | /settings | 系统设置 |

## 安全特性

### 已实现的安全措施

1. **JWT 认证**：用户注册、登录、令牌自动刷新
2. **API Key 认证**：外部集成，支持 IP 限制
3. **LDAP 认证**：企业目录集成
4. **RBAC 权限**：基于角色的访问控制（user/admin/api）
5. **密码安全**：使用 bcrypt 哈希存储
6. **路径遍历保护**：`IsPathOutOfScope()` 函数验证路径
7. **文件类型过滤**：`IsFileAllowed()` 函数限制上传类型
8. **安全响应头**：X-Frame-Options、X-Content-Type-Options 等
9. **CORS 控制**：跨域资源共享配置
10. **速率限制**：可配置请求频率限制
11. **SSRF 防护**：URL 下载时验证源 IP

## 性能特性

1. **并发处理**：
   - 多目录并发文件搜索
   - 分片并发上传
   - Goroutine 并发处理

2. **流式传输**：
   - SSE 实时搜索结果推送
   - 流式文件下载
   - 分片上传

3. **资源管理**：
   - 定期清理过期文件
   - 日志轮转（lumberjack）
   - 嵌入式静态资源
   - 文件预分配（大文件上传优化）

## 前端设计系统

### 统一布局规范

所有页面视图统一使用以下布局规范：

| 属性 | 值 | 说明 |
|------|------|------|
| 最大宽度 | `1200px` | 页面内容最大宽度 |
| 内边距 | `24px` | 页面容器内边距 |
| 圆角 | `8px` | 内容区域圆角 |

所有视图组件使用 `max-width: 1200px; margin: 0 auto; padding: 24px`，页面进入时应用 `slideUp` 动画。

### 微交互规范

| 效果 | 说明 |
|------|------|
| 卡片悬停 | `translateY(-2px)` + 阴影 `0 8px 24px rgba(0,0,0,0.12)` |
| 按钮悬停 | `translateY(-1px)` + 橙色阴影 |
| 按钮点击 | `scale(0.97)` |
| 表格行悬停 | `translateX(4px)` + 背景高亮 |

详见 `ui/src/styles/variables.less`

## API 接口

核心端点：

| 端点 | 说明 |
|------|------|
| `/api/v1/auth/*` | 认证相关 |
| `/api/v1/files/*` | 文件浏览 |
| `/download/*path` | 文件下载 |
| `/api/v1/search/*query` | 文件搜索 |
| `/api/v1/uploads/*` | 分片上传 |
| `/api/v1/temp/*` | 临时文件 |
| `/api/v1/private/*` | 私有存储（需认证） |
| `/api/v1/admin/*` | 管理功能（需管理员） |
| `/api/v1/api-keys/*` | API Key 管理（需管理员） |
| `/webdav/*` | WebDAV 协议访问 |

详见 [API_INTERFACE.md](./API_INTERFACE.md) 和 [api-contracts.md](./api-contracts.md)

## 部署说明

### 编译构建

```bash
# Windows
.\build.ps1

# Linux/macOS
./build.sh
```

### 配置文件

首次运行自动生成 `fuzhan.yaml` 配置文件，包含：
- 服务器配置（端口、地址）
- 数据库配置（支持 SQLite/MySQL/PostgreSQL）
- 根目录配置（配额）
- 临时文件配置
- 私有存储配置
- LDAP 配置（可选）
- WebDAV 配置（可选）
- OpenAPI 配置（可选）
- 日志配置

### 系统要求

- **操作系统**：Windows/macOS/Linux
- **Go版本**：1.26.0+
- **网络**：HTTP/HTTPS