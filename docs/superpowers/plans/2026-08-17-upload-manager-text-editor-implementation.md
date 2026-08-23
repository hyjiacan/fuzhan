# 上传管理面板 + 剪贴板粘贴 + 在线文本编辑器 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现统一上传管理面板、剪贴板粘贴上传、在线文本编辑器三个功能

**Architecture:** 后端新增 URL 下载任务取消/列表接口、上传会话列表接口、创建文本文件接口；前端新增底部上传管理条、管理对话框、文本编辑器组件、Markdown 预览。后端基于 Gin + GORM，前端基于 Vue3 + Naive UI。

**Tech Stack:** Go (Gin, GORM, xxh3), Vue3 (Naive UI, marked, CodeMirror 轻量)

---

### Task 1: URLDownloadTask 模型新增 cancelled 状态

**Files:**
- Modify: `server/internal/models/url_download_task.go`

- [ ] **Step 1: 添加 cancelled 状态常量**

```go
// 在 URLDownloadStatusFailed 之后添加
URLDownloadStatusCancelled  URLDownloadStatus = "cancelled"  // 已取消
```

- [ ] **Step 2: 更新 UpdateStatus 方法支持 cancelled 记录完成时间**

```go
// 在 url_download_task_repo.go 中，修改 UpdateStatus 的条件判断
// 将:
if status == models.URLDownloadStatusCompleted || status == models.URLDownloadStatusFailed {
// 改为:
if status == models.URLDownloadStatusCompleted || status == models.URLDownloadStatusFailed || status == models.URLDownloadStatusCancelled {
```

### Task 2: URLDownloadTaskRepository 新增按用户查询方法

**Files:**
- Modify: `server/internal/repositories/url_download_task_repo.go`

- [ ] **Step 1: 添加 ListByUser 方法**

```go
// ListByUser 根据用户标识查询任务（分页）
// userID 优先（登录用户），anonymousID 次之（匿名用户）
// storageType 过滤：空字符串表示不过滤
func (r *URLDownloadTaskRepository) ListByUser(userID *string, anonymousID *string, storageType string, page, pageSize int) ([]models.URLDownloadTask, int64, error) {
    var tasks []models.URLDownloadTask
    query := r.db.Model(&models.URLDownloadTask{})

    if userID != nil && *userID != "" {
        query = query.Where("user_id = ?", *userID)
    } else if anonymousID != nil && *anonymousID != "" {
        query = query.Where("anonymous_id = ? AND user_id IS NULL", *anonymousID)
    } else {
        return tasks, 0, nil
    }

    if storageType != "" {
        query = query.Where("storage_type = ?", storageType)
    }

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
    return tasks, total, err
}
```

### Task 3: 上传会话仓库新增按用户查询方法

**Files:**
- Modify: `server/internal/repositories/upload_repo.go`

- [ ] **Step 1: 添加 SessionRepository.ListByUser 方法**

```go
// ListByUser 根据用户ID查询上传会话（非临时/非私有，即公共上传）
func (r *SessionRepository) ListByUser(userID string, page, pageSize int) ([]models.UploadSession, int64, error) {
    var sessions []models.UploadSession
    query := r.db.Model(&models.UploadSession{}).
        Where("target_type = ? OR target_type = ?", models.TargetTypeRegular, "").
        Where("user_id = ?", userID)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
    return sessions, total, err
}

// ListByUserAndType 根据用户ID和存储类型查询上传会话
// 用于私有存储上传
func (r *SessionRepository) ListByUserAndType(userID string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
    var sessions []models.UploadSession
    query := r.db.Model(&models.UploadSession{}).
        Where("target_type = ?", targetType).
        Where("user_id = ?", userID)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
    return sessions, total, err
}
```

### Task 4: UploadSessionHandler 新增列表/取消 URL 任务接口

**Files:**
- Modify: `server/internal/file/upload.go`

- [ ] **Step 1: 添加 CancelURLTask 方法**

```go
// CancelURLTask 取消 URL 下载任务
func (h *UploadSessionHandler) CancelURLTask(c *gin.Context) {
    taskID := c.Param("taskId")
    if taskID == "" {
        utils.HandleBadRequest(c, "无效的任务ID", nil)
        return
    }

    task, err := h.urlTaskRepo.GetByID(taskID)
    if err != nil {
        utils.HandleNotFound(c, "任务不存在")
        return
    }

    // 权限检查：只能取消自己的任务
    if !h.checkTaskOwnership(c, task) {
        utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
        return
    }

    if task.Status != models.URLDownloadStatusPending && task.Status != models.URLDownloadStatusDownloading {
        utils.HandleErrorCompat(c, http.StatusBadRequest, "只能取消进行中的任务", nil)
        return
    }

    // TODO: 实际停止正在下载的 goroutine（通过 context cancel）
    // 当前先标记为 cancelled，后台下载会在下次写入时检测并退出
    if err := h.urlTaskRepo.UpdateStatus(taskID, models.URLDownloadStatusCancelled, "用户取消"); err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "取消失败", nil)
        return
    }

    // 清理临时文件
    if task.TargetPath != "" {
        os.Remove(task.TargetPath)
    }

    middleware.LogOperation(c, "upload.url.cancel", taskID, nil)
    utils.HandleSuccess(c, http.StatusOK, "任务已取消", nil)
}
```

