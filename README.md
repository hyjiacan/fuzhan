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

- 存储后端：本地磁盘 / S3 / MinIO / 阿里云 OSS / WebDAV
- 临时分享：无需登录，按 IP 识别用户，支持过期时间、下载后删除、允许重复下载
- 私有存储：需注册登录，个人独立配额空间，私密分享
- 最近上传 / 热门文件榜单

### 认证与安全

- JWT 认证（bcrypt 密码），支持用户注册、登录、重置密码
- API Key 认证：外部集成接入，支持 IP / 域名白名单限制
- LDAP / OAuth2 (OIDC) 认证：企业目录与统一认证集成
- RBAC 角色权限：admin / user 等细粒度权限管理
- 文件类型白名单控制、僵尸上传清理、安全事件审计（可查看 IP 访问控制与风险记录）

### 协议与集成

- FTP 服务器：匿名/认证访问，可选 TLS/FTPS 加密
- WebDAV 服务器：支持外部应用（如系统文件管理器）挂载访问与检索
- OpenAPI：scalar 在线 API 文档，支持 API Key 鉴权调用与限流
- Webhook：文件事件推送到外部系统，支持 HMAC 签名、重试与消息队列
- CLI 接口：通过 HTTP 或命令行搜索、浏览、上传（需登录）

### 管理与可观测

- 管理后台：控制台看板、用户管理、文件管理、上传任务、系统作业、重复文件识别、操作记录
- 在线日志查看、IP 统计、资源监控（CPU/内存/磁盘/网络）、行为分析（趋势、时段热度、热门文件、来源分布、失败统计）
- 通知系统、系统设置（WEB/FTP/FTPS/WebDAV/TLS 证书、存储、预览、数据库、访问控制）
- 配置热重载：修改 `fuzhan.yaml` 自动检测并热重启（存储后端、端口等底层配置需重启生效）

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.26 + Gin v1.11 |
| 数据库 | SQLite（内置）/ MySQL / PostgreSQL（GORM v1.30） |
| 索引 | Bluge + gse 中文分词 |
| 认证 | JWT (HMAC) / API Key / LDAP / OAuth2 / RBAC |
| 监控 | Prometheus 指标 |
| 日志 | Zap + Lumberjack |
| FTP | ftpserverlib |
| 前端 | Vue 3 + Element Plus + Vite |
| 前端工具 | Vue Router / Axios / ECharts / hash-wasm / Less |

## 项目结构

```
fuzhan/
├── server/           # Go 后端（入口、领域包、嵌入式前端资源）
├── ui/               # Vue 3 前端
├── docs/             # 项目文档
└── README.md
```

## 文档导航

| 我想... | 查看此文档 |
|---------|-----------|
| **部署、配置与使用** | [用户手册](./docs/user/user-guide.md) |
| **FTP / WebDAV** | [FTP/WebDAV 使用指南](./docs/user/FTP_WEBDAV_GUIDE.md) |
| **评估服务器资源** | [系统配置与资源规划](./docs/user/系统配置与资源规划.md) |
| **二次开发** | [开发指南](./docs/development/development-guide.md) |
| **查看 API** | [API 接口文档](./docs/design/api/api-contracts.md) |
| **了解架构** | [架构文档](./docs/design/architecture.md) |
| **源码结构** | [源码目录分析](./docs/design/source-tree-analysis.md) |
| **全部文档** | [文档索引](./docs/index.md) |

## 快速开始

从 [Releases](https://gitee.com/hyjiacan/fuzhan/releases) 下载对应平台的二进制文件并启动：

```bash
# Linux / macOS
chmod +x ./fuzhan
./fuzhan
```

```powershell
# Windows
.\fuzhan.exe
```

启动后访问 `http://localhost:8080`，按页面提示完成初始化配置即可使用。
完整安装、配置、功能使用与常见问题见[用户手册](./docs/user/user-guide.md)。

## 开发

```bash
# 后端
cd server && go run .

# 前端
cd ui && yarn install && yarn dev
```

环境要求、构建打包、测试与代码规范见[开发指南](./docs/development/development-guide.md)。

## 开源协议

本项目基于 [GPLv3](https://www.gnu.org/licenses/gpl-3.0.html) 许可证发布。
