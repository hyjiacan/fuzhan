# 轻共享 API 接口文档

> 更新时间: 2026-07-22

## 概述

本文档描述了轻共享系统的 API 接口，包括文件操作、上传下载、搜索、临时文件管理、认证（JWT/API Key/LDAP）等功能。所有接口均通过 HTTP/HTTPS 协议访问，使用 JSON 格式进行数据交换。

## 基础信息

- **协议**：HTTP/HTTPS
- **数据格式**：JSON
- **字符编码**：UTF-8
- **认证方式**：
  - JWT Bearer Token（部分接口需要）
  - API Key（外部集成）
  - LDAP（企业目录）
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

- **URL**：`POST /api/v1/auth/register`
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
      "token": "JWT令牌",
      "uuid": "用户UUID",
      "username": "用户名",
      "expiresIn": 86400
    }
  }
  ```

### 2. 用户登录

- **URL**：`POST /api/v1/auth/login`
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
      "token": "JWT令牌",
      "uuid": "用户UUID",
      "username": "用户名",
      "expiresIn": 86400
    }
  }
  ```

### 3. 获取用户信息

- **URL**：`GET /api/v1/auth/user`
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

- **URL**：`POST /api/v1/auth/refresh`
- **认证**：需要（Bearer Token）
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "token": "新JWT令牌",
      "expiresIn": 86400
    }
  }
  ```

### 5. 修改密码

- **URL**：`POST /api/v1/auth/change-password`
- **认证**：需要（Bearer Token）
- **请求体**：
  ```json
  {
    "oldPassword": "旧密码",
    "newPassword": "新密码 (6-128字符)"
  }
  ```

## 文件浏览接口

### 1. 获取目录列表

- **URL**：`GET /api/v1/files/list`
- **认证**：无需认证
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

- **URL**：`GET /api/v1/files/recent`
- **认证**：无需认证
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

- **URL**：`GET /api/v1/files/preview/*path`
- **认证**：无需认证
- **说明**：直接返回文件内容，支持文本、图片、PDF

### 4. 文本分片预览

- **URL**：`GET /api/v1/files/preview-chunk`
- **认证**：无需认证
- **参数**：
  - `path`：文件路径
  - `chunk`：分片编号（从 0 开始）
  - `size`：分片大小（字节）

## 文件下载接口

### 1. 下载文件

- **URL**：`GET /api/v1/download/*path`
- **认证**：无需认证
- **参数**：
  - `preview`（可选）：预览模式
- **说明**：直接返回文件内容

### 2. 分享下载（私有存储）

- **URL**：`GET /share/:code`
- **认证**：无需认证
- **说明**：通过分享码下载私有文件

## 文件搜索接口

### 1. 搜索文件

- **URL**：`GET /api/v1/search/*query`
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

## 文件操作接口

### 1. 文件重命名

- **URL**：`POST /api/v1/files/rename`
- **认证**：需要（JWT）
- **请求体**：
  ```json
  {
    "path": "rootName/原文件.txt",
    "newName": "新文件名.txt"
  }
  ```

### 2. 文件移动

- **URL**：`POST /api/v1/files/move`
- **认证**：需要（JWT）
- **请求体**：
  ```json
  {
    "path": "rootName/源文件.txt",
    "newPath": "rootName/subdir/目标位置.txt"
  }
  ```

### 3. 删除文件

- **URL**：`DELETE /api/v1/files`
- **认证**：需要（JWT）
- **参数**：
  - `path`：文件路径

## 分片上传接口

### 1. 创建上传会话

- **URL**：`POST /api/v1/uploads/session`
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

- **URL**：`GET /api/v1/uploads/session/:id`
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

- **URL**：`POST /api/v1/uploads/session/:id/resume`
- **认证**：无需认证

### 4. 取消上传会话

- **URL**：`DELETE /api/v1/uploads/session/:id`
- **认证**：无需认证

### 5. 上传分片

- **URL**：`POST /api/v1/uploads/chunk`
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

- **URL**：`POST /api/v1/uploads/finalize`
- **认证**：无需认证
- **请求体**：
  ```json
  {
    "uploadId": 1
  }
  ```

### 7. 从 URL 上传

- **URL**：`POST /api/v1/uploads/url`
- **认证**：需要（JWT）
- **请求体**：
  ```json
  {
    "url": "https://example.com/file.zip",
    "filename": "文件名.zip",
    "rootName": "share1",
    "dir": "目标目录"
  }
  ```

## 临时文件接口

### 1. 获取临时文件列表

- **URL**：`GET /api/v1/temp/list`
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

- **URL**：`POST /api/v1/temp/upload`
- **认证**：基于IP
- **Content-Type**：`multipart/form-data`
- **参数**：
  - `file`：上传的文件
  - `expireDate`（可选）：过期日期

### 3. 临时文件分片上传

| 操作 | URL | 说明 |
|------|-----|------|
| 创建会话 | `POST /api/v1/temp/upload/session` | - |
| 获取状态 | `GET /api/v1/temp/upload/session/:uploadId` | - |
| 上传分片 | `POST /api/v1/temp/upload/chunk` | - |
| 完成上传 | `POST /api/v1/temp/upload/finalize` | - |

### 4. 下载临时文件

- **URL**：`GET /api/v1/temp/:code/download`
- **认证**：无需认证

### 5. 获取临时文件信息

- **URL**：`GET /api/v1/temp/:code`
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

- **URL**：`DELETE /api/v1/temp/:code`
- **认证**：基于IP

### 7. 获取临时文件配额

- **URL**：`GET /api/v1/temp/quota`

## 私有存储接口（需认证）

### 1. 获取私有文件列表

- **URL**：`GET /api/v1/private/files`
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

### 2. 上传私有文件

- **URL**：`POST /api/v1/private/upload`
- **认证**：需要（JWT）
- **Content-Type**：`multipart/form-data`
- **参数**：
  - `file`：上传的文件

### 3. 私有文件分片上传

| 操作 | URL |
|------|-----|
| 创建会话 | `POST /api/v1/private/uploads/session` |
| 获取状态 | `GET /api/v1/private/uploads/session/:id` |
| 续传 | `POST /api/v1/private/uploads/session/:id/resume` |
| 取消 | `DELETE /api/v1/private/uploads/session/:id` |
| 上传分片 | `POST /api/v1/private/uploads/chunk` |
| 完成 | `POST /api/v1/private/uploads/finalize` |

### 4. 获取私有存储配额

- **URL**：`GET /api/v1/private/quota`
- **认证**：需要（JWT）

### 5. 删除私有文件

- **URL**：`DELETE /api/v1/private/files/:code`
- **认证**：需要（JWT）

## API Key 管理接口（需管理员权限）

### 1. 列出 API Keys

- **URL**：`GET /api/v1/api-keys`
- **认证**：需要（管理员）

### 2. 创建 API Key

- **URL**：`POST /api/v1/api-keys`
- **认证**：需要（管理员）
- **请求体**：
  ```json
  {
    "name": "应用名称",
    "expiresIn": 86400
  }
  ```

### 3. 删除 API Key

- **URL**：`DELETE /api/v1/api-keys/:id`
- **认证**：需要（管理员）

## 管理接口（需管理员权限）

### 1. 获取用户列表

- **URL**：`GET /api/v1/admin/users`

### 2. 重置用户密码

- **URL**：`PUT /api/v1/admin/users/:uuid/reset-password`
- **请求体**：
  ```json
  {
    "password": "新密码"
  }
  ```

### 3. 禁用/启用用户

- **URL**：`PUT /api/v1/admin/users/:uuid/disabled`
- **请求体**：
  ```json
  {
    "disabled": true
  }
  ```

### 4. 删除用户

- **URL**：`DELETE /api/v1/admin/users/:uuid`

### 5. 获取上传会话列表

- **URL**：`GET /api/v1/admin/sessions`

### 6. 清理过期会话

- **URL**：`POST /api/v1/admin/sessions/cleanup`

## 配置接口

### 1. 获取系统选项

- **URL**：`GET /api/v1/options`
- **认证**：无需认证
- **说明**：供前端加载时获取运行时配置
- **响应**：
  ```json
  {
    "success": true,
    "data": {
      "app": {
        "name": "轻共享"
      },
      "temp": {
        "enabled": true
      },
      "tempFilesEnabled": true,
      "chunkSize": 10485760,
      "maxFileSize": 17179869184,
      "preview": {
        "textExtensions": ["txt", "md", "log", ...],
        "imageExtensions": ["png", "jpg", ...],
        "otherExtensions": ["pdf"]
      }
    }
  }
  ```

### 2. 获取完整配置（需管理员）

- **URL**：`GET /api/v1/config`

### 3. 保存配置（需管理员）

- **URL**：`POST /api/v1/config`

### 4. 重新加载配置（需管理员）

- **URL**：`POST /api/v1/config/reload`

### 5. 初始化状态

- **URL**：`GET /api/v1/setup/status`
- **认证**：无需认证

### 6. 保存初始配置

- **URL**：`POST /api/v1/setup/save`
- **认证**：无需认证（首次配置）

### 7. 验证目录

- **URL**：`POST /api/v1/setup/validate-dir`
- **认证**：无需认证
- **请求体**：
  ```json
  {
    "path": "/path/to/directory"
  }
  ```

### 8. 获取网络接口

- **URL**：`GET /api/v1/setup/network/interfaces`

### 9. 获取默认配置

- **URL**：`GET /api/v1/setup/default-config`

## 健康检查

### 1. 健康状态

- **URL**：`GET /api/v1/health`
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

- **URL**：`GET /api/v1/ready`
- **认证**：无需认证

### 3. Prometheus 指标

- **URL**：`GET /api/v1/metrics`
- **认证**：无需认证

## CLI 接口（需认证）

### 1. CLI 帮助

- **URL**：`GET /api/v1/cli`

### 2. CLI 搜索

- **URL**：`GET /api/v1/cli/search/*query`

### 3. CLI 列出目录

- **URL**：`GET /api/v1/cli/list/*path`

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
  createSession: () => '/api/v1/uploads/session',
  getSession: (uploadId) => `/api/v1/uploads/session/${uploadId}`,
  uploadChunk: () => '/api/v1/uploads/chunk',
  finalize: () => '/api/v1/uploads/finalize'
}
```

### 各模块的 API 配置

| 模块 | createSession | getSession | uploadChunk | finalize |
|------|---------------|------------|-------------|----------|
| 公开文件 | `/api/v1/uploads/session` | `/api/v1/uploads/session/{id}` | `/api/v1/uploads/chunk` | `/api/v1/uploads/finalize` |
| 私有存储 | `/api/v1/private/uploads/session` | `/api/v1/private/uploads/session/{id}` | `/api/v1/private/uploads/chunk` | `/api/v1/private/uploads/finalize` |
| 临时文件 | `/api/v1/temp/upload/session` | `/api/v1/temp/upload/session/{id}` | `/api/v1/temp/upload/chunk` | `/api/v1/temp/upload/finalize` |

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
  createSession: () => '/api/v1/uploads/session',
  getSession: (id) => `/api/v1/uploads/session/${id}`,
  uploadChunk: () => '/api/v1/uploads/chunk',
  finalize: () => '/api/v1/uploads/finalize'
}

const onSuccess = () => {
  console.log('上传成功')
}

const onError = (error) => {
  console.error('上传失败:', error)
}
</script>
```