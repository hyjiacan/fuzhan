# 数据模型文档

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-02

---

## 目录

- [User 用户模型](#user-用户模型)
- [TempFile 临时文件模型](#tempfile-临时文件模型)
- [UploadSession 上传会话模型](#uploadsession-上传会话模型)
- [UploadedChunk 已上传分片模型](#uploadedchunk-已上传分片模型)
- [UploadRecord 上传记录模型](#uploadrecord-上传记录模型)
- [URLDownloadTask URL下载任务模型](#urldownloadtask-url下载任务模型)
- [Notification 通知模型](#notification-通知模型)
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

## UploadRecord 上传记录模型

- **表名**: `upload_records`
- **说明**: 上传/下载/搜索操作记录

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| FileName | string(255) | not null | 文件名 |
| FileSize | int64 | not null | 文件大小 |
| FilePath | string(512) | not null | 存储路径 |
| RootName | string(64) | not null | 根目录名称 |
| FileType | string(50) | - | 文件类型 |
| ClientIP | string(45) | index | 客户端IP |
| UserID | string(64) | index | 用户ID |
| UploadType | TargetType | size:20, not null, default: "regular" | 上传类型 |
| Action | string(20) | default: "upload", index | 操作类型 (upload/download/search) |
| SearchQuery | string(255) | index | 搜索关键词 |
| UploadTime | time.Time | not null, index | 操作时间 |
| CreatedAt | time.Time | index | 创建时间 |
| DeletedAt | gorm.DeletedAt | index | 软删除 |

**索引**:
- `UploadTime`: 按时间排序
- `Action`: 操作类型过滤
- `ClientIP`: IP 统计
- `UserID`: 用户统计
- `SearchQuery`: 搜索统计
- `DeletedAt`: 软删除查询

---

## URLDownloadTask URL下载任务模型

- **表名**: `url_download_tasks`
- **说明**: URL 下载任务

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| ID | uint | primaryKey | 自增主键 |
| URL | string(2048) | not null | 下载URL |
| Status | TaskStatus | size:20, not null, default: "pending" | 任务状态 |
| FileName | string(255) | - | 文件名 |
| FileSize | int64 | - | 文件大小 |
| DownloadedSize | int64 | default: 0 | 已下载大小 |
| StorageType | TargetType | size:20, not null, default: "regular" | 存储类型 |
| TargetPath | string(512) | - | 目标路径 |
| TargetRoot | string(255) | - | 目标根目录 |
| ResumeSupported | bool | default: false | 是否支持断点续传 |
| UserID | string(64) | index | 用户ID |
| AnonymousID | string(64) | index | 匿名用户标识 |
| ErrorMessage | string(1024) | - | 错误信息 |
| CreatedAt | time.Time | - | 创建时间 |
| UpdatedAt | time.Time | - | 更新时间 |
| DeletedAt | gorm.DeletedAt | index | 软删除 |

### TaskStatus 枚举

| 值 | 说明 |
|----|------|
| pending | 等待下载 |
| downloading | 下载中 |
| completed | 已完成 |
| failed | 失败 |
| cancelled | 已取消 |

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
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│    User     │  │  TempFile   │  │UploadRecord │
│  (users)    │  │ (temp_files)│  │(upload_rec) │
└─────────────┘  └─────────────┘  └─────────────┘
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
    &models.UploadRecord{},
    &models.User{},
    &models.TempFile{},
    &models.URLDownloadTask{},
)

// 运行时创建临时文件分片表
tempHandler.InitTempSessionTable()
```