- [ ] **Step 2: 添加 checkTaskOwnership 辅助方法**

```go
// checkTaskOwnership 检查当前用户是否拥有该任务
func (h *UploadSessionHandler) checkTaskOwnership(c *gin.Context, task *models.URLDownloadTask) bool {
    if uid, exists := c.Get("userUUID"); exists {
        if s, ok := uid.(string); ok && s != "" && task.UserID != nil && *task.UserID == s {
            return true
        }
    }
    anonID := c.GetHeader("X-Anonymous-ID")
    if anonID != "" && task.AnonymousID != nil && *task.AnonymousID == anonID {
        return true
    }
    return false
}
```

- [ ] **Step 3: 添加 ListURLTasks 方法**

```go
// ListURLTasks 列出当前用户的 URL 下载任务
func (h *UploadSessionHandler) ListURLTasks(c *gin.Context) {
    storageType := c.Query("type")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

    if page < 1 {
        page = 1
    }
    if pageSize < 1 || pageSize > 100 {
        pageSize = 50
    }

    // 获取用户标识
    var userID, anonymousID *string
    if uid, exists := c.Get("userUUID"); exists {
        if s, ok := uid.(string); ok && s != "" {
            userID = &s
        }
    }
    anonID := c.GetHeader("X-Anonymous-ID")
    if anonID != "" {
        anonymousID = &anonID
    }

    // 根据 storageType 参数映射
    mappedType := ""
    switch storageType {
    case "public":
        mappedType = string(models.URLDownloadStorageRegular)
    case "temp":
        mappedType = string(models.URLDownloadStorageTemp)
    case "private":
        mappedType = string(models.URLDownloadStoragePrivate)
    }

    tasks, total, err := h.urlTaskRepo.ListByUser(userID, anonymousID, mappedType, page, pageSize)
    if err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
        return
    }

    // 脱敏处理：返回前端需要的字段
    result := make([]gin.H, 0, len(tasks))
    for _, t := range tasks {
        result = append(result, gin.H{
            "id":              t.ID,
            "fileName":        t.FileName,
            "fileSize":        t.FileSize,
            "downloadedBytes": t.DownloadedBytes,
            "status":          t.Status,
            "errorMessage":    t.ErrorMessage,
            "createdAt":       t.CreatedAt,
            "completedAt":     t.CompletedAt,
            "storageType":     t.StorageType,
        })
    }

    utils.HandleSuccess(c, http.StatusOK, "", gin.H{
        "tasks": result,
        "total": total,
    })
}
```

- [ ] **Step 4: 添加 RetryURLTask 方法（公开版）**

```go
// RetryURLTask 重试失败的 URL 下载任务
func (h *UploadSessionHandler) RetryURLTask(c *gin.Context) {
    taskID := c.Param("taskId")
    if taskID == "" {
        utils.HandleBadRequest(c, "无效的任务ID", nil)
        return
    }

    task, err := h.urlTaskRepo.GetByID(taskID)
    if err != nil {
        utils.HandleNotFound(c, "任务不存在")
        return
    }

    // 权限检查
    if !h.checkTaskOwnership(c, task) {
        utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
        return
    }

    if task.Status != models.URLDownloadStatusFailed && task.Status != models.URLDownloadStatusCancelled {
        utils.HandleErrorCompat(c, http.StatusBadRequest, "只能重试失败或已取消的任务", nil)
        return
    }

    if err := h.urlTaskRepo.ResetToPending(taskID); err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "重试失败", nil)
        return
    }

    // 重新启动下载
    // 使用原始 URL 重新下载
    go h.downloadFromURL(taskID, task.URL, task.FileSize, task.FileName, "", task.TargetPath, task.TargetPath, "", "", "")

    middleware.LogOperation(c, "upload.url.retry", taskID, nil)
    utils.HandleSuccess(c, http.StatusOK, "任务已重试", nil)
}
```

- [ ] **Step 5: 添加 DeleteURLTask 方法（公开版）**

