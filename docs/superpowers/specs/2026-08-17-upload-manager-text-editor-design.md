# 上传管理面板 + 剪贴板粘贴 + 在线文本编辑器 设计文档

日期：2026-08-17

## 概述

本次新增三个功能：
1. 统一的上传管理面板，管理本地上传和 URL 下载任务
2. 剪贴板粘贴上传
3. 在线创建文本文件 + Markdown 渲染预览

---

## 功能1：上传管理面板

### 入口

在页面底部添加一个固定条（类似 `IndexStatusBar`），显示"上传管理"按钮，旁边带数字角标（进行中任务数）。点击按钮弹出上传管理对话框。

### 对话框结构

上传管理对话框使用标签页（`NTabs`）区分三类上传：

| 标签页 | 数据来源 | 归属识别 |
|--------|----------|----------|
| 公共上传 | `upload_sessions` + `url_download_tasks`（`storage_type` 为空/regular） | `AnonymousID` 或 `UserID` |
| 临时上传 | `temp_upload_sessions` + `url_download_tasks`（`storage_type=temp`） | `AnonymousID` |
| 私有上传 | 私有存储分片上传 + `url_download_tasks`（`storage_type=private`） | `UserID` |

### 每项展示内容

- 文件名
- 进度条（分片上传：已上传/总大小；URL下载：已下载/总大小）
- 状态标签：上传中 / 完成 / 失败 / 已取消
- 文件大小
- 操作按钮：取消（进行中）/ 重试（失败）/ 删除（完成/已取消）

### 后端新增接口

| 方法 | 端点 | 说明 | 认证 |
|------|------|------|------|
| GET | `/api/v1/uploads/sessions?type=public` | 列出当前用户公共上传会话 | AuthOptional |
| GET | `/api/v1/uploads/sessions?type=temp` | 列出当前临时会话 | AuthOptional |
| GET | `/api/v1/uploads/sessions?type=private` | 列出当前用户私有上传会话 | AuthRequired |
| POST | `/api/v1/uploads/sessions/:id/cancel` | 取消上传会话 | AuthOptional |
| GET | `/api/v1/uploads/url-tasks?type=public` | 列出当前用户 URL 下载任务 | AuthOptional |
| GET | `/api/v1/uploads/url-tasks?type=temp` | 列出当前临时 URL 下载任务 | AuthOptional |
| GET | `/api/v1/uploads/url-tasks?type=private` | 列出当前用户私有 URL 下载任务 | AuthRequired |
| POST | `/api/v1/uploads/url-tasks/:id/cancel` | 取消 URL 下载任务 | AuthOptional |
| POST | `/api/v1/uploads/url-tasks/:id/retry` | 重试失败 URL 下载任务 | AuthOptional |

### 后端已有关键接口（复用）

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | `/api/v1/uploads/session/:uploadId` | 获取单个上传会话状态 |
| DELETE | `/api/v1/uploads/session/:uploadId` | 取消分片上传会话 |
| GET | `/api/v1/uploads/url-task/:taskId` | 获取单个 URL 任务进度 |
| POST | `/api/v1/admin/url-tasks/:id/retry` | 重试 URL 任务（管理端） |
| DELETE | `/api/v1/admin/url-tasks/:id` | 删除 URL 任务（管理端） |

### 前端组件

- `UploadManagerBar.vue` — 底部固定条，显示进行中任务数，点击弹出对话框
- `UploadManagerDialog.vue` — 上传管理对话框，包含三个标签页
- 每个标签页内部使用 `NDataTable` 展示任务列表，虚拟滚动

### 数据模型

**UploadSession 已有字段**：`id`, `filename`, `fileSize`, `uploadedBytes`, `status`, `anonymousID`, `userID`, `createdAt`, `expiresAt`

**URLDownloadTask 已有字段**：`id`, `url`, `fileName`, `fileSize`, `downloadedBytes`, `status`, `anonymousID`, `userID`, `storageType`, `errorMessage`, `createdAt`, `completedAt`

**状态值**：
- UploadSession: `uploading`, `completed`, `failed`, `cancelled`
- URLDownloadTask: `pending`, `downloading`, `completed`, `failed`

### 取消逻辑

- **分片上传取消**：已有 `CancelSession` 逻辑，清理临时文件，标记状态为 `cancelled`
- **URL下载取消**：新增 `cancelURLDownload` 方法，通过 context cancellation 停止下载 goroutine，清理临时文件，标记状态为 `cancelled`（新增状态值）
- **URLDownloadTask 模型**：新增 `cancelled` 状态常量，状态枚举变为 `pending` / `downloading` / `completed` / `failed` / `cancelled`

