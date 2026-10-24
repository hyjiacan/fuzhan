# 项目文档索引

> 浮栈 (Light Share) 项目文档 | 更新时间: 2026-10-25

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

## 目录结构

```
docs/
├── index.md                     本文档（总索引）
├── 00-…-总结.md ~ 08-测试计划.md  功能模块规格说明
├── api/                         API 接口与契约
├── design/                      架构、数据模型、源码结构、组件清单
├── guides/                      用户手册、开发指南、协议指南、资源规划
├── proposals/                   设计与改造方案（含未来规划）
├── reports/                     历史审查报告与扫描产物（时间点快照）
└── superpowers/                 历史实现计划与设计草稿
```

---

## 功能模块规格

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
| [08-测试计划.md](./08-测试计划.md) | 测试范围、用例统计与运行方式 |

---

## 架构与设计（design/）

- [架构文档](./design/architecture.md) — 系统架构、技术栈、核心模块、部署方案
- [SYSTEM_DESIGN.md](./design/SYSTEM_DESIGN.md) — 系统设计说明
- [源码目录分析](./design/source-tree-analysis.md) — 目录结构详解、入口点、关键文件说明
- [数据模型文档](./design/data-models.md) — 数据库模型定义（含 URLDownloadTask）
- [UI 组件清单](./design/component-inventory.md) — Vue 组件、页面、路由、状态管理、API 客户端
- [临时文件管理](./design/TEMPORARY_FILE_MANAGEMENT.md) — 临时文件的生命周期与清理策略

## API 与数据（api/）

- [API 接口文档](./api/api-contracts.md) — 所有 API 端点详解（含通知、API Key 管理）
- [API_INTERFACE.md](./api/API_INTERFACE.md) — API 接口定义汇总

## 用户与开发指南（guides/）

- [用户手册](./guides/user-guide.md) — 安装部署、功能使用、常见问题
- [开发指南](./guides/development-guide.md) — 环境配置、开发命令、构建部署、测试
- [FTP/WebDAV 使用指南](./guides/FTP_WEBDAV_GUIDE.md) — FTP 和 WebDAV 配置和使用
- [系统配置与资源规划](./guides/系统配置与资源规划.md) — 不同功能、不同文件规模下的 CPU/内存/磁盘/数据库要求

## 设计方案（proposals/）

- [插件化扩展方案](./proposals/plugin-extension-plan.md) — 插件框架、生命周期与改造清单
- [URL 下载队列事件驱动改造方案](./proposals/URL下载队列事件驱动改造方案-2026-10-10.md) — 队列调度去轮询、按需定时唤醒与任务优先级设计
- [GitHub Release 二进制自动同步方案](./proposals/GitHub%20Release%20二进制自动同步方案.md) — 发布物同步与下载支持
- [WebDAV 检索支持](./proposals/webdav检索支持.md) — WebDAV 协议下的文件检索设计
- [文件名联想与纠错-双字段索引方案](./proposals/文件名联想与纠错-双字段索引方案.html) — 搜索联想与纠错索引设计
- [系统设置页优化方案](./proposals/系统设置页优化方案.html) — 设置页交互与信息架构
- [文件列表“最新版本”自动识别与高亮算法](./proposals/文件列表“最新版本”自动识别与高亮算法文档.md) — 版本识别与高亮算法

## 审查与扫描报告（reports/）

> 以下为时间点快照，结论仅对应当次审查时的代码状态。

| 报告 | 日期 | 说明 |
|------|------|------|
| [安全审查报告](./reports/安全审查报告-2026-09-09.md) | 2026-09-09 | 安全风险与加固建议 |
| [代码审查报告](./reports/代码审查报告-2026-10-09.md) | 2026-10-09 | 代码质量与可维护性 |
| [数据库交互审查报告](./reports/数据库交互审查报告-2026-09-29.md) | 2026-09-29 | 数据库访问与事务规范 |
| [系统配置使用点审计报告](./reports/系统配置使用点审计报告-2026-09-12.md) | 2026-09-12 | 配置项使用点审计 |
| [FTP 功能审查报告](./reports/FTP功能审查报告-2026-09-12.md) | 2026-09-12 | FTP 功能与安全审查 |
| [WebDAV 功能审查报告](./reports/WebDAV功能审查报告-2026-09-12.html) | 2026-09-12 | WebDAV 功能与安全审查 |
| [后端性能报告](./reports/backend-performance-report.html) | - | 性能基准与瓶颈分析 |
| [项目扫描报告](./reports/project-scan-report.json) | - | 项目结构扫描产物（机器可读） |

---

## 开始使用

### 1. 环境准备

参考 [开发指南 - 环境要求](./guides/development-guide.md#环境要求)

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
- 健康检查: `http://localhost:8080/api/v1/web/health`

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
| 远端存储 | S3 / MinIO / OSS / WebDAV 存储后端 |
| 行为分析 | 管理页按访问 / 上传 / 下载三种行为分行聚合，均支持按用户 / 按 IP 维度分析；下载行为含趋势（日/周/月）、时段热度、生命周期/衰减、热门文件（含扩散传播）、IP/用户来源分布、目录与类型聚合、失败/异常统计，支持近7/30天与自定义区间及单文件下钻 |

---

## 相关链接

- [UI README.md](../ui/README.md) — 前端说明
- [go.mod](../server/go.mod) — Go 依赖
- [package.json](../ui/package.json) — 前端依赖

---

## 文档维护

如需更新:

1. 修改源码实现
2. 重新运行文档生成，或手动编辑对应文档文件
3. 新增文档请按 `api/` / `design/` / `guides/` / `proposals/` / `reports/` 分类归档，并在本索引中登记