```go
// DeleteURLTask 删除 URL 下载任务
func (h *UploadSessionHandler) DeleteURLTask(c *gin.Context) {
    taskID := c.Param("taskId")
    if taskID == "" {
        utils.HandleBadRequest(c, "无效的任务ID", nil)
        return
    }

    task, err := h.urlTaskRepo.GetByID(taskID)
    if err != nil {
        utils.HandleNotFound(c, "任务不存在")
        return
    }

    if !h.checkTaskOwnership(c, task) {
        utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作此任务", nil)
        return
    }

    if err := h.urlTaskRepo.Delete(taskID); err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "删除失败", nil)
        return
    }

    middleware.LogOperation(c, "upload.url.delete", taskID, nil)
    utils.HandleSuccess(c, http.StatusOK, "任务已删除", nil)
}
```

### Task 5: UploadSessionHandler 新增列表上传会话接口

**Files:**
- Modify: `server/internal/file/upload.go`

- [ ] **Step 1: 添加 ListUploadSessions 方法**

```go
// ListUploadSessions 列出当前用户的上传会话
func (h *UploadSessionHandler) ListUploadSessions(c *gin.Context) {
    sessionType := c.Query("type")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

    if page < 1 {
        page = 1
    }
    if pageSize < 1 || pageSize > 100 {
        pageSize = 50
    }

    clientIP := utils.GetClientIP(c)

    // 根据 type 选择查询不同的数据源
    switch sessionType {
    case "public":
        // public 上传会话：upload_sessions 表，target_type=regular or empty
        sessions, total, err := h.service.ListByUser(clientIP, models.TargetTypeRegular, page, pageSize)
        if err != nil {
            utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
            return
        }
        result := make([]gin.H, 0, len(sessions))
        for _, s := range sessions {
            result = append(result, gin.H{
                "id":        s.ID,
                "fileName":  s.FileName,
                "fileSize":  s.FileSize,
                "status":    s.Status,
                "createdAt": s.CreatedAt,
                "expiredAt": s.ExpiredAt,
            })
        }
        utils.HandleSuccess(c, http.StatusOK, "", gin.H{"sessions": result, "total": total})

    case "temp":
        // temp 上传会话：temp_upload_sessions 表
        sessions, total, err := h.service.ListTempByUser(clientIP, page, pageSize)
        if err != nil {
            utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
            return
        }
        result := make([]gin.H, 0, len(sessions))
        for _, s := range sessions {
            result = append(result, gin.H{
                "id":        s.ID,
                "fileName":  s.Filename,
                "fileSize":  s.FileSize,
                "status":    s.Status,
                "createdAt": s.CreatedAt,
                "expiredAt": s.ExpiredAt,
            })
        }
        utils.HandleSuccess(c, http.StatusOK, "", gin.H{"sessions": result, "total": total})

    case "private":
        // private 上传会话：upload_sessions 表，target_type=private
        sessions, total, err := h.service.ListByUser(clientIP, models.TargetTypePrivate, page, pageSize)
        if err != nil {
            utils.HandleErrorCompat(c, http.StatusInternalServerError, "查询失败", nil)
            return
        }
        result := make([]gin.H, 0, len(sessions))
        for _, s := range sessions {
            result = append(result, gin.H{
                "id":        s.ID,
                "fileName":  s.FileName,
                "fileSize":  s.FileSize,
                "status":    s.Status,
                "createdAt": s.CreatedAt,
                "expiredAt": s.ExpiredAt,
            })
        }
        utils.HandleSuccess(c, http.StatusOK, "", gin.H{"sessions": result, "total": total})

    default:
        utils.HandleBadRequest(c, "无效的会话类型", nil)
    }
}
```

### Task 6: UploadSessionService 新增列表方法

**Files:**
- Modify: `server/internal/services/upload_session_service.go`

- [ ] **Step 1: 添加 ListByUser 方法**

```go
// ListByUser 根据用户ID和存储类型查询上传会话
func (s *UploadSessionService) ListByUser(userID string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
    var sessions []models.UploadSession
    query := s.db.Model(&models.UploadSession{}).
        Where("target_type = ?", targetType).
        Where("user_id = ?", userID)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
    return sessions, total, err
}
```

- [ ] **Step 2: 添加 ListTempByUser 方法**