---

## 功能2：剪贴板粘贴上传

### 入口

在 `UploadManager.vue` 的上传方式选择区添加一个"从剪贴板粘贴"按钮。

### 技术实现

1. 点击按钮后调用 `navigator.clipboard.read()` 读取剪贴板内容
2. 支持以下类型：
   - `image/png`、`image/jpeg`、`image/webp` — 构造为 `File` 对象，文件名自动生成（如 `clipboard-20260817-xxx.png`）
   - `text/plain` — 构造为 `File` 对象，扩展名为 `.txt`
3. 读取到的内容通过现有分片上传流程提交
4. 如果浏览器不支持 `clipboard.read()`，降级使用 `paste` 事件监听

### 兼容性

- `navigator.clipboard.read()` 需要 HTTPS 或 localhost，支持 Chrome/Edge/Firefox
- 降级方案：全局监听 `paste` 事件，`event.clipboardData.files` 获取文件

---

## 功能3：在线创建文本文件

### 入口

在 `UploadManager.vue` 中添加"新建文本"按钮，点击弹出文本编辑器对话框。

### 支持的格式

`.txt`、`.md`、`.json`、`.yaml`/`.yml`、`.xml`、`.html`、`.css`、`.js`、`.ts`、`.sh`、`.bat`、`.env`、`.ini`、`.conf`、`.log`

### 编辑器对话框

- 文件名输入框：默认 `newfile.txt`，用户直接填写文件名及扩展名（如 `readme.md`），编辑器根据扩展名自动识别语法高亮
- 文本编辑区：使用 `NInput` type="textarea"（monospace 字体），支持语法高亮（`CodeMirror` 轻量版）
- 底部操作：保存、取消

### 保存流程

1. 用户输入内容 → 点击保存
2. 调用后端 `POST /api/v1/files/create-text` 接口
3. 后端校验路径安全，创建文件，写入内容
4. 同步到索引表
5. 前端刷新文件列表，关闭编辑器对话框

### 后端新增接口

| 方法 | 端点 | 参数 | 说明 |
|------|------|------|------|
| POST | `/api/v1/files/create-text` | `rootName`, `directory`, `filename`, `content`, `encoding` | 创建文本文件 |

### 后端实现

- 路径安全校验：复用 `PathValidator`
- 文件已存在检查：返回 409 Conflict
- 写入内容后同步到索引表
- 记录操作日志

### Markdown 渲染预览

**前端改动**：在 `FilePreview.vue` 中，当扩展名为 `.md` 或 `.markdown` 时，使用 `marked` 库将 Markdown 渲染为 HTML 展示。

**实现方式**：
1. 安装 `marked` npm 包
2. `FilePreview.vue` 添加 `fileType === 'markdown'` 分支
3. 使用 `marked.parse()` 将文本内容转为 HTML
4. 使用 `v-html` 渲染，添加安全样式（代码块、表格、标题等）

---

## 影响范围

### 后端修改文件

| 文件 | 改动 |
|------|------|
| `server/internal/file/upload.go` | 新增 URL 下载取消接口、列表接口 |
| `server/internal/file/handler.go` | 新增 create-text 接口 |
| `server/internal/file/base.go` | 无需改动 |
| `server/internal/repositories/url_download_task_repo.go` | 新增按用户查询方法 |
| `server/internal/models/url_download_task.go` | 新增 `cancelled` 状态（可选） |
| `server/main.go` | 注册新路由 |

### 前端修改文件

| 文件 | 改动 |
|------|------|
| `ui/src/components/upload/UploadManager.vue` | 添加剪贴板粘贴按钮、新建文本按钮 |
| `ui/src/components/upload/UploadManagerBar.vue` | 新建：底部固定条组件 |
| `ui/src/components/upload/UploadManagerDialog.vue` | 新建：上传管理对话框组件 |
| `ui/src/components/upload/TextEditorDialog.vue` | 新建：文本编辑器对话框组件 |
| `ui/src/components/file/FilePreview.vue` | 添加 Markdown 渲染分支 |
| `ui/src/api/index.js` | 新增 API 方法 |
| `ui/src/App.vue` | 添加 UploadManagerBar 组件 |
| `ui/src/views/HomeView.vue` | 可选：移除旧上传对话框中的进度显示 |
| `ui/package.json` | 新增 `marked` 依赖 |