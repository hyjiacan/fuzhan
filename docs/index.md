# 项目文档索引

> 浮栈 (Light Share) 项目文档 | 更新时间: 2026-10-26

---

## 项目概述

**浮栈** 是一个轻量级文件分享工具，采用 Go + Vue 3 构建。

| 属性 | 值 |
|------|-----|
| **类型** | monorepo (前后端分离) |
| **主语言** | Go + JavaScript |
| **架构** | 分层架构 (Handler → Service → Model) |
| **仓库** | 2 个部分 (server/backend, ui/web) |

---

## 文档结构

```
docs/
├── index.md          本文档（总索引）
├── requirements/     需求规格（功能模块规格、测试计划）
├── design/           设计文档（架构、数据模型、API、方案）
│   ├── api/          API 接口与契约
│   └── proposals/    设计与改造方案
├── user/             用户文档（用户手册、协议指南、资源规划）
└── development/      开发文档（环境配置、构建、测试、规范）
```

---

## 需求规格（requirements/）

| 文档 | 说明 |
|------|------|
| [00-功能需求规格说明书-总结](./requirements/00-功能需求规格说明书-总结.md) | 功能需求总览 |
| [01-认证与授权模块](./requirements/01-认证与授权模块.md) | 认证授权（JWT/API Key/LDAP/RBAC） |
| [02-文件管理模块](./requirements/02-文件管理模块.md) | 文件浏览/操作 |
| [03-文件搜索模块](./requirements/03-文件搜索模块.md) | 搜索功能 |
| [04-文件上传模块](./requirements/04-文件上传模块.md) | 上传功能 |
| [05-临时文件分享模块](./requirements/05-临时文件分享模块.md) | 临时分享 |
| [06-命令行接口模块](./requirements/06-命令行接口模块.md) | CLI 接口 |
| [07-系统配置与选项模块](./requirements/07-系统配置与选项模块.md) | 配置管理 |
| [08-测试计划](./requirements/08-测试计划.md) | 测试范围、用例统计与运行方式 |

---

## 设计文档（design/）

### 架构与实现

- [架构文档](./design/architecture.md) — 系统架构、技术栈、核心模块、部署方案
- [SYSTEM_DESIGN.md](./design/SYSTEM_DESIGN.md) — 系统设计说明
- [源码目录分析](./design/source-tree-analysis.md) — 目录结构详解、入口点、关键文件说明
- [数据模型文档](./design/data-models.md) — 数据库模型定义（含 URLDownloadTask）
- [UI 组件清单](./design/component-inventory.md) — Vue 组件、页面、路由、状态管理、API 客户端
- [临时文件管理](./design/TEMPORARY_FILE_MANAGEMENT.md) — 临时文件的生命周期与清理策略

### API 与数据（design/api/）

- [API 接口文档](./design/api/api-contracts.md) — 所有 API 端点详解（含通知、API Key 管理）
- [API_INTERFACE.md](./design/api/API_INTERFACE.md) — API 接口定义汇总

### 设计方案（design/proposals/）

- [插件化扩展方案](./design/proposals/plugin-extension-plan.md) — 插件框架、生命周期与改造清单
- [URL 下载队列事件驱动改造方案](./design/proposals/URL下载队列事件驱动改造方案-2026-10-10.md) — 队列调度去轮询、按需定时唤醒与任务优先级设计
- [GitHub Release 二进制自动同步方案](./design/proposals/GitHub%20Release%20二进制自动同步方案.md) — 发布物同步与下载支持
- [WebDAV 检索支持](./design/proposals/webdav检索支持.md) — WebDAV 协议下的文件检索设计
- [文件名联想与纠错-双字段索引方案](./design/proposals/文件名联想与纠错-双字段索引方案.html) — 搜索联想与纠错索引设计
- [系统设置页优化方案](./design/proposals/系统设置页优化方案.html) — 设置页交互与信息架构
- [文件列表“最新版本”自动识别与高亮算法](./design/proposals/文件列表“最新版本”自动识别与高亮算法文档.md) — 版本识别与高亮算法

---

## 用户文档（user/）

- [用户手册](./user/user-guide.md) — 安装部署、配置、功能使用、常见问题
- [FTP/WebDAV 使用指南](./user/FTP_WEBDAV_GUIDE.md) — FTP 和 WebDAV 配置与客户端连接
- [系统配置与资源规划](./user/系统配置与资源规划.md) — 不同功能、不同文件规模下的 CPU/内存/磁盘/数据库要求

---

## 开发文档（development/）

- [开发指南](./development/development-guide.md) — 环境配置、开发命令、构建部署、测试、代码规范

---

## 快速参考

### 技术栈

| 部分 | 技术 | 版本 |
|------|------|------|
| 后端 | Go | 1.26.0 |
| 后端框架 | Gin | v1.11.0 |
| ORM | GORM | v1.30.0 |
| 数据库驱动 | SQLite/MySQL/PostgreSQL | - |
| 认证 | JWT (HS256) / API Key / LDAP | v5.2.1 |
| 监控 | Prometheus | v1.19.1 |
| 日志 | Zap | v1.27.0 |
| FTP服务器 | ftpserverlib | v0.32.0 |
| 前端 | Vue | 3.5.24 |
| 构建工具 | Vite | 7.2.4 |
| UI 库 | Element Plus | 2.4.4 |
| 路由 | Vue Router | 4.5.0 |
| HTTP客户端 | Axios | 1.17.0 |
| CSS预处理器 | Less | 4.2.2 |
| 分片校验 | hash-wasm | 4.12.0 |

### 入口点

- **后端**: `server/main.go`（命令行入口 + 服务生命周期 + 热重启）
- **前端**: `ui/src/main.js`（配置加载 + 通知轮询 + token 验证）
- **配置**: `fuzhan.yaml`

### 本地开发

```bash
# 后端
cd server && go run .
# 或使用 Air: cd server && air

# 前端
cd ui && npm run dev
```

访问：前端 `http://localhost:5173`，后端 `http://localhost:8080`。
环境要求与构建部署详见[开发指南](./development/development-guide.md)。

---

## 相关链接

- [UI README.md](../ui/README.md) — 前端说明
- [go.mod](../server/go.mod) — Go 依赖
- [package.json](../ui/package.json) — 前端依赖

---

## 文档维护

1. 修改源码实现。
2. 同步更新对应的需求/设计/用户/开发文档。
3. 新增文档按 `requirements/`、`design/`、`user/`、`development/` 分类归档，并在本索引中登记。