```go
// ListTempByUser 根据用户IP查询临时上传会话
// 注：TempUploadSession 在 temp 包中，这里直接使用 db 查询
func (s *UploadSessionService) ListTempByUser(clientIP string, page, pageSize int) ([]map[string]interface{}, int64, error) {
    type TempSession struct {
        ID        uint
        Filename  string
        FileSize  int64
        Status    string
        CreatedAt time.Time
        ExpiredAt time.Time
    }

    var sessions []TempSession
    var total int64

    // 查询 temp_upload_sessions 表
    tableName := "temp_upload_sessions"
    countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE client_ip = ?", tableName)
    if err := s.db.Raw(countQuery, clientIP).Scan(&total).Error; err != nil {
        return nil, 0, err
    }

    dataQuery := fmt.Sprintf("SELECT id, filename, file_size, 'in_progress' as status, created_at, expired_at FROM %s WHERE client_ip = ? ORDER BY created_at DESC LIMIT ? OFFSET ?", tableName)
    if err := s.db.Raw(dataQuery, clientIP, pageSize, (page-1)*pageSize).Scan(&sessions).Error; err != nil {
        return nil, 0, err
    }

    result := make([]map[string]interface{}, len(sessions))
    for i, s := range sessions {
        result[i] = map[string]interface{}{
            "id":        s.ID,
            "fileName":  s.Filename,
            "fileSize":  s.FileSize,
            "status":    s.Status,
            "createdAt": s.CreatedAt,
            "expiredAt": s.ExpiredAt,
        }
    }
    return result, total, nil
}
```

### Task 7: 后端新增 CreateTextFile 接口

**Files:**
- Create: `server/internal/file/create_text.go`
- Modify: `server/main.go`（路由注册）

- [ ] **Step 1: 创建 create_text.go 文件**

```go
package file

import (
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/middleware"
    "fuzhan/internal/models"
    "fuzhan/internal/utils"
)

// CreateTextFileRequest 创建文本文件请求
type CreateTextFileRequest struct {
    RootName  string `json:"rootName" binding:"required,max=255"`
    Directory string `json:"directory" binding:"required,max=1024"`
    Filename  string `json:"filename" binding:"required,max=255"`
    Content   string `json:"content" binding:"max=1048576"` // 最大 1MB
}

// CreateTextFile 创建文本文件
func (fh *FileHandlers) CreateTextFile(c *gin.Context) {
    var req CreateTextFileRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        utils.HandleBadRequest(c, "请求参数格式错误: "+err.Error(), nil)
        return
    }

    // 检查公共目录上传开关
    if !appconfig.GlobalConfig.Upload.Enabled {
        utils.HandleErrorCompat(c, http.StatusForbidden, "公共目录上传已关闭", nil)
        return
    }

    // 路径安全校验
    if strings.Contains(req.Directory, "..") {
        utils.HandleBadRequest(c, "路径不能包含 ..", nil)
        return
    }

    req.Filename = strings.ReplaceAll(req.Filename, "/", "")
    req.Filename = strings.ReplaceAll(req.Filename, "\\", "")
    req.Filename = strings.ReplaceAll(req.Filename, "\x00", "")

    if req.Filename == "" {
        utils.HandleBadRequest(c, "文件名不能为空", nil)
        return
    }

    rootDir, exists := appconfig.RootNames[req.RootName]
    if !exists {
        utils.HandleBadRequest(c, "指定的根目录不存在", nil)
        return
    }

    cleanDir := filepath.Clean(req.Directory)
    if cleanDir == "." || cleanDir == "/" {
        cleanDir = ""
    }

    targetDir := filepath.Join(rootDir, cleanDir)
    absTargetDir, absErr := filepath.Abs(targetDir)
    if absErr != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "路径解析失败", nil)
        return
    }
    absRootPath, absErr := filepath.Abs(rootDir)
    if absErr != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "根路径解析失败", nil)
        return
    }
    if !strings.HasPrefix(absTargetDir, absRootPath) {
        utils.HandleBadRequest(c, "路径越界", nil)
        return
    }

    targetPath := filepath.Join(targetDir, req.Filename)

    // 检查文件是否已存在
    if _, err := os.Stat(targetPath); err == nil {
        utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
        return
    }

    // 创建目录
    if err := os.MkdirAll(targetDir, 0755); err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建目录失败", nil)
        return
    }

    // 写入文件
    if err := os.WriteFile(targetPath, []byte(req.Content), 0644); err != nil {
        utils.HandleErrorCompat(c, http.StatusInternalServerError, "写入文件失败", nil)
        return
    }

    // 同步到索引表
    if fh.BaseHandler != nil && fh.BaseHandler.IndexSvc != nil {
        relativePath := "/" + req.Filename
        if cleanDir != "" {
            relativePath = "/" + cleanDir + "/" + req.Filename
        }
        if err := fh.BaseHandler.IndexSvc.SyncFile(req.RootName, relativePath); err != nil {
            utils.Warn("创建文本文件后同步索引失败", utils.String("path", targetPath), utils.Err(err))
        } else {
            fh.BaseHandler.IndexSvc.MarkRecentlySynced(req.RootName, relativePath)
        }
        // 触发哈希计算
        fh.BaseHandler.IndexSvc.TriggerHash(c.Request.Context())
    }

    // 记录操作日志
    record := &models.OperationRecord{
        FileName:   req.Filename,
        FileSize:   int64(len(req.Content)),
        FilePath:   "/" + req.Filename,
        FullPath:   "/" + req.RootName + "/" + req.Filename,
        RootName:   req.RootName,
        FileType:   "text",
        ClientIP:   utils.GetClientIP(c),
        UserID:     utils.GetClientIP(c),
        UploadType: models.TargetTypeRegular,
        UploadTime: time.Now(),
    }
    if fh.BaseHandler != nil && fh.BaseHandler.RecordRepo != nil {
        if err := fh.BaseHandler.RecordRepo.Create(record); err != nil {
            utils.Warn("创建文本文件记录失败", utils.Err(err))
        }
    }

    middleware.LogOperation(c, "file.create-text", req.Filename, nil)
    utils.HandleSuccess(c, http.StatusCreated, "文件创建成功", gin.H{
        "path": targetPath,
    })
}
```

