# 浮栈 API 接口文档

> 更新时间: 2026-07-22

## 概述

本文档描述了浮栈系统的 API 接口，包括文件操作、上传下载、搜索、临时文件管理、认证（JWT/API Key/LDAP/OAuth2）等功能。所有接口均通过 HTTP/HTTPS 协议访问，使用 JSON 格式进行数据交换。

## 基础信息

- **协议**：HTTP/HTTPS
- **数据格式**：JSON
- **字符编码**：UTF-8
- **认证方式**：
  - JWT Bearer Token（部分接口需要）
  - API Key（外部集成）
  - LDAP（企业目录）
  - OAuth2 / OIDC（外部统一认证，授权码模式）
  - **强制登录（LoginGate）**：开启 `account.require_login` 后，`/api/v1/web`（含 `/temp/**`）、`/api/v1/cli`、`/download`、`/lite`、`/share/:code` 等接口需携带登录令牌（`Authorization: Bearer <token>` 或 HttpOnly Cookie `fuzhan_token`，不支持 `?token=`）；下载/预览使用短时效签名票据（URL 上 `t=`，与文件绑定、按时限生效）。未开启时保持匿名。登录/注册、初始化 `/setup/`、健康检查、`/options`、CLI 安装脚本为匿名例外
- **响应格式**：
  ```json
  {
    "success": true,
    "message": "消息",
    "data": {}
  }
  ```

## 认证接口

### 1. 用户注册

- **URL**：`POST /api/v1/web/auth/register`
- **认证**：无需认证
- **请求体**：
  ```json
  {
    "username": "用户名 (3-32字符)",
    "password": "密码 (6-128字符)"
  }
  ```
- **响应**：
  ```json
  {
    "success": true,
    "message": "注册成功",
    "data": {
      "uuid": "用户UUID",
      "username": "用户名",
      "expiresIn": 86400
    }
  }
  ```
- **说明**：登录凭证只通过 HttpOnly Cookie（`fuzhan_token`）下发，响应体不回传 JWT；
  CLI 等客户端同样使用 Cookie 会话（见「命令行接口」章节）。

### 2. 用户登录

- **URL**：`POST /api/v1/web/auth/login`
- **认证**：无需认证
- **请求体**：
  ```json
  {
    "username": "用户名",
    "password": "密码"
  }
  ```
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "uuid": "用户UUID",
      "username": "用户名",
      "expiresIn": 86400
    }
  }
  ```
- **说明**：凭证只通过 HttpOnly Cookie（`fuzhan_token`）下发，响应体不回传 JWT；
  CLI 等客户端同样使用 Cookie 会话，不再从响应体取令牌。

### 3. 获取用户信息

- **URL**：`GET /api/v1/web/auth/user`
- **认证**：需要（Bearer Token）
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "uuid": "用户UUID",
      "username": "admin",
      "role": "admin",
      "disabled": false
    }
  }
  ```

### 4. 刷新 Token

- **URL**：`POST /api/v1/web/auth/refresh`
- **认证**：需要（HttpOnly Cookie 或 Bearer Token）
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "uuid": "用户UUID",
      "username": "用户名",
      "expiresIn": 86400
    }
  }
  ```
- **说明**：刷新后的令牌直接写回 HttpOnly Cookie，不经响应体或响应头回传。

### 5. 修改密码

- **URL**：`POST /api/v1/web/auth/change-password`
- **认证**：需要（Bearer Token）
- **请求体**：
  ```json
  {
    "oldPassword": "旧密码",
    "newPassword": "新密码 (6-128字符)"
  }
  ```

### 6. 外部 OAuth2 / OIDC 认证

用于对接外部统一认证系统（授权码模式）。三个接口均无需认证。

- **查询状态**：`GET /api/v1/web/auth/oauth2/status`
  - 返回是否启用，供登录页决定是否展示外部认证入口（不暴露提供方等实现细节）。
  ```json
  { "success": true, "data": { "enabled": true } }
  ```

- **发起授权**：`GET /api/v1/web/auth/oauth2/authorize`
  - 返回外部认证系统的授权地址与签名 state，由前端整页跳转。
  ```json
  { "success": true, "data": { "authorizeUrl": "https://sso.example.com/oauth2/authorize?...", "state": "..." } }
  ```

- **回调处理**：`GET /api/v1/web/auth/oauth2/callback`
  - 由外部系统回调，后端校验 state、换取令牌、映射本地用户并签发 JWT，
    令牌直接写入 HttpOnly Cookie，随后 302 跳转到 `/#/oauth2/callback`（URL 不携带令牌）；
    失败时重定向为 `/#/oauth2/callback?error=...`。

