# 用户手册

> 浮栈 (Light Share) 用户指南 | 更新时间: 2026-06-12

---

## 目录

- [快速开始](#快速开始)
- [配置指南](#配置指南)
- [功能使用](#功能使用)
- [FTP 访问](#ftp-访问)
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

---

## 配置指南

配置文件为 `fuzhan.yaml`，位于程序同目录下。

### 服务配置

```yaml
server:
  host: "0.0.0.0"  # 监听地址，0.0.0.0 表示所有网卡
  port: 8080       # 服务端口
```

### 共享目录

```yaml
storage:
  public:
    root_dirs:
      - path: "./data/share1"
        quota: "10GB"   # 配额限制，如 "10GB"、"500MB"
      - path: "./data/photos"
        quota: "50GB"
```

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

### FTP 服务

```yaml
ftp:
  enabled: true
  port: 2121           # FTP 端口
  tls: false           # 是否启用 TLS 加密
```

### 日志配置

```yaml
log:
  level: "info"        # 日志级别: debug, info, warn, error
  directory: "./logs"
  max_size: 100        # 单个日志文件大小 (MB)
  max_backups: 30      # 保留的日志文件数
  compress: true       # 是否压缩旧日志
```

---

## 功能使用

### 文件浏览

1. 打开主页，显示根目录文件列表
2. 点击文件夹进入子目录
3. 点击文件名下载文件
4. 使用顶部搜索框搜索文件

### 文件上传

**临时分享（无需登录）：**
1. 点击"临时上传"或拖拽文件到上传区域
2. 系统生成临时链接和提取码
3. 分享链接给他人下载

**私有存储（需要登录）：**
1. 登录后进入"我的文件"
2. 点击上传按钮或拖拽文件
3. 文件保存在私有空间中

### 文件搜索

1. 在顶部搜索框输入关键词
2. 支持实时搜索，显示匹配结果
3. 点击结果直接下载

### 文件操作

普通用户可对**本人上传的公共文件**（当前IP与该文件的上传者IP一致）执行重命名、移动、删除操作；管理员对所有公共文件拥有管理权限。列表中仅对你有权限管理的文件显示"重命名/删除"按钮：
- **重命名**: 点击文件名旁的编辑按钮
- **移动**: 选中文件后点击移动，选择目标目录
- **删除**: 选中文件后点击删除

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
- 参考 [开发指南](./development-guide.md) 了解更多配置选项