- [ ] **Step 2: 检查 BaseHandler 结构是否有 IndexSvc 和 RecordRepo 字段**

查看 `server/internal/file/base.go` 确认 BaseHandler 的字段定义，如果缺少 IndexSvc 或 RecordRepo 字段，则需要添加。

```go
// 如果 base.go 的 BaseHandler 中没有 IndexSvc 和 RecordRepo，需要添加
// 在 base.go 中添加字段
```

- [ ] **Step 3: 在 main.go 中注册路由**

```go
// 在文件路由组 files 中添加（约第 750 行附近）
files.POST("/create-text", authMiddleware.AuthOptional(), fileHandlers.CreateTextFile)
```

### Task 8: 注册上传管理相关路由

**Files:**
- Modify: `server/main.go`

- [ ] **Step 1: 在 uploads 路由组中添加新路由**

```go
// 在 uploads 路由组中添加（约第 725 行附近）
uploads.GET("/sessions", authMiddleware.AuthOptional(), uploadSessionHandler.ListUploadSessions)
uploads.GET("/url-tasks", authMiddleware.AuthOptional(), uploadSessionHandler.ListURLTasks)
uploads.POST("/url-tasks/:taskId/cancel", authMiddleware.AuthOptional(), uploadSessionHandler.CancelURLTask)
uploads.POST("/url-tasks/:taskId/retry", authMiddleware.AuthOptional(), uploadSessionHandler.RetryURLTask)
uploads.DELETE("/url-tasks/:taskId", authMiddleware.AuthOptional(), uploadSessionHandler.DeleteURLTask)
```

### Task 9: 前端 API 方法

**Files:**
- Modify: `ui/src/api/index.js`

- [ ] **Step 1: 添加上传管理相关 API**

```javascript
// 上传管理
export const listUploadSessions = (type, page = 1, pageSize = 50) =>
  get('/api/v1/uploads/sessions', { type, page, pageSize })

export const listURLTasks = (type, page = 1, pageSize = 50) =>
  get('/api/v1/uploads/url-tasks', { type, page, pageSize })

export const cancelURLTask = (taskId) =>
  post(`/api/v1/uploads/url-tasks/${taskId}/cancel`, {})

export const retryURLTask = (taskId) =>
  post(`/api/v1/uploads/url-tasks/${taskId}/retry`, {})

export const deleteURLTask = (taskId) =>
  del(`/api/v1/uploads/url-tasks/${taskId}`)

export const cancelSession = (sessionId) =>
  del(`/api/v1/uploads/session/${sessionId}`)

// 创建文本文件
export const createTextFile = (data) =>
  post('/api/v1/files/create-text', data)
```

### Task 10: 安装 marked 依赖

**Files:**
- Modify: `ui/package.json`

- [ ] **Step 1: 安装 marked 包**

```bash
cd d:\Projects\lightshare\ui
npm install marked
```

### Task 11: 创建 UploadManagerBar.vue 组件

**Files:**
- Create: `ui/src/components/upload/UploadManagerBar.vue`

- [ ] **Step 1: 创建底部固定条组件**

