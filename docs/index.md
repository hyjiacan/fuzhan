# 项目文档索引

> 浮栈 (Light Share) 项目文档 | 更新时间: 2026-07-22

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

- **后端**: `server/cmd/fuzhan/main.go` (worker 生命周期 + 热重启)
- **前端**: `ui/src/main.js` (配置加载 + 通知轮询 + token 验证)
- **配置**: `fuzhan.yaml`

---

## 生成文档

### 架构与设计

- [架构文档](./architecture.md) — 系统架构、技术栈、核心模块、部署方案
- [源码目录分析](./source-tree-analysis.md) — 目录结构详解、入口点、关键文件说明
- [数据模型文档](./data-models.md) — 数据库模型定义（含 URLDownloadTask）

### API 与数据

- [API 接口文档](./api-contracts.md) — 所有 API 端点详解（含通知、API Key 管理）

### 前端

- [UI 组件清单](./component-inventory.md) — Vue 组件、页面、路由、状态管理、API 客户端

### 用户文档

- [用户手册](./user-guide.md) — 安装部署、功能使用、常见问题
- [开发指南](./development-guide.md) — 环境配置、开发命令、构建部署、测试
- [FTP/WebDAV 使用指南](./FTP_WEBDAV_GUIDE.md) — FTP 和 WebDAV 配置和使用
- [系统配置与资源规划](./系统配置与资源规划.md) — 不同功能、不同文件规模下的 CPU/内存/磁盘/数据库要求

---

## 现有文档 (功能模块)

| 文档 | 说明 |
|------|------|
| [00-功能需求规格说明书-总结.md](./00-功能需求规格说明书-总结.md) | 功能需求总览 |
| [01-认证与授权模块.md](./01-认证与授权模块.md) | 认证授权（JWT/API Key/LDAP/RBAC） |
| [02-文件管理模块.md](./02-文件管理模块.md) | 文件浏览/操作 |
| [03-文件搜索模块.md](./03-文件搜索模块.md) | 搜索功能 |
| [04-文件上传模块.md](./04-文件上传模块.md) | 上传功能 |
| [05-临时文件分享模块.md](./05-临时文件分享模块.md) | 临时分享 |
| [06-命令行接口模块.md](./06-命令行接口模块.md) | CLI 接口 |
| [07-系统配置与选项模块.md](./07-系统配置与选项模块.md) | 配置管理 |
| [08-测试计划.md](./08-测试计划.md) | 测试计划 |
| [API_INTERFACE.md](./API_INTERFACE.md) | API 文档 |
| [SYSTEM_DESIGN.md](./SYSTEM_DESIGN.md) | 系统设计 |
| [TEMPORARY_FILE_MANAGEMENT.md](./TEMPORARY_FILE_MANAGEMENT.md) | 临时文件管理 |

---

## BMad 流程文档

BMad 相关文档位于 `_bmad-output/` 目录:

| 类型 | 路径 |
|------|------|
| 产品需求 | `_bmad-output/planning-artifacts/prds/` |
| 架构文档 | `_bmad-output/planning-artifacts/architecture/` |
| UX 设计 | `_bmad-output/planning-artifacts/ux-designs/` |
| 故事卡 | `_bmad-output/implementation-artifacts/stories/` |
| 项目上下文 | `_bmad-output/project-context.md` |

---

## 开始使用

### 1. 环境准备

参考 [开发指南 - 环境要求](./development-guide.md#环境要求)

### 2. 本地开发

```bash
# 后端
cd server/cmd/fuzhan && go run main.go
# 或使用 Air: cd server && air

# 前端
cd ui && yarn dev
```

### 3. 访问应用

- 前端: `http://localhost:5173`
- 后端: `http://localhost:8080`
- 健康检查: `http://localhost:8080/api/v1/health`

### 4. 配置

首次运行会显示配置页面，或手动编辑 `fuzhan.yaml`

---

## 新增功能 (v3.0)

| 功能 | 说明 |
|------|------|
| WebDAV 服务器 | 支持外部应用通过 WebDAV 协议访问文件 |
| API Key 认证 | 外部集成认证，支持 IP 限制 |
| LDAP 认证 | 企业目录集成 |
| RBAC 权限控制 | 基于角色的细粒度权限管理 |
| OpenAPI 支持 | 自动生成 API 文档 |
| 文件索引 | 搜索加速服务 |

---

## 相关链接

- [CLAUDE.md](../CLAUDE.md) — AI 开发指南
- [UI README.md](../ui/README.md) — 前端说明
- [go.mod](../server/go.mod) — Go 依赖
- [package.json](../ui/package.json) — 前端依赖

---

## 文档更新

本文档由项目扫描自动生成。

如需更新:
1. 修改源码实现
2. 重新运行文档生成
3. 或手动编辑对应文档文件
