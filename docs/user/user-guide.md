# 用户手册

> 浮栈 (Light Share) 用户指南 | 更新时间: 2026-10-26

---

## 目录

- [快速开始](#快速开始)
- [配置指南](#配置指南)
- [功能使用](#功能使用)
- [FTP 访问](#ftp-访问)
- [API 访问](#api-访问)
- [常见问题](#常见问题)

---

## 快速开始

### 下载安装

1. 从 releases 下载对应平台的二进制文件：
   - Windows: `fuzhan.exe`
   - Linux: `fuzhan`
   - macOS: `fuzhan`

2. 将文件保存到任意目录

### 启动服务

**Windows:**
```powershell
.\fuzhan.exe
```

**Linux/macOS:**
```bash
chmod +x ./fuzhan
./fuzhan
```

### 首次配置

首次启动后，打开浏览器访问 `http://localhost:8080`，根据页面提示完成初始配置：

- 设置服务端口（默认 8080）
- 添加共享目录（可设置多个，每个可有独立配额）
- 配置临时文件存储（可选）
- 设置管理员账户

### 访问应用

配置完成后，访问 `http://localhost:8080` 即可使用。

### 安装为系统服务

**Windows:**

```powershell
# 安装为开机自启服务
.\fuzhan.exe --install

# 自定义服务名称与描述
.\fuzhan.exe --install --name "Fuzhan" --display "浮栈文件服务" --description "轻量级文件分享服务"

# 卸载服务
.\fuzhan.exe --uninstall
```

**Linux (systemd):**

```bash
sudo ./fuzhan --install
sudo ./fuzhan --uninstall
```

> 安装为服务后，请确认工作目录与 `fuzhan.yaml`、`data/`、`logs/` 的相对位置正确；
> 若需调整，请修改服务定义中的工作目录。

---

## 配置指南

配置文件为 `fuzhan.yaml`，位于程序同目录下。

### 服务配置

```yaml
server:
  host: "0.0.0.0"      # 监听地址，0.0.0.0 表示所有网卡
  http:
    enabled: true      # 是否启用 HTTP
    port: 8080         # HTTP 端口
  https:
    enabled: false     # 是否启用 HTTPS
    port: 8443         # HTTPS 端口
  ftp:
    enabled: false     # 是否启用 FTP
    port: 2121         # FTP 端口
    passive_port_start: 2122   # 被动模式数据端口范围
    passive_port_end: 2221
  ftps:
    enabled: false     # 是否启用 FTPS
    port: 990
  webdav:
    enabled: false     # 是否启用 WebDAV（复用 HTTP/HTTPS 端口）
  tls:
    cert_file: ""      # TLS 证书（HTTPS/FTPS 共用）
    key_file: ""
```

### 数据库

```yaml
database:
  driver: "sqlite"     # sqlite / mysql / postgres
  dsn: "./fuzhan.db"   # SQLite 路径，或 MySQL/PostgreSQL 连接串
```

> 切换数据库类型后需重启服务生效，已有数据需通过外部工具自行迁移。

### 共享目录

```yaml
storage:
  public:
    root_dirs:
      - path: "./data/share1"
        quota: "10GB"   # 配额限制，如 "10GB"、"500MB"
        allow_upload: true   # 是否允许向该目录上传（默认 true）
      - path: "./data/photos"
        quota: "50GB"
        allow_upload: false  # 设为 false 后该目录仅可浏览/下载，无法上传
```

> `allow_upload` 可单独控制每个共享目录是否允许上传（默认允许）。设为 `false` 后：
> - 上传界面中该目录不可选择；
> - Web 上传、FTP、WebDAV、URL 上传的上传请求都会被后端拒绝；
> - 将文件**移动/复制**进该目录的操作也会被拒绝（旧版移动接口、文件管理移动、FTP 重命名、WebDAV 写入均受限）。<br>
> 不影响该目录的浏览与下载。

### 临时文件分享

```yaml
storage:
  temp:
    enabled: true
    path: "./data/temp"
    quota:
      global: "10G"      # 全局总配额
      per_ip: "100MB"    # 单IP存储配额
    default_expire_days: 7
    delete_on_download: false
```

### 私有存储

```yaml
storage:
  private:
    enabled: true
    path: "./data/private"
    quota:
      global: "100G"     # 全局总配额
      per_user: "1GB"    # 单用户存储空间
    default_expire_days: 30
```

### 存储后端

存储后端决定公开、私有与临时文件的实际存放位置，支持：

| 取值 | 说明 |
|------|------|
| `local` | 本地磁盘（默认） |
| `s3` | AWS S3 或 S3 兼容对象存储 |
| `minio` | MinIO（S3 兼容协议） |
| `oss` | 阿里云 OSS |
| `webdav` | 远端 WebDAV 服务 |

```yaml
storage:
  backend: "local"     # local / s3 / minio / oss / webdav
  s3:                  # backend=s3/minio 时使用
    endpoint: "https://s3.amazonaws.com"
    access_key: "..."
    secret_key: "..."
    bucket: "..."
    region: "us-east-1"
    use_ssl: true
  oss:                 # backend=oss 时使用
    endpoint: "oss-cn-hangzhou.aliyuncs.com"
    access_key: "..."
    secret_key: "..."
    bucket: "..."
  webdav:              # backend=webdav 时使用
    url: "https://example.com/dav"
    username: "..."
    password: "..."
```

> **修改存储后端后必须重启服务才能生效**（服务启动时才会初始化存储连接）。
> 选择远端后端时，`storage.public.root_dirs[].path` 作为对象存储中的虚拟前缀，不再对应本地目录。

#### 切换存储后端与文件迁移

**切换存储后端不会自动迁移已有文件。** 切换后数据库记录仍在，但新后端是空的，历史文件会无法访问。

数据库中的文件路径是**逻辑路径**，不是磁盘绝对路径：

- `file_records_*` 表只保存 `file_path`（根目录内相对路径）、`root_name` 和 `full_path`（`/root_name/file_path`）；
- 共享目录的真实磁盘路径只保存在 `fuzhan.yaml` 的 `storage.public.root_dirs[].path` 中；
- 私有/临时文件的元数据在数据库中，文件本体按相对路径存放在各自存储目录下。

因此切换后端时**不需要修改数据库中的路径**，只要保证新后端中的对象 key 与原有相对路径一致即可。

推荐步骤：

1. 停服，备份 `fuzhan.yaml`、数据库以及原存储目录。
2. 按相同相对路径把文件同步到新后端。例如从本地迁移到 S3：
   ```bash
   # 共享目录 data/share1 -> bucket 的 share1/ 前缀
   rclone copy ./data/share1 s3:bucket/share1
   # 私有文件与临时文件同理，按配置的 path 作为前缀复制
   rclone copy ./data/private s3:bucket/private
   rclone copy ./data/temp s3:bucket/temp
   ```
3. 修改 `storage.backend` 与对应连接参数。
4. 启动服务并抽查文件浏览、下载、预览、分享链接是否正常。
5. 验证通过前不要删除旧后端数据；如需回滚，把 `backend` 改回原值并重启即可。

**例外说明：**

- URL 下载任务的 `temp_file_path` 保存的是本地临时下载文件的绝对路径，仅用于下载中转，任务完成/取消时会清理，不参与长期存储；跨机器恢复时这些未完成任务可能失效，建议先取消或等待其结束。
- 远端后端下，资源监控页的“磁盘总量/已用空间”仍基于本地目录统计，对象存储的实际用量请以云厂商控制台为准。
- 临时文件可依靠过期清理自然淘汰，通常无需迁移。

### 上传与预览

```yaml
upload:
  chunk_size: 10485760         # 分片大小（字节），默认 10MB
  max_file_size: 17179869184   # 最大文件大小（字节），0 表示不限制
  url_upload:
    enabled: false             # 是否允许从 URL 下载文件到服务器
    allowed_ip_ranges: []      # URL 源地址白名单（留空时拒绝内网/回环/保留地址）
    max_concurrent: 2          # 全局同时进行的 URL 上传任务数
    max_retries: 3             # 失败重试次数（4xx 不重试）
  rate_limit:
    window_minutes: 1          # 上传频率统计窗口（分钟）
    max_requests: 0            # 窗口内单 IP 最大上传操作数，0 表示不限制

preview:
  enabled: true                # 预览全局开关
  groups:                      # 各分组独立开关与 MIME/扩展名白名单
    image: { enabled: true, mimes: "image/png,image/jpeg", exts: "png,jpg,jpeg" }
    text: { enabled: true, mimes: "text/*", exts: "txt,md,log", max_inline_size: "1M", text_chunk_size: "100K" }
    audio: { enabled: true, mimes: "audio/mpeg,audio/wav", exts: "mp3,wav" }
    video: { enabled: true, mimes: "video/mp4,video/webm", exts: "mp4,webm" }
    document: { enabled: true, mimes: "application/pdf", exts: "pdf" }
    custom: { enabled: false, mimes: "", exts: "" }   # 兜底白名单，默认关闭
```

### FTP 服务

FTP/FTPS 的启用与端口在 `server.ftp` / `server.ftps` 节点配置（见「服务配置」），
TLS 证书由 `server.tls` 统一提供。客户端连接方式见 [FTP 访问](#ftp-访问)。

### 日志配置

```yaml
log:
  level: "info"        # 日志级别: debug, info, warn, error
  directory: "./logs"
  max_size: 100        # 单个日志文件大小 (MB)
  max_backups: 30      # 保留的日志文件数
  compress: true       # 是否压缩旧日志
```

### 身份认证（LDAP / OAuth2）

**系统设置 › 身份认证**页面不再区分标签页：上方固定展示本地账户设置，下方通过「接入方式」选择一种第三方身份认证系统，同一时间只启用一种，本地账户始终保留作为回退。

- **不启用**：仅使用本地账户密码登录（默认）。
- **LDAP**：对接企业目录（Active Directory / OpenLDAP）。启用后登录先走 LDAP 认证，
  失败再回退本地密码；可按周期同步目录用户，并选择是否自动创建本地账户。
- **OAuth2 / OIDC**：对接外部统一认证系统（授权码模式）。启用后登录页会展示外部认证入口，
  点击后跳转到外部系统完成认证，回调后自动映射为本地用户。支持 OIDC Issuer 自动发现端点。

> 配置说明与字段含义详见[认证与授权模块](../requirements/01-认证与授权模块.md)。

---

## 功能使用

### 登录与外部认证

- 本地账号：点击顶栏「登录」，输入用户名与密码。
- 外部认证：当管理员启用了 OAuth2/OIDC 后，登录弹框会显示「使用 xxx 登录」按钮，
  点击后跳转到外部认证系统，认证成功自动登录并返回应用。

### 文件浏览

1. 打开主页，显示根目录文件列表
2. 点击文件夹进入子目录
3. 点击文件名下载文件
4. 使用顶部搜索框搜索文件

### 文件上传

- 点击上传按钮或将文件拖拽到页面即可上传，大文件自动按分片上传并支持断点续传。
- 管理员开启 `upload.url_upload.enabled` 后，可在上传面板中填写文件 URL，由服务器下载到当前目录。
- 公共目录是否允许上传由管理员按目录配置；上传大小、分片大小与频率限制由服务端统一约束。

**临时分享（无需登录）：**

1. 进入「临时分享」并上传文件
2. 系统生成临时链接与提取码
3. 分享链接给他人，对方凭提取码下载
4. 文件按配置的过期天数自动清理，也可设置下载后立即删除

**私有存储（需要登录）：**

1. 登录后进入「我的文件」
2. 点击上传按钮或拖拽文件
3. 文件保存在私有空间，仅本人可见（管理员可在后台管理）

### 文件搜索

1. 在顶部搜索框输入关键词
2. 支持实时搜索、文件名联想与大小写不敏感匹配
3. 点击结果直接下载，或进入所在目录

### 文件预览

在文件列表中点击可预览的文件即可在线查看，支持图片、文本/代码、音频、视频、PDF 等类型。
管理员可在**系统设置 › 预览配置**中按分组开关并配置允许的 MIME 与扩展名；未启用或类型不支持时直接下载。

### 文件操作

普通用户可对**本人上传的公共文件**（当前IP与该文件的上传者IP一致）执行重命名、移动、删除操作；管理员对所有公共文件拥有管理权限。列表中仅对你有权限管理的文件显示"重命名/删除"按钮：
- **重命名**: 点击文件名旁的编辑按钮
- **移动**: 选中文件后点击移动，选择目标目录
- **删除**: 选中文件后点击删除

### 服务器资源监控

管理员可在**管理后台 › 资源监控**查看服务器运行状况（需在配置中开启 `resource.enabled`）：

- **服务器配置**：页面顶部展示 CPU 型号/核心数/主频、内存容量、磁盘数量与容量、网卡数量与速率。
- **实时曲线**：CPU、内存、磁盘占用、磁盘 IO、网络流量的实时曲线，标注当前占用率；
  内存同时展示总量与已用量的具体值。实时数据经 SSE（`/admin/resource/stream`）推送，
  不可用时自动降级为轮询。
- **历史曲线**：按 `collect_interval` 周期落库的历史数据，可切换时间范围查看，超长区间自动降采样。

### 管理后台

管理员登录后可进入管理后台，主要模块包括：

- **仪表盘**：用户数、文件数、存储用量、近期上传与分享等概览。
- **用户与用户组**：创建/禁用用户、重置密码、分配角色与用户组权限。
- **文件管理**：浏览并管理所有公共目录文件，支持重命名、移动、删除。
- **分享管理**：查看与撤销临时分享、私有分享链接。
- **系统设置**：应用名称、共享目录、上传/下载限制、存储后端、预览、MIME 表、数据库、访问控制、身份认证、Webhook、资源监控等。
- **日志查看**：按级别与来源筛选服务端日志，便于排查问题。
- **API Key 管理**：为脚本或第三方系统签发、禁用 API Key，并限制可用 IP/域名。
- **存储与索引**：配置存储后端（本地/S3/MinIO/OSS/WebDAV）及索引扫描策略；存储后端修改后需重启服务生效。

> 存储后端、数据库、HTTP/HTTPS/FTP 端口等底层配置修改后需重启服务生效；其余配置保存后即时生效。

### Webhook 通知

管理员可在**系统设置 › Webhook** 中配置应用向外部系统推送事件通知的 HTTP 回调。

**回调地址（端点）：** 采用「每个地址一条记录」的管理方式，每条记录可独立配置：

- **名称**：便于识别的显示名称（可选）。
- **启用**：单独开关；关闭后该端点不再接收推送，但仍可测试。
- **URL**：回调地址（HTTP POST 目标）。
- **事件过滤**：不选表示订阅全部事件；支持精确事件名（如 `file.uploaded`）、
  通配符 `*`（匹配全部）与前缀通配（如 `file.*` 匹配所有文件相关事件）。
- **密钥 / 超时 / 重试**：留空或 0 表示继承全局默认值（签名密钥、单次请求超时、失败重试次数）。
- **接收方鉴权（Headers / Query）**：配置回调时附加的自定义请求头与 URL 查询参数，
  常用于携带接收方的 Bearer Token、API Key 或 URL Token。界面提供
  `none / Bearer Token / API Key / URL Token / 自定义` 快速预设，仅用于快速填充键值。
- **测试**：每条记录可单独发起测试推送，展示返回的 HTTP 状态码与耗时；也支持「测试全部」。
  测试忽略启用状态与事件过滤，直接向该地址发送一条测试事件。

**发送方签名：** 推送请求携带 `X-Fuzhan-Signature` 报头，值为对请求体使用密钥（全局或端点级）
计算得到的 HMAC-SHA256 十六进制摘要，接收方可用于校验请求确实来自本服务。界面中密钥明文可见，
服务端日志对 URL 查询参数中的敏感键值及鉴权请求头值做脱敏（以 `***` 展示）。

**全局设置：**

- **默认签名密钥 / 默认超时 / 默认重试**：所有端点的缺省值。
- **队列容量**：进程内事件队列大小，默认 1024；超过时新事件被丢弃并写入告警日志。
  修改队列容量需**重启服务**生效，其余 Webhook 配置保存后即时生效。

**事件目录：** 完整的可订阅事件清单可从管理界面的事件选择器中查看，也可通过 API
`GET /api/v1/web/config/webhook/events` 获取，涵盖文件上传/重命名/移动/删除、
临时与私有文件下载/删除、URL 上传任务、登录与用户管理、会话清理、证书与密钥上传、记录清理等 33 个事件。

---

## 界面外观

### 主题切换

界面主题跟随系统自动适配，同时支持手动切换：

- 公共端：点击顶栏右上角的主题图标，可在「跟随系统 / 亮色 / 暗色」间切换。
- 管理端：点击内容区右上角悬浮的圆形主题按钮，同样支持三种模式切换。
- 选择会被记住并持久化；不同标签页之间自动同步。

主题偏好保存在浏览器本地（`localStorage`），仅影响当前浏览器，不会更改服务器端配置。

---

## FTP 访问

### 连接信息

| 项目 | 值 |
|------|-----|
| 服务器 | 你的服务器地址 |
| 端口 | 2121 |
| 用户名 | `anonymous` |
| 密码 | 任意或留空 |

### 使用方式

**Windows 资源管理器：**
1. 打开文件资源管理器
2. 在地址栏输入 `ftp://服务器地址:2121`
3. 无需登录，直接访问

**浏览器：**
```
ftp://localhost:2121
```

**FileZilla：**
1. 主机: `ftp://你的服务器地址`
2. 端口: `2121`
3. 协议: FTP
4. 加密: 不使用 TLS
5. 登录类型: 匿名

### 可用操作

- 浏览共享目录内容
- 下载文件到本地
- 上传本地文件到根目录
- 创建新文件夹
- 删除文件/文件夹

---

## API 访问

服务端提供 REST API，接口前缀为 `/api/v1`，可用于脚本、自动化与第三方集成。

- **认证方式**：登录后获取 JWT，通过 `Authorization: Bearer <token>` 调用；或在管理后台创建 API Key，通过 `X-API-Key: <key>` 调用。
- **统一响应**：`{ "success": true, "code": 0, "message": "", "data": ... }`。
- **API Key 限制**：可为每个 Key 配置允许访问的 IP / 域名白名单，未配置时默认拒绝。
- **完整端点**：参见 [API 接口文档](../design/api/api-contracts.md)。

---

## 常见问题

### 端口被占用

**症状**: 启动时报 "port already in use" 错误

**解决方法:**

Windows:
```powershell
netstat -ano | findstr :8080
# 找到占用端口的进程 PID
taskkill /PID <PID> /F
```

Linux/macOS:
```bash
lsof -i :8080
kill -9 <PID>
```

### 无法访问服务

1. 检查防火墙是否放行端口
2. 确认服务是否正常启动（查看日志）
3. 检查配置中的 host 是否为 `0.0.0.0`

### 磁盘空间不足

- 检查共享目录配额设置
- 清理临时文件目录 `./data/temp`
- 清理日志文件 `./logs`

### 忘记管理员密码

删除数据库文件后重新启动，服务会自动进入初始配置状态：

```bash
# Windows
del fuzhan.db

# Linux/macOS
rm fuzhan.db
```

### 查看日志

日志文件位于 `./logs/fuzhan.log`（或 `./logs/app.log`）：

```bash
# Windows
type logs\fuzhan.log

# Linux/macOS
cat logs/fuzhan.log
```

### 安装为系统服务

**Windows:**
```powershell
.\fuzhan.exe --install
```

**Linux (systemd):**
```bash
sudo ./fuzhan --install
```

**卸载服务:**
```bash
fuzhan --uninstall
```

---

## 技术支持

如遇到问题，可通过以下方式获取帮助：

- 查看 `logs/` 目录下的日志文件
- 检查 [常见问题](#常见问题) 部分
- 参考 [开发指南](../development/development-guide.md) 了解更多配置选项