```vue
<template>
  <div class="upload-manager-bar" v-if="activeCount > 0">
    <n-button text @click="showDialog = true" class="manager-btn">
      <template #icon>
        <n-icon><svg><!-- 上传图标 --></svg></n-icon>
      </template>
      上传管理
      <n-badge :value="activeCount" :max="99" class="badge" />
    </n-button>
    <UploadManagerDialog v-model:show="showDialog" />
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { listUploadSessions, listURLTasks } from '../../api'
import UploadManagerDialog from './UploadManagerDialog.vue'

const showDialog = ref(false)
const activeCount = ref(0)
const anonID = ref(localStorage.getItem('anonymousId') || '')

let pollTimer = null

async function refreshCount() {
  try {
    // 统计公共上传的进行中数量
    const [sessRes, urlRes] = await Promise.all([
      listUploadSessions('public', 1, 1000),
      listURLTasks('public', 1, 1000)
    ])
    const sessions = sessRes?.data?.sessions || []
    const tasks = urlRes?.data?.tasks || []
    const activeSessions = sessions.filter(s => s.status === 'in_progress' || s.status === 'uploading').length
    const activeTasks = tasks.filter(t => t.status === 'pending' || t.status === 'downloading').length
    activeCount.value = activeSessions + activeTasks
  } catch {
    // 静默失败
  }
}

onMounted(() => {
  refreshCount()
  pollTimer = setInterval(refreshCount, 5000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.upload-manager-bar {
  position: fixed;
  bottom: 0;
  right: 20px;
  z-index: 1000;
  background: var(--bar-bg, #fff);
  border: 1px solid var(--border-color, #e0e0e0);
  border-bottom: none;
  border-radius: 8px 8px 0 0;
  padding: 8px 16px;
  box-shadow: 0 -2px 8px rgba(0,0,0,0.1);
}
.manager-btn {
  display: flex;
  align-items: center;
  gap: 8px;
}
.badge {
  margin-left: 4px;
}
</style>
```

### Task 12: 创建 UploadManagerDialog.vue 组件

**Files:**
- Create: `ui/src/components/upload/UploadManagerDialog.vue`

- [ ] **Step 1: 创建上传管理对话框**

```vue
<template>
  <n-modal v-model:show="show" :mask-closable="false" preset="card" title="上传管理" style="width: 800px; max-height: 80vh;">
    <n-tabs type="line" animated @update:value="onTabChange">
      <n-tab-pane name="public" tab="公共上传">
        <upload-task-table ref="publicTable" type="public" />
      </n-tab-pane>
      <n-tab-pane name="temp" tab="临时上传">
        <upload-task-table ref="tempTable" type="temp" />
      </n-tab-pane>
      <n-tab-pane name="private" tab="私有上传">
        <upload-task-table ref="privateTable" type="private" />
      </n-tab-pane>
    </n-tabs>
  </n-modal>
</template>

<script setup>
import { ref, watch } from 'vue'
import UploadTaskTable from './UploadTaskTable.vue'

const props = defineProps({
  show: Boolean
})
const emit = defineEmits(['update:show'])

const show = ref(props.show)
watch(() => props.show, (v) => { show.value = v })
watch(show, (v) => { emit('update:show', v) })

function onTabChange() {
  // 切换标签页重新加载
}
</script>
```

### Task 13: 创建 UploadTaskTable.vue 组件

**Files:**
- Create: `ui/src/components/upload/UploadTaskTable.vue`

- [ ] **Step 1: 创建上传任务列表组件**

