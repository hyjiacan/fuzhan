# API 接口文档

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-22

---

## 目录

- [认证](#认证)
- [公共文件操作](#公共文件操作)
- [分片上传](#分片上传)
- [URL 下载](#url-下载)
- [临时文件](#临时文件)
- [私有存储](#私有存储)
- [通知系统](#通知系统)
- [管理员接口](#管理员接口)
- [数据库迁移](#数据库迁移)
- [系统配置](#系统配置)
- [健康检查与监控](#健康检查与监控)
- [CLI 接口](#cli-接口)
- [API Key 管理](#api-key-管理)
- [统一响应格式](#统一响应格式)
- [认证中间件](#认证中间件)
- [分片上传状态机](#分片上传状态机)

---

## 认证

| HTTP方法 | 路由 | Handler | 功能描述 | 认证 |
|----------|------|---------|----------|------|
| POST | `/api/v1/auth/register` | `auth.Handler.Register` | 用户注册 | 无 |
| POST | `/api/v1/auth/login` | `auth.Handler.Login` | 用户登录 | 无 |
| POST | `/api/v1/auth/refresh` | `auth.Handler.RefreshToken` | 刷新Token | JWT |
| GET | `/api/v1/auth/user` | `auth.Handler.GetCurrentUser` | 获取当前用户 | JWT |
| POST | `/api/v1/auth/change-password` | `auth.Handler.ChangePassword` | 修改密码 | JWT |

### 注册请求

```json
POST /api/v1/auth/register
Content-Type: application/json

{
  "username": "string (3-32字符)",
  "password": "string (6-128字符)"
}

// 响应
{
  "success": true,
  "data": {
    "token": "JWT字符串",
    "uuid": "用户UUID",
    "username": "用户名",
    "expiresIn": 86400
  }
}
```

### 登录请求/响应

```json
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "string",
  "password": "string"
}

// 响应
{
  "success": true,
  "data": {
    "token": "JWT字符串",
    "uuid": "用户UUID",
    "username": "用户名",
    "expiresIn": 86400
  }
}
```

---

## 公共文件操作

| HTTP方法 | 路由 | Handler | 功能描述 | 认证 |
|----------|------|---------|----------|------|
| GET | `/api/v1/files/list` | `file.Handler.ListDirectories` | 目录列表 | 无 |
| GET | `/api/v1/files/preview/*path` | `file.PreviewHandler.PreviewFile` | 文件预览 | 无 |
| GET | `/api/v1/files/preview-chunk` | `file.PreviewHandler.PreviewChunk` | 文本分片预览 | 无 |
| GET | `/api/v1/files/recent` | `file.RecentHandler.GetRecent` | 最近上传 | 无 |
| GET | `/api/v1/files/recent/carousel` | `file.RecentHandler.GetRecentCarousel` | 首页轮播 | 无 |
| GET | `/download/*path` | `file.DownloadHandler.DownloadFile` | 文件下载 | 无 |
| GET | `/api/v1/search/*query` | `file.SearchHandler.SearchFiles` | 文件搜索(SSE) | 无 |
| POST | `/api/v1/files/rename` | `file.Handler.RenameFileHandler` | 重命名文件 | JWT |
| POST | `/api/v1/files/move` | `file.Handler.MoveFileHandler` | 移动文件 | JWT |
| DELETE | `/api/v1/files` | `file.Handler.DeleteFileHandler` | 删除文件 | JWT |

### 目录列表

```json
GET /api/v1/files/list?path=rootName/subdir

// 响应
{
  "success": true,
  "data": {
    "currentPath": "rootName/subdir",
    "items": [
      {
        "name": "文件名.txt",
        "path": "rootName/subdir/文件名.txt",
        "type": "file",
        "size": 1024,
        "modifiedTime": "2026-07-22T10:00:00Z"
      },
      {
        "name": "子目录",
        "path": "rootName/subdir/子目录",
        "type": "directory",
        "size": 0,
        "modifiedTime": "2026-07-22T10:00:00Z",
        "quota": 10737418240,
        "used": 5368709120
      }
    ]
  }
}
```

### 文件搜索 (SSE)

```json
GET /api/v1/search/关键词

// SSE 流式响应
event: result
data: {"name":"file.txt","path":"root/file.txt","type":"file","size":1024,"modifiedTime":"..."}

event: result
data: {"name":"file2.txt","path":"root2/file2.txt","type":"file","size":2048,"modifiedTime":"..."}

event: message
data: {"success":true,"message":"搜索完成，共找到 2 个结果","data":[]}

event: done
data: {"total":2,"matched":2}
```

---

## 分片上传

无需认证，支持断点续传。

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| POST | `/api/v1/uploads/session` | `file.UploadSessionHandler.CreateSession` | 创建上传会话 |
| GET | `/api/v1/uploads/session/:id` | `file.UploadSessionHandler.GetSession` | 获取会话状态 |
| POST | `/api/v1/uploads/session/:id/resume` | `file.UploadSessionHandler.ResumeSession` | 续传 |
| DELETE | `/api/v1/uploads/session/:id` | `file.UploadSessionHandler.CancelSession` | 取消会话 |
| POST | `/api/v1/uploads/chunk` | `file.UploadSessionHandler.UploadChunk` | 上传分片 |
| POST | `/api/v1/uploads/finalize` | `file.UploadSessionHandler.FinalizeSession` | 完成上传 |
| POST | `/api/v1/uploads/url` | `modules.UploadFromURL` | 从URL上传 |
| POST | `/api/v1/uploads/url_info` | `modules.GetFileInfoFromURL` | 获取URL文件信息 |

### 创建会话

```json
POST /api/v1/uploads/session
Content-Type: application/json

{
  "filename": "example.zip",
  "fileSize": 104857600,
  "dir": "subdir",
  "rootName": "share1",
  "targetType": "regular"
}

// 响应
{
  "success": true,
  "data": {
    "uploadId": 1,
    "chunkSize": 10485760,
    "totalChunks": 10,
    "expiredAt": "2026-07-23T10:00:00Z"
  }
}
```

### 上传分片

```json
POST /api/v1/uploads/chunk
Content-Type: multipart/form-data

uploadId: 1
chunkIndex: 0
file: <二进制数据>
checksum: xxhash64_hex
sessionId: 1

// 响应
{
  "success": true,
  "data": {
    "chunkIndex": 0,
    "offset": 0
  }
}
```

### 完成上传

```json
POST /api/v1/uploads/finalize
Content-Type: application/json

{
  "uploadId": 1
}

// 响应
{
  "success": true,
  "message": "文件上传完成",
  "data": {}
}
```

---

## URL 下载

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/url-tasks` | `URLTaskHandler.ListTasks` | 列出我的URL下载任务 |
| GET | `/api/v1/url-tasks/:id` | `URLTaskHandler.GetTask` | 获取任务详情 |

---

## 临时文件

基于 IP 访问的临时文件分享。

| HTTP方法 | 路由 | Handler | 功能描述 | 认证 |
|----------|------|---------|----------|------|
| GET | `/api/v1/temp/list` | `temp.Handler.ListHandler` | 获取临时文件列表 | IP |
| POST | `/api/v1/temp/upload` | `temp.Handler.UploadHandler` | 上传临时文件 | IP |
| GET | `/api/v1/temp/quota` | `temp.Handler.QuotaHandler` | 查询IP配额 | IP |
| GET | `/api/v1/temp/client-ip` | `temp.Handler.ClientIPHandler` | 获取客户端IP | 无 |
| GET | `/api/v1/temp/:code` | `temp.Handler.InfoHandler` | 获取文件信息 | 无 |
| GET | `/api/v1/temp/:code/download` | `temp.Handler.DownloadHandler` | 下载临时文件 | 无 |
| DELETE | `/api/v1/temp/:code` | `temp.Handler.DeleteHandler` | 删除临时文件 | IP |
| PUT | `/api/v1/temp/:code/extend` | `temp.Handler.ExtendHandler` | 延长有效期 | IP |

### 临时文件分片上传

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| POST | `/api/v1/temp/upload/session` | `temp.Handler.CreateTempSessionHandler` | 创建会话 |
| GET | `/api/v1/temp/upload/session/:uploadId` | `temp.Handler.GetTempSessionHandler` | 获取状态 |
| POST | `/api/v1/temp/upload/chunk` | `temp.Handler.UploadTempChunkHandler` | 上传分片 |
| POST | `/api/v1/temp/upload/finalize` | `temp.Handler.FinalizeTempUploadHandler` | 完成上传 |

### 临时文件响应

```json
// 响应
{
  "success": true,
  "data": {
    "code": "A1B2C3D4",
    "filename": "file.zip",
    "fileSize": 1024000,
    "expiredAt": "2026-07-19T10:00:00Z",
    "downloadUrl": "/api/v1/temp/A1B2C3D4/download"
  }
}
```

---

## 私有存储

需要 JWT 认证。

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/private/files` | `file.PrivateStorageHandler.List` | 获取文件列表 |
| GET | `/api/v1/private/quota` | `file.PrivateUploadHandler.QuotaHandler` | 获取配额 |
| POST | `/api/v1/private/upload` | `file.PrivateStorageHandler.Upload` | 简单上传 |
| DELETE | `/api/v1/private/files/:code` | `file.PrivateStorageHandler.Delete` | 删除文件 |
| GET | `/api/v1/share/:code` | `file.PrivateStorageHandler.Download` | 下载分享文件 |

### 私有存储分片上传

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| POST | `/api/v1/private/uploads/session` | `file.PrivateUploadHandler.CreateSession` | 创建会话 |
| GET | `/api/v1/private/uploads/session/:id` | `file.PrivateUploadHandler.GetSession` | 获取状态 |
| POST | `/api/v1/private/uploads/session/:id/resume` | `file.PrivateUploadHandler.ResumeSession` | 续传 |
| DELETE | `/api/v1/private/uploads/session/:id` | `file.PrivateUploadHandler.CancelSession` | 取消 |
| POST | `/api/v1/private/uploads/chunk` | `file.PrivateUploadHandler.UploadChunk` | 上传分片 |
| POST | `/api/v1/private/uploads/finalize` | `file.PrivateUploadHandler.FinalizeSession` | 完成 |

---

## 通知系统

| HTTP方法 | 路由 | Handler | 功能描述 | 认证 |
|----------|------|---------|----------|------|
| GET | `/api/v1/notifications` | `notification.Handler.GetNotifications` | 获取通知列表 | JWT/Anonymous |
| PUT | `/api/v1/notifications/:id/read` | `notification.Handler.MarkAsRead` | 标记已读 | JWT |
| POST | `/api/v1/notifications/merge` | `notification.Handler.MergeAnonymous` | 合并匿名通知 | JWT |

---

## 管理员接口

需要 JWT + Admin 角色。

### 用户管理

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/admin/users` | `admin.Handler.UsersHandler` | 用户列表 |
| PUT | `/api/v1/admin/users/:uuid/reset-password` | `admin.Handler.ResetPasswordHandler` | 重置密码 |
| PUT | `/api/v1/admin/users/:uuid/disabled` | `admin.Handler.SetUserDisabledHandler` | 禁用/启用 |
| DELETE | `/api/v1/admin/users/:uuid` | `admin.Handler.DeleteUserHandler` | 删除用户 |
| GET | `/api/v1/admin/sessions` | `admin.Handler.SessionsHandler` | 上传会话列表 |
| POST | `/api/v1/admin/sessions/cleanup` | `admin.Handler.CleanupSessionsHandler` | 清理会话 |

### 文件管理

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/admin/files/list` | `file.Handler.ListDirectories` | 文件列表 |
| POST | `/api/v1/admin/files/rename` | `file.Handler.RenameFileHandler` | 重命名 |
| POST | `/api/v1/admin/files/move` | `file.Handler.MoveFileHandler` | 移动 |
| DELETE | `/api/v1/admin/files` | `file.Handler.DeleteFileHandler` | 删除 |

### URL 下载任务管理

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/admin/url-tasks` | `admin.URLDownloadHandler.ListURLTasks` | 任务列表 |
| DELETE | `/api/v1/admin/url-tasks/:id` | `admin.URLDownloadHandler.DeleteURLTask` | 删除任务 |

### 数据库迁移

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/admin/database/status` | `admin.Handler.GetMigrationStatus` | 获取迁移状态 |
| POST | `/api/v1/admin/database/test` | `admin.Handler.TestDatabaseConnection` | 测试数据库连接 |
| POST | `/api/v1/admin/database/migrate` | `admin.Handler.StartMigration` | 开始迁移 | -> SSE |
| POST | `/api/v1/admin/database/cancel` | `admin.Handler.CancelMigration` | 取消迁移 |
| POST | `/api/v1/admin/database/resume` | `admin.Handler.ResumeMigration` | 恢复迁移 |
| POST | `/api/v1/admin/database/restart` | `admin.Handler.RestartMigration` | 重启迁移 |
| POST | `/api/v1/admin/database/rollback` | `admin.Handler.RollbackMigration` | 回滚迁移 |

### 备份管理

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/admin/backups` | `admin.Handler.ListBackups` | 备份列表 |
| POST | `/api/v1/admin/backups` | `admin.Handler.CreateBackup` | 创建备份 |
| POST | `/api/v1/admin/backups/:id/restore` | `admin.Handler.RestoreBackup` | 恢复备份 |
| DELETE | `/api/v1/admin/backups/:id` | `admin.Handler.DeleteBackup` | 删除备份 |

### TLS 证书

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| POST | `/api/v1/admin/tls/upload` | `admin.Handler.UploadTLSCertificate` | 上传TLS证书 |

---

## 系统配置

| HTTP方法 | 路由 | Handler | 功能描述 | 认证 |
|----------|------|---------|----------|------|
| GET | `/api/v1/setup/status` | `setup.Handler.IsInitialized` | 检查初始化状态 | 无 |
| POST | `/api/v1/setup/save` | `setup.Handler.SaveConfig` | 保存初始配置 | 无 |
| POST | `/api/v1/setup/validate-dir` | `setup.Handler.ValidateDirectory` | 验证目录 | 无 |
| GET | `/api/v1/setup/network/interfaces` | `setup.Handler.GetNetworkInterfaces` | 获取网络接口 | 无 |
| GET | `/api/v1/setup/default-config` | `setup.Handler.GetDefaultConfig` | 获取默认配置 | 无 |
| GET | `/api/v1/config` | `config.Handler.GetConfig` | 获取系统配置 | JWT+Admin |
| POST | `/api/v1/config` | `config.Handler.SaveConfig` | 保存系统配置 | JWT+Admin |
| GET | `/api/v1/options` | `config.Handler.GetOptions` | 获取公开配置选项 | 无 |
| POST | `/api/v1/config/reload` | `config.Handler.ReloadConfig` | 重新加载配置 | JWT+Admin |

---

## 健康检查与监控

| HTTP方法 | 路由 | Handler | 功能描述 | 认证 |
|----------|------|---------|----------|------|
| GET | `/api/v1/health` | `health.Handler.Health` | 健康检查 | 无 |
| GET | `/api/v1/ready` | `health.Handler.Ready` | 就绪检查 | 无 |
| GET | `/api/v1/metrics` | `PrometheusHandler()` | Prometheus指标 | 无 |
| GET | `/api/v1/monitor/storage` | `monitor.Handler.Storage` | 存储统计 | 无 |
| GET | `/api/v1/monitor/access` | `monitor.Handler.Access` | 访问统计 | 无 |
| GET | `/api/v1/monitor/keywords` | `monitor.Handler.Keywords` | 常用搜索词 | 无 |
| GET | `/api/v1/monitor/recent` | `monitor.Handler.RecentKeywords` | 最近搜索词 | 无 |
| GET | `/api/v1/monitor/rankings` | `monitor.Handler.Rankings` | 排行榜 | 无 |
| GET | `/api/v1/monitor/hot-downloads` | `monitor.Handler.HotDownloads` | 热门下载排行 | 无 |
| GET | `/api/v1/system/shutdown` | `ShutdownHandler` | 关闭服务 | 本地 |

### 存储统计响应

```json
GET /api/v1/monitor/storage

{
  "success": true,
  "data": {
    "totalSpace": 107374182400,
    "usedSpace": 53687091200,
    "freeSpace": 53687091200,
    "roots": [
      {
        "name": "share1",
        "path": "/data/share1",
        "total": 107374182400,
        "used": 53687091200,
        "free": 53687091200,
        "quota": 107374182400
      }
    ],
    "temp": {
      "quotaPerIP": 524288000,
      "usedByIP": {
        "192.168.1.100": 104857600
      }
    },
    "private": {
      "usedByUser": {
        "user-uuid-1": 20971520
      }
    }
  }
}
```

---

## CLI 接口

需要 JWT 认证。

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/cli` | `cli.Handler.HandleCli` | CLI通用入口 |
| GET | `/api/v1/cli/search/*query` | `cli.Handler.CliSearch` | CLI搜索 |
| GET | `/api/v1/cli/list/*path` | `cli.Handler.CliList` | CLI列表 |

---

## API Key 管理

需要管理员角色。

| HTTP方法 | 路由 | Handler | 功能描述 |
|----------|------|---------|----------|
| GET | `/api/v1/api-keys` | `auth.APIKeyHandler.List` | 列出 API Keys |
| POST | `/api/v1/api-keys` | `auth.APIKeyHandler.Create` | 创建 API Key |
| DELETE | `/api/v1/api-keys/:id` | `auth.APIKeyHandler.Delete` | 删除 API Key |

### 创建 API Key

```json
POST /api/v1/api-keys
Content-Type: application/json

{
  "name": "my-app-key",
  "expiresIn": 86400  // 秒，0 表示不过期
}

// 响应
{
  "success": true,
  "data": {
    "id": "key-uuid",
    "name": "my-app-key",
    "key": "ls_api_xxxxxxxxxxxxxx",  // 仅返回一次
    "createdAt": "2026-07-22T10:00:00Z",
    "expiresAt": "2026-07-23T10:00:00Z"
  }
}
```

---

## 统一响应格式

```json
// 成功
{
  "success": true,
  "message": "操作成功",
  "data": {}
}

// 失败
{
  "success": false,
  "message": "错误信息",
  "details": {}
}
```

---

## 认证中间件

| 中间件 | 说明 |
|--------|------|
| `AuthRequired()` | 需要有效JWT Token |
| `AuthOptional()` | 可选认证（有Token解析但不强制） |
| `RequireAdmin()` | 需要管理员角色 |
| `APIKeyAuth()` | 需要有效的 API Key |
| `TokenRefreshMiddleware()` | 自动刷新即将过期的Token |
| `AuditMiddleware()` | 审计日志记录 |
| `ResponseWrapperMiddleware()` | 统一响应格式 |
| 用户禁用检查 | 认证时检查 Disabled 字段 |

---

## 分片上传状态机

```
                    ┌─────────────┐
                    │   pending   │
                    └──────┬──────┘
                           │ 创建会话
                           ▼
                    ┌─────────────┐
              ┌─────│ in_progress │─────┐
              │     └─────────────┘     │
              │           │             │
   上传分片    │           │  会话过期   │
              │           ▼             │
              │    ┌─────────────┐      │
              │    │   expired   │      │
              │    └─────────────┘      │
              │                         │
              │     ┌─────────────┐     │
              └────►│  cancelled  │◄────┘
                    └─────────────┘
                           │
                    完成上传 (finalize)
                           │
                    ┌─────────────┐
                    │  completed  │
                    └─────────────┘
```