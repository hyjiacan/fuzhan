# FTP/WebDAV 使用指南

## 概述

fuzhan 内建 FTP 和 WebDAV 服务，支持通过标准客户端访问共享文件。

### 目录结构

```
/ (根目录)
  public/           ← 共享根目录（所有用户可见）
    photos/         ← 配置的共享根目录
    documents/
  private/          ← 私有存储（仅认证用户可见）
                     （需启用私有存储且用户已登录认证）
```

### 认证方式

- **匿名访问**：无需提供凭据，只能访问 `public/` 目录
- **认证访问**：提供有效用户名和密码，可访问 `public/` + `private/` 目录

### 账户配置

```yaml
account:
  anonymous:
    username: "public"  # 匿名/公开目录用户名
  reserved_usernames:
    - admin
    - root
```

`account.anonymous.username` 是公用账户的约定名称，系统中需存在对应用户。FTP 客户端可使用 `anonymous` 或此用户名免密登录。

---

## WebDAV 配置和使用

### 服务端配置

```yaml
server:
  webdav:
    enabled: false     # 设为 true 启用
    port: 0            # 0 表示与 HTTP/HTTPS 同端口
```

WebDAV 统一端点为 `/api/v1/webdav`，支持可选 BasicAuth 认证。

### curl 操作示例

```bash
# 列出 public/ 目录（匿名）
curl -X PROPFIND http://localhost:8888/api/v1/webdav/public/

# 上传文件到 public/ 目录（需要认证用户有写入权限）
curl -u username:password -T file.txt http://localhost:8888/api/v1/webdav/public/my_files/file.txt

# 从 public/ 下载文件（匿名）
curl http://localhost:8888/api/v1/webdav/public/my_files/file.txt

# 访问 private/ 目录（需要认证）
curl -u username:password -X PROPFIND http://localhost:8888/api/v1/webdav/private/
```

### Windows 资源管理器

1. 打开「此电脑」→ 右键「添加一个网络位置」
2. 输入地址：`http://localhost:8888/api/v1/webdav/public/`
3. 如需认证，勾选「登录时使用其他用户名」并输入凭据
4. 完成后可在资源管理器中直接操作文件

### macOS Finder

1. 菜单栏「前往」→「连接服务器」(Cmd+K)
2. 输入地址：`http://localhost:8888/api/v1/webdav/public/`
3. 选择「注册用户」并输入凭据（匿名则选「访客」）
4. 连接后可在 Finder 中直接操作

### Linux davfs2 挂载

```bash
# 安装 davfs2
sudo apt install davfs2  # Debian/Ubuntu
sudo yum install davfs2  # CentOS/RHEL

# 挂载（匿名）
sudo mount -t davfs http://localhost:8888/api/v1/webdav/public/ /mnt/webdav

# 挂载（认证）
sudo mount -t davfs http://localhost:8888/api/v1/webdav/ /mnt/webdav
# 将用户名和密码写入 ~/.davfs2/secrets：
# http://localhost:8888/api/v1/webdav/ username password
```

### 第三方客户端

- **RaiDrive** (Windows)：支持 WebDAV 映射为磁盘，界面友好
- **Cyberduck** (跨平台)：支持 WebDAV，可配置连接书签
- **WinSCP** (Windows)：选择 WebDAV 协议连接

---

## FTP/FTPS 配置和使用

### 服务端配置

```yaml
server:
  # FTP 配置
  ftp:
    enabled: false
    port: 21           # FTP 端口
  # FTPS 配置（启用需配置 server.tls）
  ftps:
    enabled: false
    port: 990          # FTPS 端口
```

### 认证方式

| 用户名 | 密码 | 访问范围 |
|---|---|---|
| `anonymous` | 任意或空 | 仅 public/ |
| 配置的公用用户名（如 `public`） | 对应密码 | 仅 public/ |
| 注册用户 | 用户密码 | public/ + private/ |

### 命令行示例

```bash
# 匿名登录（只读 public/）
ftp localhost 21
# Name: anonymous
# Password: (任意或直接回车)

# 用户登录
ftp localhost 21
# Name: username
# Password: your_password

# 列出文件
ftp> ls
# 切换到 public/
ftp> cd public
ftp> ls

# 下载文件
ftp> get public/documents/report.pdf

# 上传文件（public/ 目录可创建新文件，不可修改已有文件）
ftp> put localfile.txt public/
```

### 被动模式

如果客户端在 NAT 或防火墙后面，需要启用被动模式：

```bash
ftp> passive
```

### Windows 资源管理器

1. 打开「此电脑」→ 在地址栏输入：`ftp://localhost:21/`
2. 右键选择「登录」，输入用户名和密码
3. 匿名可勾选「匿名登录」

### FileZilla 客户端

1. 文件 → 站点管理器 → 新站点
2. 协议：FTP 或 FTP over TLS (FTPS)
3. 主机：`localhost`，端口：`21` 或 `990`
4. 登录类型：
   - 匿名：选择「匿名」
   - 用户：选择「正常」，输入用户名和密码
5. 连接后自动显示目录结构

### FTPS 连接（加密）

启用 TLS 加密后，所有 FTP 连接需使用 FTPS：

```bash
# 使用 lftp（Linux/macOS）
lftp -u username,password ftps://localhost:990

# 使用 curl
curl -k --ftp-ssl -u username:password ftp://localhost:21/
```

---

## 操作记录

通过 WebDAV 和 FTP 进行的上传、下载、创建目录操作会自动记录到数据库的 `operation_records` 表中。

可在 Web 管理界面的监控页面查看最近的访问统计和操作记录。

### 查看记录

- Web 界面：监控页面 → 访问统计/关键词排行/最近操作
- 服务端日志：查看访问日志中的操作记录

---

## 注意事项

1. **public/ 目录权限**：可创建新文件和目录，不可修改或删除已有文件
2. **private/ 目录权限**：完整读写权限，前提是已启用私有存储并完成用户认证
3. **FTP 被动模式**：如果客户端无法列出目录，尝试启用被动模式
4. **路径分隔符**：始终使用 `/`（正斜杠），兼容所有操作系统
5. **编码**：支持 UTF-8 文件名