## 文件浏览接口

### 1. 获取目录列表

- **URL**：`GET /api/v1/web/files/list`
- **认证**：受强制登录开关控制（未开启时匿名可访问；开启后需携带登录令牌）
- **参数**：
  - `path`（可选）：目录路径，格式 `rootName/subdir`
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "currentPath": "rootName/subdir",
      "items": [
        {
          "name": "目录名",
          "type": "directory",
          "path": "rootName/subdir/目录名",
          "modifiedTime": "2026-07-22T10:00:00Z",
          "quota": 1073741824,
          "used": 536870912
        },
        {
          "name": "文件名.txt",
          "type": "file",
          "path": "rootName/subdir/文件名.txt",
          "modifiedTime": "2026-07-22T10:00:00Z",
          "size": 1024
        }
      ]
    }
  }
  ```

### 2. 获取最近上传

- **URL**：`GET /api/v1/web/files/recent`
- **认证**：受强制登录开关控制（未开启时匿名可访问；开启后需携带登录令牌）
- **参数**：
  - `limit`（可选）：返回数量，默认 50
- **响应**：
  ```json
  {
    "success": true,
    "data": [
      {
        "filename": "文件名",
        "path": "rootName/文件名",
        "uploadTime": "2026-07-22T10:00:00Z",
        "size": 1024
      }
    ]
  }
  ```

### 3. 文件预览

- **URL**：`GET /api/v1/web/files/preview/*path`
- **认证**：受强制登录开关控制（未开启时匿名可访问；开启后需携带登录令牌）
- **说明**：直接返回文件内容，支持文本、图片、PDF

### 4. 文本分片预览

- **URL**：`GET /api/v1/web/files/preview-chunk`
- **认证**：受强制登录开关控制（未开启时匿名可访问；开启后需携带登录令牌）
- **参数**：
  - `path`：文件路径
  - `chunk`：分片编号（从 0 开始）
  - `size`：分片大小（字节）

## 文件下载接口

### 1. 下载文件

- **URL**：`GET /download/*path`（等价 API 路径：`GET /api/v1/web/download/*path`）
- **认证**：受强制登录开关控制（未开启时匿名可访问；开启后需携带登录令牌或短时效下载票据 `t=`，不支持 `?token=`）。匿名下载按 IP 限流
- **参数**：
  - `preview`（可选）：预览模式
- **说明**：直接返回文件内容
- **断点续传**：支持 HTTP Range 请求，响应恒带 `Accept-Ranges: bytes`；带合法 `Range` 时返回 `206` 与 `Content-Range`，区间不可满足时返回 `416`

### 2. 分享下载（私有存储）

- **URL**：`GET /share/:code`
- **认证**：无需认证（匿名例外，即使在强制登录模式下仍可直接访问，便于外部分享）
- **参数**：
  - `preview`（可选）：设为 `true` 时以内联方式返回（不设置 `Content-Disposition`，按扩展名推断 `Content-Type`），供图片/PDF/文本在线预览；不写入下载记录
- **说明**：通过分享码下载私有文件
- **断点续传**：支持 HTTP Range 请求，响应恒带 `Accept-Ranges: bytes`；带合法 `Range` 时返回 `206` 与 `Content-Range`，区间不可满足时返回 `416`
- **下载记录**：仅新下载（无 `Range` 或 `Range` 起始为 0 的 GET）写入成功下载记录；续传分段、尾部区间、HEAD 探活与预览请求不记录，避免一次下载被拆成多条记录

## 文件搜索接口

### 1. 搜索文件

- **URL**：`GET /api/v1/web/search/*query`
- **认证**：无需认证
- **说明**：使用 SSE 实时返回搜索结果
- **响应**（SSE 格式）：
  ```
  event: result
  data: {"name":"file.txt","path":"root/file.txt","type":"file","size":1024,"modifiedTime":"..."}

  event: result
  data: {"name":"file2.txt","path":"root2/file2.txt","type":"file","size":2048,"modifiedTime":"..."}

  event: message
  data: {"success":true,"message":"搜索完成，共找到 2 个结果","data":[]}

  event: done
  data: {"total":2,"matched":2}
  ```

### 2. 搜索自动补全（Bluge 联想）

- **URL**：`GET /api/v1/web/search-suggest`
- **认证**：无需认证
- **说明**：基于文件名检索索引（Bluge + gse）的关键词联想，返回最常见的关键词 term（英文小写、中文语义词），而非完整文件名
- **请求参数**：
  | 参数 | 类型 | 必填 | 说明 |
  |------|------|------|------|
  | q | string | 是 | 搜索前缀（中文需 URL 编码） |
  | limit | int | 否 | 返回数量上限，默认 10，最大 30 |
- **响应**：
  ```json
  {"success":true,"code":0,"message":"","data":["firefox","setup"]}
  ```

### 3. 搜索拼写纠错

- **URL**：`GET /api/v1/web/search-spellcheck`
- **认证**：无需认证
- **说明**：基于文件名检索索引（Bluge Fuzzy 查询）的拼写纠错，返回编辑距离内最相似的关键词候选（非完整文件名）
- **请求参数**：
  | 参数 | 类型 | 必填 | 说明 |
  |------|------|------|------|
  | q | string | 是 | 待纠错单词（中文需 URL 编码） |
  | limit | int | 否 | 返回数量上限，默认 5，最大 10 |
- **响应**：
  ```json
  {"success":true,"code":0,"message":"","data":["firefox"]}
  ```

### 4. 搜索相关词推荐

- **URL**：`GET /api/v1/web/search-related`
- **认证**：无需认证
- **说明**：返回与已输入各段（空格分隔）**同文件共现**的高频关键词，供搜索框「输入空格后」给出下一段候选。只做共现、不要求词序相邻；已输入段本身及其补全不计入候选
- **请求参数**：
  | 参数 | 类型 | 必填 | 说明 |
  |------|------|------|------|
  | q | string | 是 | 已输入的各段（空格分隔，中文需 URL 编码） |
  | limit | int | 否 | 返回数量上限，默认 10，最大 30 |
- **响应**：
  ```json
  {"success":true,"code":0,"message":"","data":["财务报表","pdf"]}
  ```
- **备注**：单次请求最多统计 200 个命中文档；无命中或 `q` 为空时返回空数组

## 文件操作接口

### 1. 文件重命名

- **URL**：`POST /api/v1/web/files/rename`
- **认证**：需要（JWT）
- **请求体**：
  ```json
  {
    "path": "rootName/原文件.txt",
    "newName": "新文件名.txt"
  }
  ```

### 2. 文件移动

- **URL**：`POST /api/v1/web/files/move`
- **认证**：需要（JWT）
- **请求体**：
  ```json
  {
    "path": "rootName/源文件.txt",
    "newPath": "rootName/subdir/目标位置.txt"
  }
  ```

### 3. 删除文件

- **URL**：`DELETE /api/v1/web/files`
- **认证**：需要（JWT）
- **参数**：
  - `path`：文件路径

## 分片上传接口

### 1. 创建上传会话

- **URL**：`POST /api/v1/web/uploads/session`
- **认证**：无需认证（支持断点续传）
- **请求体**：
  ```json
  {
    "filename": "文件名.zip",
    "fileSize": 104857600,
    "rootName": "share1",
    "dir": "目标目录",
    "targetType": "regular"
  }
  ```
- **响应**：
  ```json
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

### 2. 获取上传会话状态

- **URL**：`GET /api/v1/web/uploads/session/:id`
- **认证**：无需认证
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "uploadId": 1,
      "filename": "文件名.zip",
      "uploadedChunks": [0, 1, 2],
      "completed": false
    }
  }
  ```

### 3. 恢复上传会话

- **URL**：`POST /api/v1/web/uploads/session/:id/resume`
- **认证**：无需认证

### 4. 取消上传会话

- **URL**：`DELETE /api/v1/web/uploads/session/:id`
- **认证**：无需认证

### 5. 上传分片

- **URL**：`POST /api/v1/web/uploads/chunk`
- **认证**：无需认证
- **Content-Type**：`multipart/form-data`
- **参数**：
  - `file`：分片数据
  - `uploadId`：会话ID
  - `chunkIndex`：分片索引
  - `checksum`：校验值（xxh3）
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "chunkIndex": 0,
      "offset": 0
    }
  }
  ```

### 6. 完成上传

- **URL**：`POST /api/v1/web/uploads/finalize`
- **认证**：无需认证
- **请求体**：
  ```json
  {
    "uploadId": 1
  }
  ```

### 7. 从 URL 上传

- **URL**：`POST /api/v1/web/uploads/url`
- **认证**：可选（AuthOptional）
- **说明**：仅做校验与入队，返回 `taskId`；实际传输由后台全局队列按 `upload.url_upload.max_concurrent`（所有用户共享）调度，关闭浏览器/进程重启后继续执行。失败且错误可重试（超时/5xx/408/429）时按 `upload.url_upload.max_retries`（默认 3）自动重试并排到队尾，用尽后标记失败；4xx 不重试。服务器侧大小硬上限由 `upload.url_upload.max_size` 控制（字节，0=不限，下载实际累计写入超过即中断并清理）。
- **请求体**：
  ```json
  {
    "url": "https://example.com/file.zip",
    "filename": "文件名.zip",
    "rootName": "share1",
    "dir": "目标目录",
    "storageType": "regular"
  }
  ```
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "taskId": "uuid",
      "taskType": "url",
      "fileName": "文件名.zip",
      "fileSize": 1024000,
      "status": "pending"
    }
  }
  ```

### 8. 批量探测 URL

- **URL**：`POST /api/v1/web/uploads/url-probe`
- **认证**：可选（AuthOptional）
- **请求体**：`{ "urls": ["https://..."], "dir": "", "rootName": "", "storageType": "" }`
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "items": [
        {
          "url": "https://example.com/file.zip",
          "fileName": "file.zip",
          "fileSize": 1024000,
          "targetExists": false,
          "alreadyUploaded": false
        }
      ]
    }
  }
  ```

### 9. URL 上传任务进度

- **URL**：`GET /api/v1/web/uploads/url-task/:taskId`
- **认证**：可选（AuthOptional，仅任务归属者可用）
- **说明**：返回任务状态（`pending`/`downloading`/`completed`/`failed`/`cancelled`）、`downloadedBytes`、`fileSize`、`retryCount`（已自动重试次数）等，供前端轮询进度。
- **错误**：任务不存在返回 404；非任务归属者返回 403。

### 10. 计算 URL 上传任务校验值

- **URL**：`POST /api/v1/web/uploads/url-tasks/:taskId/checksum`
- **认证**：可选（AuthOptional，仅任务归属者可用）
- **说明**：为已完成（`completed`）的 URL 上传任务计算校验值，供用户执行完整性校验。服务端对已落盘文件单次流式读取，按所选算法并行计算；结果仅返回前端，**不落库**。
- **请求体**：
  ```json
  {
    "algorithms": ["md5", "sha1", "sha256"]
  }
  ```
  `algorithms` 为 `md5`/`sha1`/`sha256` 的任意非空子集（可多选），重复项自动去重，未知算法返回 400。
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "checksums": {
        "md5": "d41d8cd98f00b204e9800998ecf8427e",
        "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
      }
    }
  }
  ```
- **错误**：任务不存在返回 404；无权操作返回 403；任务未完成或缺少目标文件路径返回 400；目标文件不存在返回 404。

## 临时文件接口

### 1. 获取临时文件列表

- **URL**：`GET /api/v1/web/temp/list`
- **认证**：基于IP，无需用户认证
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "files": [
        {
          "filename": "文件名",
          "uploadTime": "2026-07-22T10:00:00Z",
          "fileSize": 1024,
          "code": "A1B2C3D4",
          "expiredAt": "2026-07-29T10:00:00Z"
        }
      ],
      "quota": {
        "global": 10737418240,
        "user": 1073741824
      },
      "used": 536870912
    }
  }
  ```

### 2. 上传临时文件

- **URL**：`POST /api/v1/web/temp/upload`
- **认证**：基于IP
- **Content-Type**：`multipart/form-data`
- **参数**：
  - `file`：上传的文件
  - `expireDate`（可选）：过期日期

### 3. 临时文件分片上传

| 操作 | URL | 说明 |
|------|-----|------|
| 创建会话 | `POST /api/v1/web/temp/upload/session` | - |
| 获取状态 | `GET /api/v1/web/temp/upload/session/:uploadId` | - |
| 上传分片 | `POST /api/v1/web/temp/upload/chunk` | - |
| 完成上传 | `POST /api/v1/web/temp/upload/finalize` | - |

### 4. 下载临时文件

- **URL**：`GET /api/v1/web/temp/:code/download`
- **认证**：无需认证
- **参数**：
  - `preview`（可选）：设为 `true` 时以内联方式返回（不设置 `Content-Disposition`，按扩展名推断 `Content-Type`），供图片/PDF/文本在线预览；不触发下载即删、不计入下载次数、不写下载记录

### 5. 获取临时文件信息

- **URL**：`GET /api/v1/web/temp/:code`
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "filename": "文件名.zip",
      "fileSize": 1024,
      "expiredAt": "2026-07-29T10:00:00Z"
    }
  }
  ```

### 6. 删除临时文件

- **URL**：`DELETE /api/v1/web/temp/:code`
- **认证**：基于IP

### 7. 获取临时文件配额

- **URL**：`GET /api/v1/web/temp/quota`

## 私有存储接口（需认证）

### 1. 获取私有文件列表

- **URL**：`GET /api/v1/web/private/files`
- **认证**：需要（JWT）
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "files": [...],
      "totalSize": 5368709120,
      "maxSize": 10737418240
    }
  }
  ```

### 2. 私有文件分片上传

私有文件仅支持分片上传（断点续传），不提供单次简单上传入口。

| 操作 | URL |
|------|-----|
| 创建会话 | `POST /api/v1/web/private/uploads/session` |
| 获取状态 | `GET /api/v1/web/private/uploads/session/:id` |
| 续传 | `POST /api/v1/web/private/uploads/session/:id/resume` |
| 取消 | `DELETE /api/v1/web/private/uploads/session/:id` |
| 上传分片 | `POST /api/v1/web/private/uploads/chunk` |
| 完成 | `POST /api/v1/web/private/uploads/finalize` |

### 3. 获取私有存储配额

- **URL**：`GET /api/v1/web/private/quota`
- **认证**：需要（JWT）

### 4. 删除私有文件

- **URL**：`DELETE /api/v1/web/private/files/:code`
- **认证**：需要（JWT）

## API Key 管理接口（需管理员权限）

### 1. 列出 API Keys

- **URL**：`GET /api/v1/web/api-keys`
- **认证**：需要（管理员）

### 2. 创建 API Key

- **URL**：`POST /api/v1/web/api-keys`
- **认证**：需要（管理员）
- **请求体**：
  ```json
  {
    "name": "应用名称",
    "expiresIn": 86400
  }
  ```

### 3. 删除 API Key

- **URL**：`DELETE /api/v1/web/api-keys/:id`
- **认证**：需要（管理员）

## 管理接口（需管理员权限）

### 1. 获取用户列表

- **URL**：`GET /api/v1/web/admin/users`

### 2. 重置用户密码

- **URL**：`PUT /api/v1/web/admin/users/:uuid/reset-password`
- **请求体**：
  ```json
  {
    "password": "新密码"
  }
  ```

### 3. 禁用/启用用户

- **URL**：`PUT /api/v1/web/admin/users/:uuid/disabled`
- **请求体**：
  ```json
  {
    "disabled": true
  }
  ```

### 4. 删除用户

- **URL**：`DELETE /api/v1/web/admin/users/:uuid`

### 5. 获取上传会话列表

- **URL**：`GET /api/v1/web/admin/sessions`

### 6. 清理过期会话

- **URL**：`POST /api/v1/web/admin/sessions/cleanup`

## 配置接口

### 1. 获取系统选项

- **URL**：`GET /api/v1/web/options`
- **认证**：无需认证（匿名例外，强制登录模式下也可匿名访问）
- **说明**：供前端加载时获取运行时配置（含 `account.requireLogin`，前端据此决定是否跳转登录页）
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "app": {
        "name": "浮栈"
      },
      "account": {
        "allowRegistration": false,
        "requireLogin": false
      },
      "temp": {
        "enabled": true
      },
      "tempFilesEnabled": true,
      "chunkSize": 10485760,
      "maxFileSize": 17179869184,
      "preview": {
        "enabled": true,
        "groups": {
          "image": { "enabled": true, "mimes": "image/png,image/jpeg,...", "exts": "png,jpg,jpeg,gif,bmp,webp,svg,ico" },
          "text": { "enabled": true, "mimes": "text/*,application/json,...", "exts": "txt,html,css,..." },
          "audio": { "enabled": true, "mimes": "audio/mpeg,audio/wav,...", "exts": "mp3,wav,..." },
          "video": { "enabled": true, "mimes": "video/mp4,video/webm,video/ogg", "exts": "mp4,webm,..." },
          "document": { "enabled": true, "mimes": "application/pdf", "exts": "pdf" },
          "custom": { "enabled": false, "mimes": "", "exts": "" }
        },
        "defaults": {
          "image": { "enabled": true, "mimes": "image/png,...", "exts": "png,jpg,..." },
          "text": { "enabled": true, "mimes": "text/*,...", "exts": "txt,md,..." },
          "audio": { "enabled": true, "mimes": "audio/mpeg,...", "exts": "mp3,wav,..." },
          "video": { "enabled": true, "mimes": "video/mp4,...", "exts": "mp4,webm,..." },
          "document": { "enabled": true, "mimes": "application/pdf", "exts": "pdf" },
          "custom": { "enabled": false, "mimes": "", "exts": "" }
        }
      }
    }
  }
  ```

### 2. 获取完整配置（需管理员）

- **URL**：`GET /api/v1/web/config`

### 3. 保存配置（需管理员）

- **URL**：`POST /api/v1/web/config`

### 4. 重新加载配置（需管理员）

- **URL**：`POST /api/v1/web/config/reload`

### 5. 初始化状态

- **URL**：`GET /api/v1/web/setup/status`
- **认证**：无需认证

### 6. 保存初始配置

- **URL**：`POST /api/v1/web/setup/save`
- **认证**：无需认证（首次配置）

### 7. 验证目录

- **URL**：`POST /api/v1/web/setup/validate-dir`
- **认证**：无需认证
- **请求体**：
  ```json
  {
    "path": "/path/to/directory"
  }
  ```

### 8. 获取网络接口

- **URL**：`GET /api/v1/web/setup/network/interfaces`

### 9. 获取默认配置

- **URL**：`GET /api/v1/web/setup/default-config`

## 健康检查

### 1. 健康状态

- **URL**：`GET /api/v1/web/health`
- **认证**：无需认证
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "status": "healthy"
    }
  }
  ```

### 2. 就绪检查

- **URL**：`GET /api/v1/web/ready`
- **认证**：强制登录开启后需登录（否则 401）

### 3. Prometheus 指标

- **URL**：`GET /api/v1/metrics`
- **认证**：无需认证

## CLI 接口（受强制登录开关控制）

CLI 接口的认证行为与配置文件 `account.require_login` 关联：未开启时匿名可访问；开启后需携带登录凭证（HttpOnly Cookie `fuzhan_token` 或 `Authorization: Bearer <token>`，不支持 `?token=`），未认证返回 `401`。CLI 可用 `fuzhan login <用户名> <密码>` 登录并把会话 Cookie 保存到 `~/.fuzhan_cookie`（可用环境变量 `FUZHAN_COOKIE_FILE` 覆盖），后续请求自动携带。CLI 安装/升级脚本 `install.sh`、`fuzhan.sh` 为匿名例外（保持公开，便于新机器拉取脚本）。

### 1. CLI 帮助

- **URL**：`GET /api/v1/cli`（别名 `GET /cli`）
- **认证**：见上方说明

### 2. CLI 搜索

- **URL**：`GET /api/v1/cli/search/*query`（别名 `GET /cli/search/*query`）
- **认证**：见上方说明

### 3. CLI 列出目录

- **URL**：`GET /api/v1/cli/list/*path`（别名 `GET /cli/list/*path`）
- **认证**：见上方说明

## 错误处理

所有 API 接口都遵循统一的错误响应格式：

```json
{
  "success": false,
  "message": "错误描述信息"
}
```

常见 HTTP 状态码：
- `200 OK`：请求成功
- `400 Bad Request`：请求参数错误
- `401 Unauthorized`：未授权访问
- `403 Forbidden`：禁止访问
- `404 Not Found`：资源不存在
- `409 Conflict`：资源冲突
- `500 Internal Server Error`：服务器内部错误

## 安全考虑

1. **路径遍历防护**：所有文件操作验证路径范围
2. **文件类型过滤**：限制允许上传的文件类型
3. **用户标识验证**：
   - 临时文件基于 IP
   - 私有存储基于 JWT 或 API Key
4. **JWT/API Key 认证**：私有存储需要有效令牌
5. **配额限制**：防止存储空间被滥用
6. **速率限制**：可配置的请求频率限制
7. **SSRF 防护**：URL 下载时验证源 IP 地址

## 前端组件 - UploadManager

UploadManager 组件封装了分片上传功能，支持断点续传。

### uploadApi 属性

组件通过 `upload-api` prop 接收上传 API 配置：

```javascript
const uploadApi = {
  type: 'chunked',  // 固定值
  createSession: () => '/api/v1/web/uploads/session',
  getSession: (uploadId) => `/api/v1/web/uploads/session/${uploadId}`,
  uploadChunk: () => '/api/v1/web/uploads/chunk',
  finalize: () => '/api/v1/web/uploads/finalize'
}
```

### 各模块的 API 配置

| 模块 | createSession | getSession | uploadChunk | finalize |
|------|---------------|------------|-------------|----------|
| 公开文件 | `/api/v1/web/uploads/session` | `/api/v1/web/uploads/session/{id}` | `/api/v1/web/uploads/chunk` | `/api/v1/web/uploads/finalize` |
| 私有存储 | `/api/v1/web/private/uploads/session` | `/api/v1/web/private/uploads/session/{id}` | `/api/v1/web/private/uploads/chunk` | `/api/v1/web/private/uploads/finalize` |
| 临时文件 | `/api/v1/web/temp/upload/session` | `/api/v1/web/temp/upload/session/{id}` | `/api/v1/web/temp/upload/chunk` | `/api/v1/web/temp/upload/finalize` |

### 使用示例

```vue
<template>
  <upload-manager
    :upload-api="uploadApi"
    @upload-success="onSuccess"
    @upload-error="onError"
  />
</template>

<script setup>
const uploadApi = {
  type: 'chunked',
  createSession: () => '/api/v1/web/uploads/session',
  getSession: (id) => `/api/v1/web/uploads/session/${id}`,
  uploadChunk: () => '/api/v1/web/uploads/chunk',
  finalize: () => '/api/v1/web/uploads/finalize'
}

const onSuccess = () => {
  console.log('上传成功')
}

const onError = (error) => {
  console.error('上传失败:', error)
}
</script>
```
