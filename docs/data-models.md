# 数据模型文档

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-02

---

## 目录

- [User 用户模型](#user-用户模型)
- [TempFile 临时文件模型](#tempfile-临时文件模型)
- [UploadSession 上传会话模型](#uploadsession-上传会话模型)
- [UploadedChunk 已上传分片模型](#uploadedchunk-已上传分片模型)
- [OperationRecord 操作记录模型](#operationrecord-操作记录模型)
- [URLDownloadTask URL 上传任务模型](#urldownloadtask-url-上传任务模型)
- [Notification 通知模型](#notification-通知模型)
- [SecurityEvent 安全事件模型](#securityevent-安全事件模型)
- [关联关系图](#关联关系图)
- [数据库支持](#数据库支持)
- [自动迁移](#自动迁移)

---

## User 用户模型

- **表名**: `users`
- **说明**: 用户认证信息

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| UUID | string(36) | uniqueIndex, not null | 用户唯一标识 |
| Username | string(64) | uniqueIndex, not null | 用户名 |
| PasswordHash | string(256) | not null | bcrypt密码哈希 |
| Role | string(32) | default: "user" | 角色 (user/admin) |
| Disabled | bool | default: false | 是否禁用 |
| CreatedAt | time.Time | - | 创建时间 |
| UpdatedAt | time.Time | - | 更新时间 |
| DeletedAt | gorm.DeletedAt | index | 软删除 |

---

## TempFile 临时文件模型

- **表名**: `temp_files`
- **说明**: 临时文件分享记录，基于 IP 访问

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| Code | string(8) | uniqueIndex, not null | 访问码 (8位) |
| Filename | string(255) | not null | 原始文件名 |
| FileSize | int64 | not null | 文件大小 |
| FilePath | string(512) | json:"-" | 存储路径 (内部使用) |
| Dir | string(512) | - | 存储目录 |
| ClientIP | string(45) | index | 上传者IP |
| DownloadCount | int | default: 0 | 下载次数 |
| NeverExpire | bool | default: false | 是否永不过期 |
| ExpiredAt | time.Time | index | 过期时间 |
| CreatedAt | time.Time | - | 创建时间 |
| DeletedAt | gorm.DeletedAt | index | 软删除 |

**索引**:
- `Code`: uniqueIndex (快速查找)
- `ClientIP`: index
- `ExpiredAt`: index

---

## UploadSession 上传会话模型

- **表名**: `upload_sessions`
- **说明**: 分片上传会话管理，支持断点续传

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| FileName | string(255) | not null | 文件名 |
| FileSize | int64 | not null | 文件大小 |
| ChunkSize | int64 | not null | 分片大小 |
| TotalChunks | int | not null | 总分片数 |
| Checksum | string(64) | - | 文件校验和 |
| Status | UploadStatus | size:20, not null, default: "pending" | 会话状态 |
| TargetType | TargetType | size:20, not null, default: "regular" | 目标类型 |
| TargetPath | string(512) | - | 目标路径 |
| TargetRoot | string(255) | - | 目标根目录 |
| UserID | string(64) | - | 用户ID |
| ChunkDir | string(512) | - | 分片存储目录 |
| CreatedAt | time.Time | - | 创建时间 |
| UpdatedAt | time.Time | - | 更新时间 |
| ExpiredAt | time.Time | index | 过期时间 |
| DeletedAt | gorm.DeletedAt | index | 软删除 |

### UploadStatus 枚举

| 值 | 说明 |
|----|------|
| pending | 等待上传 |
| in_progress | 上传中 |
| completed | 已完成 |
| failed | 失败 |
| cancelled | 已取消 |
| expired | 已过期 |

### TargetType 枚举

| 值 | 说明 |
|----|------|
| regular | 常规共享文件 |
| temp | 临时文件 |
| private | 私有存储 |

**索引**:
- `ExpiredAt`: 清理过期会话
- `DeletedAt`: 软删除查询

**关联关系**:
- UploadSession **HasMany** → UploadedChunk (通过 SessionID)

---

## UploadedChunk 已上传分片模型

- **表名**: `uploaded_chunks`
- **说明**: 已上传分片记录

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| SessionID | uint | not null, uniqueIndex:idx_session_chunk | 所属会话ID |
| ChunkIndex | int | not null, uniqueIndex:idx_session_chunk | 分片索引 |
| ChunkChecksum | string(64) | - | 分片校验和 |
| ChunkSize | int64 | not null | 分片大小 |
| CreatedAt | time.Time | - | 创建时间 |

**索引**:
- `idx_session_chunk`: 联合唯一索引 (SessionID + ChunkIndex)

**关联关系**:
- UploadedChunk **BelongsTo** → UploadSession (SessionID)

---

## OperationRecord 操作记录模型

- **表名**: `operation_records`
- **说明**: 上传/下载/搜索等操作审计记录。行为分析中的下载行为基于其中 `dl_event=1` 的公开下载事件聚合（私有/临时来源不计入）。

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| FileName | string(255) | not null | 文件名 |
| FileSize | int64 | not null | 文件大小 |
| FilePath | string(512) | not null | 相对根目录的文件路径（带前导 /） |
| FullPath | string(1024) | - | 完整路径 rootName/filePath |
| RootName | string(64) | not null | 根目录名称 |
| FileType | string(50) | - | 文件类型 |
| Status | string(10) | not null, default:"success" | 处理状态 (success/failed) |
| FailReason | string(255) | - | 失败原因（仅 Status=failed 时填写） |
| SourceType | string(16) | default:"public" | 下载来源类型 (public/private/temp) |
| SourceID | uint | default:0 | 来源文件索引记录ID（0=未能关联） |
| FileRecordID | uint | index | 关联公共文件索引ID（移动/重命名后身份关联） |
| ClientIP | string(45) | index | 客户端IP |
| UserID | string(64) | index | 用户ID |
| UploadType | TargetType | size:20, not null, default:"regular" | 上传类型 |
| Action | string(20) | default:"upload", index | 操作类型 (upload/download/download-by-hash/search/preview) |
| SearchQuery | string(255) | index | 搜索关键词 |
| UploadTime | time.Time | not null, index | 上传时间 |
| CreatedAt | time.Time | index | 创建时间 |
| DeletedAt | gorm.DeletedAt | index | 软删除 |
| DlEvent | int | default:0 | 下载统计签名：1=公开下载事件（action∈{download,download-by-hash} 且来源 public），写库时由 BeforeCreate 计算 |
| DlOk | int | default:0 | 下载事件成功标志：仅 DlEvent=1 有意义，1=成功，0=失败/异常 |
| DlCounted | int | default:0 | 下载计数签名：1=该记录已计入 `file_records_public.download_count`（公开来源、非预览、非断点续传/分段延续）。由各下载入口显式置位；有了它，下载次数可由记录集唯一推导，删除记录后按 `dl_counted` 重算即可精确回退 |

**索引**:
- `ClientIP`: IP 统计
- `UserID`: 用户统计
- `SearchQuery`: 搜索统计
- `DeletedAt`: 软删除查询
- `idx__operation_records__action_created_at`: `action, created_at`（最近/热门列表）；历史冗余索引 `idx_operation_records_action_created` 已被 `CleanupOperationRecordIndexes` 幂等清理（列与之完全一致，仅存在于手动执行过 005.sql 的存量库）
- `idx__operation_records__dl_event_created_at_dl_ok`: `dl_event, created_at, dl_ok`（行为分析下载行为覆盖索引，按 dl_event 等值 + created_at 区间过滤）
- `idx__operation_records__dl_event_id`: `dl_event, id`（分析缓存水位判定）
- `idx__operation_records__dl_counted_full_path`: `dl_counted, full_path`（下载次数重算：按 dl_counted 等值 + full_path 计数，删除记录后回退下载次数；同时覆盖「按 dl_counted 分组统计各路径计数」的全量对齐扫描）

---

## URLDownloadTask URL 上传任务模型

- **表名**: `url_download_tasks`
- **说明**: URL 上传任务（后台持久化队列；`POST /api/v1/uploads/url` 入队后由全局调度器执行，进程重启后自动恢复）

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | string(36) | primaryKey | 任务ID（UUID） |
| UserID | *string(64) | index | 登录用户ID（AuthOptional 下已登录时落库） |
| AnonymousID | *string(36) | index | 匿名用户标识（未登录时落库） |
| URL | string(2048) | not null | 源 URL（http/https/ftp/ftps） |
| FileName | string(255) | not null | 文件名 |
| TargetPath | string(512) | - | 目标文件绝对路径（temp 完成时回写为哈希命名后的真实路径） |
| StorageType | URLDownloadStorageType | size:20, not null, default: "temp" | 存储类型：temp/private/regular |
| RootName | string(64) | - | 目标根目录名（供重试还原会话上下文） |
| Status | URLDownloadStatus | size:20, not null, default: "pending" | 任务状态 |
| ErrorMessage | string(1024) | - | 错误信息 |
| FileSize | int64 | default: 0 | 文件大小 |
| DownloadedBytes | int64 | default: 0 | 已下载字节数（断点续传依据） |
| ETag | string(255) | - | 远端 ETag（断点续传校验） |
| ResumeSupported | bool | default: false | 是否支持断点续传 |
| ResumeURL | string(2048) | - | 续传使用的 URL |
| TempFilePath | string(512) | - | 下载中的临时文件路径 |
| ClientIP | string(64) | - | 入队请求方 IP（worker 还原归属与操作记录） |
| SessionDir | string(512) | - | 目标目录（会话语义，相对根目录或临时目录） |
| RetryCount | int | default: 0 | 已自动重试次数（不含首次尝试） |
| QueuedAt | time.Time | index | 入队时间，用于调度排序；重试时刷新以排到队尾 |
| Notified | bool | default: false, index | 是否已发送完成/失败通知 |
| CreatedAt | time.Time | - | 创建时间 |
| CompletedAt | *time.Time | - | 完成时间 |
| UpdatedAt | time.Time | - | 更新时间 |

> 注：任务为**硬删除**（无 `DeletedAt` 软删除字段），删除即移除记录。

### URLDownloadStatus 枚举

| 值 | 说明 |
|----|------|
| pending | 等待下载 |
| downloading | 下载中 |
| completed | 已完成 |
| failed | 失败 |
| cancelled | 已取消 |

### URLDownloadStorageType 枚举

| 值 | 说明 |
|----|------|
| temp | 临时文件 |
| private | 私有存储 |
| regular | 普通文件 |

---

## Notification 通知模型

- **表名**: `notifications`
- **说明**: URL 下载任务通知

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| UserID | string(64) | index | 用户ID |
| AnonymousID | string(64) | index | 匿名用户标识 |
| Title | string(255) | - | 通知标题 |
| Message | string(1024) | - | 通知内容 |
| TaskID | uint | index | 关联任务ID |
| IsRead | bool | default: false | 是否已读 |
| NotificationType | string(50) | - | 通知类型 |
| CreatedAt | time.Time | - | 创建时间 |

---

## SecurityEvent 安全事件模型

- **表名**: `security_events`
- **说明**: 记录被识别为攻击 / 越权 / 异常请求的风险事件，支撑管理员安全分析与处置；由专门的安全事件处理器（`internal/security`）经有界异步写入器落库，避免攻击场景下同步写库拖慢被拒请求的响应。

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| EventType | string(64) | not null | 事件类型，见 `Event*` 常量 |
| Severity | string(16) | not null; default: low | 严重级别 `high` / `medium` / `low` |
| ClientIP | string(45) | index | 源 IP |
| Identity | string(64) | - | 操作者身份（用户名 或 anonymous） |
| Operation | string(64) | - | 操作名称（如 `auth.login`、`file.download`） |
| Target | string(512) | - | 操作对象（路径 / 分享码 / 用户名） |
| HTTPMethod | string(16) | - | HTTP 方法 |
| Path | string(1024) | - | 请求路径 + 查询串 |
| UserAgent | string(512) | - | 客户端 UA |
| StatusCode | int | default: 0 | HTTP 状态码 |
| Result | string(16) | not null; default: failure | 结果 `success` / `failure` |
| Reason | string(512) | - | 事件详情 / 错误描述 |
| RequestID | string(64) | index | 用于关联同一波攻击的请求 ID |
| Guard | string(64) | - | 命中的防护机制（auth / accessguard / webdav / ftp） |
| Read | bool | not null; default: false; index | 管理员是否已读（评估未读提醒） |
| CreatedAt | time.Time | index | 事件产生时间（UTC） |

### Severity 严重级别

| 取值 | 含义 |
|------|------|
| `high` | 高危：需管理员关注（暴破锁定、路径穿越、越权绕过等） |
| `medium` | 中危：可疑但不一定恶意（单次 401 / 403 / 登录失败） |
| `low` | 低危：常规限流触发等 |

### EventType 事件类型

| 取值 | 含义 |
|------|------|
| `auth.failed_login` | 登录 / 注册失败 |
| `auth.locked` | 登录 / 注册失败达阈值被锁定（暴破缓解） |
| `auth.unauthorized` | 认证失败（401） |
| `access.forbidden` | 越权 / 禁止访问（403） |
| `rate.limited` | 触发频率限制（429） |
| `code.bruteforce` | 无效分享 / 临时码 / 口令枚举达阈值被锁定 |

### 索引

- `idx_security_events_created_at`: `created_at`（按时间倒序分页）
- `idx_security_events_client_ip`: `client_ip`（TOP IP 分析）
- `idx_security_events_request_id`: `request_id`（同一波攻击关联）
- `idx_security_events_read`: `read`（未读过滤 / 未读计数）

---

## 关联关系图

```
┌─────────────────────┐     1:N      ┌─────────────────────┐
│   UploadSession     │──────────────│   UploadedChunk     │
│  (upload_sessions)  │              │  (uploaded_chunks)  │
└─────────────────────┘              └─────────────────────┘
        │
        │ SessionID 关联
        ▼

┌─────────────────────┐              ┌─────────────────────┐
│  URLDownloadTask    │──── 1:1 ─────│   Notification      │
│ (url_download_tasks)│              │  (notifications)    │
└─────────────────────┘              └─────────────────────┘
        │
        │ TaskID 关联
        ▼

独立表（无外键关联）:
┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────────┐
│    User     │  │  TempFile   │  │ Operation   │  │ SecurityEvent │
│  (users)    │  │ (temp_files)│  │ (op_records)│  │(security_events)│
└─────────────┘  └─────────────┘  └─────────────┘  └───────────────┘
```

---

## 数据库支持

| 数据库 | 驱动 | 说明 |
|--------|------|------|
| SQLite | `github.com/glebarez/sqlite` | 默认，开发和小规模部署 |
| MySQL | `gorm.io/driver/mysql` | 生产环境推荐 |
| PostgreSQL | `gorm.io/driver/postgres` | 生产环境可选 |

GORM 自动处理数据库适配，所有模型定义兼容以上三种数据库。

> 注意：本程序不内置跨数据库引擎的数据迁移能力。需在引擎之间迁移时请使用外部工具（如 [dbswitch](https://github.com/light-art/dbswitch)）。

---

## 自动迁移

在 `main.go` 中调用:

```go
db.AutoMigrate(
    &models.UploadSession{},
    &models.UploadedChunk{},
    &models.OperationRecord{},
    &models.User{},
    &models.TempFile{},
    &models.URLDownloadTask{},
    &models.SecurityEvent{},
)

// 运行时创建临时文件分片表
tempHandler.InitTempSessionTable()
```