```vue
<template>
  <div class="upload-task-table">
    <n-spin :show="loading">
      <n-data-table
        :columns="columns"
        :data="tasks"
        :pagination="pagination"
        :bordered="false"
        :single-line="false"
        size="small"
        virtual-scroll
        :max-height="400"
      />
    </n-spin>
  </div>
</template>

<script setup>
import { h, ref, onMounted, onUnmounted } from 'vue'
import { NButton, NTag, NProgress, NSpace, useMessage } from 'naive-ui'
import { listUploadSessions, listURLTasks, cancelURLTask, retryURLTask, deleteURLTask, cancelSession } from '../../api'

const props = defineProps({
  type: { type: String, required: true }
})

const message = useMessage()
const loading = ref(false)
const tasks = ref([])
const pagination = ref({ page: 1, pageSize: 50, showSizePicker: true, pageSizes: [20, 50, 100] })
let pollTimer = null

const statusMap = {
  'in_progress': { type: 'info', text: '上传中' },
  'uploading': { type: 'info', text: '上传中' },
  'pending': { type: 'warning', text: '等待中' },
  'downloading': { type: 'info', text: '下载中' },
  'completed': { type: 'success', text: '已完成' },
  'failed': { type: 'error', text: '失败' },
  'cancelled': { type: 'default', text: '已取消' },
  'expired': { type: 'default', text: '已过期' },
}

const columns = [
  { title: '文件名', key: 'fileName', width: 200, ellipsis: { tooltip: true } },
  { title: '进度', key: 'progress', width: 200,
    render: (row) => row.fileSize > 0
      ? h(NProgress, { type: 'line', status: row.status === 'completed' ? 'success' : 'info', percentage: Math.round((row.downloadedBytes || row.uploadedBytes || 0) / row.fileSize * 100), indicatorPlacement: 'inside', height: 20 })
      : '-'
  },
  { title: '状态', key: 'status', width: 100,
    render: (row) => {
      const info = statusMap[row.status] || { type: 'default', text: row.status }
      return h(NTag, { type: info.type, size: 'small' }, { default: () => info.text })
    }
  },
  { title: '文件大小', key: 'fileSize', width: 100,
    render: (row) => row.fileSize ? formatSize(row.fileSize) : '-'
  },
  { title: '操作', key: 'actions', width: 180,
    render: (row) => {
      const btns = []
      if (['pending', 'downloading', 'uploading', 'in_progress'].includes(row.status)) {
        btns.push(h(NButton, { size: 'tiny', type: 'warning', onClick: () => handleCancel(row) }, { default: () => '取消' }))
      }
      if (['failed', 'cancelled'].includes(row.status)) {
        btns.push(h(NButton, { size: 'tiny', type: 'primary', onClick: () => handleRetry(row) }, { default: () => '重试' }))
      }
      if (['completed', 'cancelled', 'failed', 'expired'].includes(row.status)) {
        btns.push(h(NButton, { size: 'tiny', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删除' }))
      }
      return h(NSpace, {}, { default: () => btns })
    }
  }
]

function formatSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
  return `${size.toFixed(1)} ${units[i]}`
}

// 判断是否为 session 类型（upload_sessions 的 id 是数字，url_tasks 的 id 是字符串 UUID）
function isSession(row) {
  return typeof row.id === 'number'
}

async function handleCancel(row) {
  try {
    if (isSession(row)) {
      await cancelSession(row.id)
    } else {
      await cancelURLTask(row.id)
    }
    message.success('已取消')
    await loadData()
  } catch (e) {
    message.error('取消失败')
  }
}

async function handleRetry(row) {
  try {
    await retryURLTask(row.id)
    message.success('已重试')
    await loadData()
  } catch (e) {
    message.error('重试失败')
  }
}

async function handleDelete(row) {
  try {
    await deleteURLTask(row.id)
    message.success('已删除')
    await loadData()
  } catch (e) {
    message.error('删除失败')
  }
}

async function loadData() {
  loading.value = true
  try {
    const [sessRes, urlRes] = await Promise.all([
      listUploadSessions(props.type, pagination.value.page, pagination.value.pageSize),
      listURLTasks(props.type, pagination.value.page, pagination.value.pageSize)
    ])
    const sessions = (sessRes?.data?.sessions || []).map(s => ({ ...s, _type: 'session' }))
    const urlTasks = (urlRes?.data?.tasks || []).map(t => ({ ...t, _type: 'url' }))
    // 合并并按创建时间排序
    tasks.value = [...sessions, ...urlTasks].sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
  } catch (e) {
    message.error('加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadData()
  pollTimer = setInterval(loadData, 2000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.upload-task-table {
  min-height: 200px;
}
</style>
```

### Task 14: 创建 TextEditorDialog.vue 组件

**Files:**
- Create: `ui/src/components/upload/TextEditorDialog.vue`

- [ ] **Step 1: 创建文本编辑器对话框**

```vue
<template>
  <n-modal v-model:show="show" :mask-closable="false" preset="card" title="新建文本文件" style="width: 700px;" :segmented="{ content: true }">
    <n-space vertical>
      <n-input v-model:value="filename" placeholder="文件名，如 readme.md" clearable />
      <n-input
        v-model:value="content"
        type="textarea"
        :rows="20"
        placeholder="在此输入文件内容..."
        :input-props="{ style: 'font-family: monospace; line-height: 1.6;' }"
      />
    </n-space>
    <template #footer>
      <n-space justify="end">
        <n-button @click="show = false">取消</n-button>
        <n-button type="primary" @click="handleSave" :loading="saving">保存</n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { createTextFile } from '../../api'

const props = defineProps({
  show: Boolean,
  rootName: { type: String, default: '' },
  directory: { type: String, default: '' }
})
const emit = defineEmits(['update:show', 'saved'])

const message = useMessage()
const show = ref(props.show)
const filename = ref('newfile.txt')
const content = ref('')
const saving = ref(false)

watch(() => props.show, (v) => { show.value = v })
watch(show, (v) => { emit('update:show', v) })

async function handleSave() {
  if (!filename.value.trim()) {
    message.warning('请输入文件名')
    return
  }
  saving.value = true
  try {
    await createTextFile({
      rootName: props.rootName,
      directory: props.directory,
      filename: filename.value.trim(),
      content: content.value
    })
    message.success('文件创建成功')
    show.value = false
    emit('saved')
  } catch (e) {
    message.error('创建失败: ' + (e.message || '未知错误'))
  } finally {
    saving.value = false
  }
}
</script>
```

### Task 15: 修改 UploadManager.vue 添加按钮

**Files:**
- Modify: `ui/src/components/upload/UploadManager.vue`

- [ ] **Step 1: 添加"从剪贴板粘贴"和"新建文本"按钮**

