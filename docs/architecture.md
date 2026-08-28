# 架构文档

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-22

---

## 目录

- [项目概述](#项目概述)
- [技术栈](#技术栈)
- [架构模式](#架构模式)
- [目录结构](#目录结构)
- [核心模块](#核心模块)
- [数据流](#数据流)
- [安全机制](#安全机制)
- [部署架构](#部署架构)

---

## 项目概述

**浮栈 (Light Share)** 是一个轻量级文件分享工具，采用 Go + Vue 3 构建。

### 主要特性

- 跨平台支持 (Windows/macOS/Linux)
- 公共文件浏览、搜索、下载
- 临时文件分享（基于访问码，无需登录）
- 私有存储（需注册登录）
- 分片上传 + 断点续传（支持 xxh3 校验）
- URL 下载任务（支持断点续传）
- 内置 FTP/FTPS 服务器
- WebDAV 服务器（支持外部应用访问）
- 文件预览（文本分块/图片/PDF）
- 管理员后台（用户管理、文件管理、监控）
- JWT 认证 + 自动 token 刷新 + 用户禁用检查
- API Key 认证（外部集成）
- LDAP 认证（企业目录）
- RBAC 权限控制
- 数据迁移说明（SQLite ↔ MySQL ↔ PostgreSQL 使用外部工具如 dbswitch）
- OpenAPI 文档
- Prometheus 监控指标
- 系统监控（存储/访问/关键词/热门排名）
- 配置热重载
- 服务安装（systemd/sc/launchd）
- 最近操作记录与通知系统

---

## 技术栈

### 后端 (server/)

| 技术 | 版本 | 用途 |
|------|------|------|
| Go | 1.26.0 | 后端语言 |
| Gin | v1.11.0 | HTTP 框架 |
| GORM | v1.30.0 | ORM |
| SQLite/MySQL/PostgreSQL | - | 数据库 |
| JWT | v5.2.1 | 认证 |
| Zap | v1.27.0 | 日志 (lumberjack 轮转) |
| Prometheus | v1.19.1 | 监控 |
| ftpserverlib | v0.32.0 | FTP 服务器 |
| xxh3 (zeebo/xxh3) | v1.1.0 | 分片校验 |
| go-playground/validator | v10.27.0 | 参数验证 |
| bcrypt (golang.org/x/crypto) | - | 密码哈希 |

### 前端 (ui/)

| 技术 | 版本 | 用途 |
|------|------|------|
| Vue | 3.5.24 | 前端框架 |
| Vite | 7.2.4 | 构建工具 |
| Element Plus | 2.4.4 | UI 组件库 |
| Vue Router | 4.5.0 | 路由 (Hash 模式) |
| Axios | 1.17.0 | HTTP 客户端 |
| Less | 4.2.2 | CSS 预处理器 |
| hash-wasm | 4.12.0 | 分片 xxhash 校验 |

---

## 架构模式

采用 **分层架构**：

```
┌─────────────────────────────────────────┐
│           前端 (Vue 3 + Element Plus)      │
│         SPA (Hash 路由模式)              │
└─────────────────────────────────────────┘
                    │
                    ▼ HTTP/REST
┌─────────────────────────────────────────┐
│         后端 (Go + Gin)                  │
│  ┌─────────────────────────────────────┐│
│  │         Router & Handlers           ││
│  │  auth, file, temp, upload, admin,   ││
│  │  notify, monitor, preview, setup... ││
│  └─────────────────────────────────────┘│
│                    ▼                    │
│  ┌─────────────────────────────────────┐│
│  │         Services (业务逻辑)          ││
│  │  auth, file, search, cleanup,       ││
│  │  upload, admin, monitor  ││
│  └─────────────────────────────────────┘│
│         │                │              │
│         ▼                ▼              │
│  ┌───────────┐    ┌─────────────┐      │
│  │ GORM 模型  │    │ 文件系统 /  │      │
│  │ (数据库)   │    │ FTP/WebDAV │      │
│  └───────────┘    └─────────────┘      │
│         │                               │
│         ▼                               │
│  ┌─────────────────────────────────────┐│
│  │      SQLite / MySQL / PostgreSQL    ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
┌───────────────┐ ┌───────────┐ ┌───────────┐
│  文件系统存储  │ │  FTP 服务器│ │ WebDAV    │
│  (共享目录)    │ │ (port 2121)│ │ (port 0)  │
└───────────────┘ └───────────┘ └───────────┘
```

### 生命周期管理

后端使用 **context cancellation + hot-restart loop** 模式：
- 收到 SIGINT/SIGTERM 时触发优雅关闭
- HTTP 服务 Shutdown + worker goroutine 等待
- 关闭超时（默认 30s）强制退出
- 配置文件变更时自动热重启

---

## 目录结构

```
fuzhan/
├── server/                    # Go 后端
│   ├── cmd/fuzhan/       # 程序入口
│   │   └── main.go           # 入口（含 worker 生命周期 + 热重启）
│   ├── go.mod / go.sum       # 依赖
│   │
│   ├── cmd/xxh3sum/          # xxhash 命令行工具
│   │
│   ├── internal/             # 核心应用代码
│   │   ├── access/           # 访问协议
│   │   │   ├── ftp/          # FTP 虚拟文件系统
│   │   │   └── webdav/       # WebDAV 服务器
│   │   │
│   │   ├── admin/            # 管理员模块
│   │   │   ├── handler.go    # 管理员处理器
│   │   │   ├── service.go    # 管理服务
│   │   │   └── url_download_handler.go # URL 下载任务管理
│   │   │
│   │   ├── appconfig/        # 应用配置
│   │   │   ├── config.go     # 配置结构体
│   │   │   ├── database.go   # 数据库连接
│   │   │   ├── dto.go        # DTO 定义
│   │   │   ├── validator.go  # 配置验证
│   │   │   └── writer.go     # 配置写入
│   │   │
│   │   ├── auth/             # 认证模块
│   │   │   ├── handler.go    # 认证处理器
│   │   │   ├── api_key.go    # API Key 认证
│   │   │   ├── ldap.go       # LDAP 认证
│   │   │   └── rbac.go       # RBAC 权限控制
│   │   │
│   │   ├── cli/              # CLI 模块
│   │   │   ├── cli.go        # CLI 主逻辑
│   │   │   └── handler.go    # CLI 处理器
│   │   │
│   │   ├── config/           # 配置模块
│   │   │   ├── config.go     # Config 结构体
│   │   │   ├── store.go      # 配置存储
│   │   │   ├── handler.go    # 配置处理器
│   │   │   └── binding_example.go
│   │   │
│   │   ├── constants/        # 常量定义
│   │   │   └── context_keys.go # Gin Context Key
│   │   │
│   │   ├── file/             # 文件操作模块
│   │   │   ├── handler.go    # 文件处理器
│   │   │   ├── download.go   # 下载处理器
│   │   │   ├── search.go     # 搜索（SSE 流式）
│   │   │   ├── preview.go    # 文件预览
│   │   │   ├── private.go    # 私有存储
│   │   │   ├── upload.go     # 分片上传
│   │   │   ├── recent.go     # 最近上传
│   │   │   ├── base.go       # 基础处理器
│   │   │   ├── legacy.go     # 遗留代码
│   │   │   ├── storage.go    # 存储服务
│   │   │   └── temp_upload.go # 临时文件上传
│   │   │
│   │   ├── ftp/              # FTP 服务器模块
│   │   │
│   │   ├── health/           # 健康检查
│   │   │   └── handler.go
│   │   │
│   │   ├── index/            # 文件索引服务
│   │   │
│   │   ├── middleware/       # Gin 中间件
│   │   │   ├── auth.go       # JWT 认证 + 用户禁用检查
│   │   │   ├── audit.go      # 审计日志
│   │   │   ├── monitoring.go # Prometheus
│   │   │   ├── response_wrapper.go # 统一响应
│   │   │   ├── token_refresh.go # Token 自动刷新
│   │   │   └── security.go   # 安全头
│   │   │
│   │   ├── models/           # 数据模型
│   │   │   ├── user.go       # User
│   │   │   ├── temp_file.go  # TempFile
│   │   │   ├── upload.go     # UploadSession, UploadedChunk, UploadRecord
│   │   │   ├── file.go       # 文件相关
│   │   │   ├── url_download_task.go # URLDownloadTask
│   │   │   ├── api_key.go    # ApiKey
│   │   │   └── notification.go
│   │   │
│   │   ├── monitor/          # 系统监控
│   │   │   ├── handler.go    # 监控处理器
│   │   │   └── metrics.go    # Prometheus 指标
│   │   │
│   │   ├── notification/     # 通知系统
│   │   │   ├── handler.go    # 通知处理器
│   │   │   └── service.go    # 通知服务
│   │   │
│   │   ├── openapi/          # OpenAPI 支持
│   │   │   ├── spec.go       # OpenAPI 规范
│   │   │   ├── stats_handler.go # 统计处理器
│   │   │   └── middleware.go # OpenAPI 中间件
│   │   │
│   │   ├── repositories/     # 数据访问层
│   │   │   ├── upload_repo.go
│   │   │   └── url_download_task_repo.go
│   │   │
│   │   ├── resources/        # 嵌入资源
│   │   │   ├── embed.go      # 资源嵌入
│   │   │   ├── cli_help.txt
│   │   │   ├── systemd.service.txt
│   │   │   └── launchd.plist.txt
│   │   │
│   │   ├── router/           # 路由配置
│   │   │   └── router.go     # Gin 路由设置
│   │   │
│   │   ├── services/         # 业务服务层
│   │   │   ├── auth_service.go
│   │   │   ├── file_service.go
│   │   │   ├── search_service.go
│   │   │   ├── download_service.go
│   │   │   ├── admin_service.go
│   │   │   ├── temp_file_service.go
│   │   │   ├── upload_utils.go
│   │   │   ├── disk_space_*.go  # 平台磁盘空间
│   │   │   ├── prealloc_*.go    # 文件预分配
│   │   │   └── (services 其余包)
│   │   │
│   │   ├── setup/            # 初始化向导
│   │   │   └── handler.go
│   │   │
│   │   ├── temp/             # 临时文件
│   │   │   ├── model.go
│   │   │   └── service.go
│   │   │
│   │   └── utils/            # 工具函数
│   │       ├── logger.go     # Zap 日志
│   │       ├── path_utils.go
│   │       ├── file_utils.go
│   │       ├── url_validator.go
│   │       ├── http_utils.go
│   │       ├── metrics.go
│   │       ├── preview_utils.go
│   │       ├── asset_utils.go
│   │       ├── pathpolicy/   # 路径策略
│   │       └── service/      # 服务安装
│   │
│   ├── pkg/                  # 公共包
│   │   ├── jwt/              # JWT 工具
│   │   ├── pathutils/        # 路径工具
│   │   ├── response/         # 响应工具
│   │   └── xxh3/             # xxh3 实现
│   │
│   ├── integration/          # 集成测试
│   │
│   └── web/                  # 嵌入的前端资源
│       ├── index.html
│       └── assets/
│
└── ui/                       # Vue 3 前端
    ├── src/
    │   ├── main.js           # 入口
    │   ├── App.vue           # 根组件
    │   ├── api/index.js      # Axios + API 模块
    │   ├── assets/           # 静态资源
    │   ├── components/
    │   │   ├── common/       # 通用组件
    │   │   ├── file/         # 文件组件
    │   │   ├── upload/       # 上传组件
    │   │   ├── settings/     # 设置组件
    │   │   ├── setup/        # 初始化组件
    │   ├── views/            # 页面 (13+)
    │   ├── router/index.js   # 路由
    │   ├── store/index.js    # 状态管理
    │   ├── composables/      # 组合式函数
    │   ├── config/           # 配置
    │   ├── plugins/          # 插件
    │   ├── styles/           # 样式
    │   └── utils/            # 工具
    ├── public/               # 静态资源
    ├── package.json
    └── vite.config.js
```

---

## 核心模块

### 1. 认证模块

- **JWT Token**: HS256 算法，24小时有效期
- **API Key**: 外部集成认证，支持 IP 限制
- **LDAP**: 企业目录集成
- **自动刷新**: 距过期 < 30 分钟时自动刷新
- **密码**: bcrypt 哈希
- **RBAC**: 基于角色的访问控制（user/admin/api）

### 2. 分片上传模块

```
创建会话 → 上传分片 → 完成上传
   │           │           │
   ▼           ▼           ▼
sessionId   chunkIndex   merge files
           (断点续传)    (原子操作)
```

- 默认分片大小: 10MB
- xxh3 校验和验证
- 预分配文件空间（平台特定实现）
- 支持普通/temp/private 三种目标类型

### 3. URL 下载模块

- 异步下载，支持队列管理
- 支持 HTTP/FTP/FTPS 协议
- 断点续传（服务端检测）
- 通知推送（前端轮询）
- 管理员任务管理

### 4. 临时文件模块

- 基于访问码（8位）的无认证分享
- 支持过期时间和下载次数限制
- 基于客户端 IP 的配额控制
- SHA256 安全文件名
- 支持分片上传

### 5. 搜索模块

- 并发搜索多个根目录 (goroutine + channel)
- SSE 流式返回结果
- 文件名/扩展名匹配
- 关键词记录统计
- 文件索引加速（可选）

### 6. FTP 服务器

- 内置 FTP 服务器 (ftpserverlib)
- 默认端口 2121
- 可选 TLS 加密
- 匿名访问支持
- 运行时启停控制

### 7. WebDAV 服务器

- 标准 WebDAV 协议支持
- 公共和私有存储访问
- 可配置端口和凭证
- 与外部应用集成

### 8. 文件预览

- MIME 类型自动检测
- 文本文件分段加载（大文件优化）
- 图片内联预览
- PDF 内联预览
- 可配置允许的 MIME 和扩展名

### 9. 数据迁移说明

> 浮栈**不内置**跨数据库引擎的数据迁移/备份/恢复能力。若需在不同引擎之间迁移数据（如 SQLite → MySQL / PostgreSQL），请使用成熟的外部工具，例如 [dbswitch](https://github.com/light-art/dbswitch)。迁移完成后在配置中更新 `database.driver` 与 `database.dsn` 指向新库即可。

### 10. 系统监控

- 存储使用统计（全局 + 根目录级别）
- 访问统计（时间段过滤）
- 热门搜索关键词
- 热门下载排行
- 近期操作追踪
- Prometheus 指标导出

### 11. OpenAPI 支持

- 自动生成 OpenAPI 规范
- 可配置速率限制
- IP 访问控制
- 统计和监控

### 12. 清理服务

- 定时清理过期上传会话 (每5分钟)
- 自动清理残留分片文件
- 临时文件过期清理
- 私有存储过期文件清理 (每小时)

### 13. 配置热重载

- 文件系统监听配置变更
- 自动触发 worker 热重启
- 支持动态更新日志级别

### 14. 服务安装

- Windows: sc.exe
- Linux: systemd
- macOS: launchd

---

## 数据流

### 文件上传流程

```
用户选择文件
     │
     ▼
前端: 计算分片 + xxh3 校验
     │
     ▼
POST /uploads/session (创建会话)
     │
     ▼
循环上传分片:
  POST /uploads/chunk (含校验和)
     │
     ▼
POST /uploads/finalize (合并文件 + 原子重命名)
     │
     ▼
返回成功响应
```

### 搜索流程

```
用户输入关键词
     │
     ▼
GET /search/关键词 (SSE)
     │
     ▼
后端: 并发搜索各根目录
     │
 ├─→ /share1/ (goroutine)
 ├─→ /share2/ (goroutine)
 └─→ /share3/ (goroutine)
     │
     ▼
实时返回匹配结果 (channel)
     │
     ▼
前端: 流式渲染结果
```

### URL 下载流程

```
用户提交 URL
     │
     ▼
POST /uploads/url (创建下载任务)
     │
     ▼
后端异步下载:
 ├─→ HTTP/FTP 下载
 ├─→ 断点续传检测
 └─→ 完成通知
     │
     ▼
前端轮询通知:
 ├─→ 启动后 3s 首次检查
 └─→ 每 10s 后续轮询
```

### 数据迁移流程

> 浮栈不内置跨引擎迁移。如需在 SQLite ↔ MySQL ↔ PostgreSQL 之间迁移，推荐使用外部工具 [dbswitch](https://github.com/light-art/dbswitch) 导出并导入数据，然后在配置（`fuzhan.yaml`）中更新 `database.driver` 与 `database.dsn` 指向新库后重启服务。

---

## 安全机制

### 路径安全

- 所有文件操作使用 `filepath.Clean()` 规范化路径
- 使用 `IsPathOutOfScope()` 验证路径在允许范围内
- 防止 `../` 等路径遍历攻击
- URL Query 解码后检查路径遍历
- 隐藏绝对路径，仅返回相对路径

### 认证与授权

| 路由类型 | 认证方式 |
|----------|----------|
| 公开浏览 | 无 |
| 临时文件 | 基于 IP |
| 私有存储 | JWT / API Key |
| 管理功能 | JWT + Admin 角色 |
| CLI 接口 | JWT |
| WebDAV | 用户名/密码或 API Key |
| OpenAPI | API Key |

### 中间件链

```
Monitoring → SecurityHeaders → CORS → RequestID → Recovery
                                                        │
                                          AuthRequired/AuthOptional/RequireAdmin
                                                        │
                                               Token Refresh / API Key
                                                        │
                                              Response Wrapper
                                                        │
                                              Audit Log
```

### 安全头

- X-Frame-Options: DENY
- X-Content-Type-Options: nosniff
- Content-Security-Policy

### SSRF 防护

- URL 上传验证源 IP
- 支持 CIDR 网段白名单
- 禁用时仅允许公网地址

---

## 部署架构

```
                    ┌─────────────────┐
                    │   反向代理       │
                    │  (Nginx/Caddy)  │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
        ┌──────────┐  ┌──────────┐  ┌──────────┐
        │ Frontend │  │ Backend  │  │ Backend  │
        │  (静态)   │  │ Server 1 │  │ Server 2 │
        └──────────┘  └────┬─────┘  └────┬─────┘
                           │              │
                           └──────┬───────┘
                                  │
                           ┌──────▼──────┐
                           │  数据库     │
                           │ (SQLite/    │
                           │  MySQL/     │
                           │  PostgreSQL)│
                           └─────────────┘
                                  │
                           ┌──────▼──────┐
                           │  文件存储   │
                           │ (共享目录)  │
                           └─────────────┘
                                  │
              ┌───────────────────┼───────────────────┐
              │                   │                   │
       ┌──────▼──────┐     ┌──────▼──────┐     ┌──────▼──────┐
       │ FTP 服务器  │     │ WebDAV     │     │  CLI 接口   │
       │ (port 2121) │     │ (port 0)   │     │  (JWT)      │
       └─────────────┘     └────────────┘     └─────────────┘
```