在 UploadManager.vue 的上传方式选择区添加两个按钮，参考已有按钮样式。

```vue
<!-- 在合适位置添加 -->
<n-button @click="pasteFromClipboard" :disabled="pasting">
  <template #icon><n-icon><PasteIcon /></n-icon></template>
  从剪贴板粘贴
</n-button>
<n-button @click="showTextEditor = true">
  <template #icon><n-icon><FileTextIcon /></n-icon></template>
  新建文本
</n-button>

<!-- 文本编辑器对话框 -->
<TextEditorDialog
  v-model:show="showTextEditor"
  :root-name="state.rootName"
  :directory="state.dir"
  @saved="refreshList"
/>
```

- [ ] **Step 2: 实现剪贴板粘贴方法**

```javascript
import { useMessage } from 'naive-ui'

const pasting = ref(false)
const showTextEditor = ref(false)
const message = useMessage()

async function pasteFromClipboard() {
  pasting.value = true
  try {
    const items = await navigator.clipboard.read()
    const files = []
    for (const item of items) {
      for (const type of item.types) {
        if (type.startsWith('image/')) {
          const blob = await item.getType(type)
          const ext = type.split('/')[1]
          const filename = `clipboard-${Date.now()}.${ext}`
          files.push(new File([blob], filename, { type }))
        } else if (type === 'text/plain') {
          const blob = await item.getType(type)
          const text = await blob.text()
          const filename = `clipboard-${Date.now()}.txt`
          files.push(new File([text], filename, { type: 'text/plain' }))
        }
      }
    }
    if (files.length === 0) {
      message.warning('剪贴板中没有可读取的文件或文本')
      return
    }
    // 使用现有上传流程提交
    for (const file of files) {
      await startUpload(file)
    }
  } catch (e) {
    // 降级方案：提示用户使用 Ctrl+V
    message.warning('无法读取剪贴板，请尝试 Ctrl+V 粘贴')
  } finally {
    pasting.value = false
  }
}
```

### Task 16: 修改 FilePreview.vue 添加 Markdown 渲染

**Files:**
- Modify: `ui/src/components/file/FilePreview.vue`

- [ ] **Step 1: 添加 Markdown 渲染分支**

在文件预览的文本类型判断中，添加 `.md` / `.markdown` 扩展名检测，使用 `marked` 库渲染。

```vue
<script setup>
// 添加 import
import { marked } from 'marked'

// 添加计算属性
const isMarkdown = computed(() => {
  const ext = props.file?.name?.toLowerCase() || ''
  return ext.endsWith('.md') || ext.endsWith('.markdown')
})

// 添加渲染方法
const renderedMarkdown = computed(() => {
  if (!textContent.value) return ''
  return marked.parse(textContent.value)
})
</script>

<template>
  <!-- 在预览区域添加 -->
  <div v-if="isMarkdown" class="markdown-preview" v-html="renderedMarkdown"></div>
</template>

<style scoped>
.markdown-preview {
  padding: 16px;
  line-height: 1.8;
  overflow: auto;
  max-height: 70vh;
}
.markdown-preview :deep(h1) { font-size: 1.8em; margin: 0.6em 0; }
.markdown-preview :deep(h2) { font-size: 1.5em; margin: 0.5em 0; }
.markdown-preview :deep(h3) { font-size: 1.3em; margin: 0.4em 0; }
.markdown-preview :deep(p) { margin: 0.5em 0; }
.markdown-preview :deep(code) { background: #f5f5f5; padding: 2px 6px; border-radius: 3px; font-family: monospace; }
.markdown-preview :deep(pre) { background: #f5f5f5; padding: 12px; border-radius: 6px; overflow-x: auto; }
.markdown-preview :deep(pre code) { background: none; padding: 0; }
.markdown-preview :deep(table) { border-collapse: collapse; width: 100%; }
.markdown-preview :deep(th), .markdown-preview :deep(td) { border: 1px solid #ddd; padding: 8px; }
.markdown-preview :deep(blockquote) { border-left: 4px solid #ddd; margin: 0; padding-left: 16px; color: #666; }
.markdown-preview :deep(img) { max-width: 100%; }
</style>
```

### Task 17: 修改 App.vue 添加 UploadManagerBar

**Files:**
- Modify: `ui/src/App.vue`

- [ ] **Step 1: 添加 UploadManagerBar 组件**

```vue
<!-- 在 layout 底部添加 -->
<UploadManagerBar />
```

### Task 18: 验证编译

- [ ] **Step 1: 构建后端**

```bash
cd d:\Projects\lightshare
go build ./...
```

- [ ] **Step 2: 构建前端**

```bash
cd d:\Projects\lightshare\ui
npm run build
```

- [ ] **Step 3: 修复所有编译错误并重复直到通过**
