package admin

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/constants"
    "fuzhan/internal/middleware"
    "fuzhan/pkg/response"
    "fuzhan/internal/repositories"
    "fuzhan/internal/services"
    "fuzhan/internal/services/migration"
    "fuzhan/internal/utils"
)

// Handler 管理员处理器
type Handler struct {
    adminService               *services.AdminService
    db                         *gorm.DB
    migrationRecoveryService   *migration.RecoveryService
    migrationRollbackService   *migration.RollbackService
}

// NewHandler 创建管理员处理器
func NewHandler(adminService *services.AdminService, db *gorm.DB, recoveryService *migration.RecoveryService, rollbackService *migration.RollbackService) *Handler {
    return &Handler{
        adminService:               adminService,
        db:                         db,
        migrationRecoveryService:   recoveryService,
        migrationRollbackService:   rollbackService,
    }
}

// UsersHandler 处理获取用户列表
func (h *Handler) UsersHandler(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
    query := c.Query("query")

    result, err := h.adminService.ListUsers(page, pageSize, query)
    if err != nil {
        response.HandleInternalServerError(c, err.Error())
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", result)
}

// ResetPasswordHandler 处理重置用户密码
func (h *Handler) ResetPasswordHandler(c *gin.Context) {
    uuid := c.Param("uuid")
    if uuid == "" {
        middleware.LogOperation(c, "admin.user.reset_password", uuid, fmt.Errorf("uuid empty"))
        response.HandleBadRequest(c, "用户ID不能为空", nil)
        return
    }

    var req struct {
        NewPassword string `json:"newPassword" binding:"required,min=6"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.user.reset_password", uuid, err)
        response.HandleBadRequest(c, "新密码至少6位", nil)
        return
    }

    callerUUID := c.GetString(string(constants.ContextKeyUserUUID))
    if err := h.adminService.ResetPassword(callerUUID, uuid, req.NewPassword); err != nil {
        msg := err.Error()
        if strings.Contains(msg, "不存在") {
            middleware.LogOperation(c, "admin.user.reset_password", uuid, err)
            response.HandleBadRequest(c, msg, nil)
            return
        }
        middleware.LogOperation(c, "admin.user.reset_password", uuid, err)
        response.HandleInternalServerError(c, msg)
        return
    }

    middleware.LogOperation(c, "admin.user.reset_password", uuid, nil)
    response.HandleSuccess(c, http.StatusOK, "密码重置成功", nil)
}

// SetUserDisabledHandler 处理禁用/启用用户
func (h *Handler) SetUserDisabledHandler(c *gin.Context) {
    uuid := c.Param("uuid")
    if uuid == "" {
        middleware.LogOperation(c, "admin.user.set_disabled", uuid, fmt.Errorf("uuid empty"))
        response.HandleBadRequest(c, "用户ID不能为空", nil)
        return
    }

    var req struct {
        Disabled bool `json:"disabled"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.user.set_disabled", uuid, err)
        response.HandleBadRequest(c, "请求参数错误", nil)
        return
    }

    if err := h.adminService.ToggleUserStatus(uuid, req.Disabled); err != nil {
        msg := err.Error()
        if strings.Contains(msg, "不存在") {
            middleware.LogOperation(c, "admin.user.set_disabled", uuid, err)
            response.HandleBadRequest(c, msg, nil)
            return
        }
        middleware.LogOperation(c, "admin.user.set_disabled", uuid, err)
        response.HandleBadRequest(c, msg, nil)
        return
    }

    msg := "用户已禁用"
    op := "admin.user.disable"
    if !req.Disabled {
        msg = "用户已启用"
        op = "admin.user.enable"
    }
    middleware.LogOperation(c, op, uuid, nil)
    response.HandleSuccess(c, http.StatusOK, msg, nil)
}

// DeleteUserHandler 处理删除用户
func (h *Handler) DeleteUserHandler(c *gin.Context) {
    uuid := c.Param("uuid")
    if uuid == "" {
        middleware.LogOperation(c, "admin.user.delete", uuid, fmt.Errorf("uuid empty"))
        response.HandleBadRequest(c, "用户ID不能为空", nil)
        return
    }

    if err := h.adminService.DeleteUser(uuid); err != nil {
        msg := err.Error()
        if strings.Contains(msg, "不存在") {
            middleware.LogOperation(c, "admin.user.delete", uuid, err)
            response.HandleBadRequest(c, msg, nil)
            return
        }
        middleware.LogOperation(c, "admin.user.delete", uuid, err)
        response.HandleBadRequest(c, msg, nil)
        return
    }

    middleware.LogOperation(c, "admin.user.delete", uuid, nil)
    response.HandleSuccess(c, http.StatusOK, "用户已删除", nil)
    }

// SessionsHandler 处理获取上传会话列表
func (h *Handler) SessionsHandler(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

    result, err := h.adminService.ListSessions(page, pageSize)
    if err != nil {
        response.HandleInternalServerError(c, err.Error())
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", result)
}

// CleanupSessionsHandler 清理指定的会话
func (h *Handler) CleanupSessionsHandler(c *gin.Context) {
    var req struct {
        SessionIDs []uint `json:"sessionIds" binding:"required"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.session.cleanup", fmt.Sprintf("%v", req.SessionIDs), err)
        response.HandleBadRequest(c, "请求格式错误", nil)
        return
    }

    cleanedCount, err := h.adminService.CleanupSessions(req.SessionIDs)
    if err != nil {
        middleware.LogOperation(c, "admin.session.cleanup", fmt.Sprintf("%v", req.SessionIDs), err)
        response.HandleInternalServerError(c, "清理会话失败")
        return
    }

    middleware.LogOperation(c, "admin.session.cleanup", fmt.Sprintf("cleaned %d sessions", cleanedCount), nil)
    response.HandleSuccess(c, http.StatusOK, fmt.Sprintf("成功清理 %d 个会话", cleanedCount), nil)
    }

// UploadCertHandler 处理上传 TLS 证书文件
func (h *Handler) UploadCertHandler(c *gin.Context) {
    file, err := c.FormFile("file")
    if err != nil {
        middleware.LogOperation(c, "admin.cert.upload", fmt.Sprintf("%s", file.Filename), fmt.Errorf("no file"))
        response.HandleBadRequest(c, "未上传文件", nil)
        return
    }

    // 验证文件扩展名
    ext := strings.ToLower(filepath.Ext(file.Filename))
    if ext != ".pem" && ext != ".crt" {
        middleware.LogOperation(c, "admin.cert.upload", file.Filename, fmt.Errorf("invalid extension: %s", ext))
        response.HandleBadRequest(c, "仅支持 .pem 或 .crt 格式的证书文件", nil)
        return
    }

    // 保存到配置目录下的 certs 子目录
    certsDir := filepath.Join(appconfig.WorkDir, "certs")
    if err := os.MkdirAll(certsDir, 0755); err != nil {
        middleware.LogOperation(c, "admin.cert.upload", certsDir, err)
        response.HandleInternalServerError(c, "创建证书目录失败")
        return
    }

    // 使用固定文件名，便于管理
    destPath := filepath.Join(certsDir, "server.pem")

    // 如果已存在旧证书，先删除
    if _, err := os.Stat(destPath); err == nil {
        os.Remove(destPath)
    }

    // 保存文件
    if err := c.SaveUploadedFile(file, destPath); err != nil {
        middleware.LogOperation(c, "admin.cert.upload", destPath, err)
        response.HandleInternalServerError(c, "保存证书文件失败")
        return
    }

    middleware.LogOperation(c, "admin.cert.upload", destPath, nil)
    response.HandleSuccess(c, http.StatusOK, "证书上传成功", gin.H{
        "path": destPath,
    })
}

// UploadKeyHandler 处理上传 TLS 密钥文件
func (h *Handler) UploadKeyHandler(c *gin.Context) {
    file, err := c.FormFile("file")
    if err != nil {
        middleware.LogOperation(c, "admin.key.upload", fmt.Sprintf("%s", file.Filename), fmt.Errorf("no file"))
        response.HandleBadRequest(c, "未上传文件", nil)
        return
    }

    // 验证文件扩展名
    ext := strings.ToLower(filepath.Ext(file.Filename))
    if ext != ".key" {
        middleware.LogOperation(c, "admin.key.upload", file.Filename, fmt.Errorf("invalid extension: %s", ext))
        response.HandleBadRequest(c, "仅支持 .key 格式的密钥文件", nil)
        return
    }

    // 保存到配置目录下的 certs 子目录
    certsDir := filepath.Join(appconfig.WorkDir, "certs")
    if err := os.MkdirAll(certsDir, 0755); err != nil {
        middleware.LogOperation(c, "admin.key.upload", certsDir, err)
        response.HandleInternalServerError(c, "创建证书目录失败")
        return
    }

    // 使用固定文件名，便于管理
    destPath := filepath.Join(certsDir, "server.key")

    // 如果已存在旧密钥，先删除
    if _, err := os.Stat(destPath); err == nil {
        os.Remove(destPath)
    }

    // 保存文件
    if err := c.SaveUploadedFile(file, destPath); err != nil {
        middleware.LogOperation(c, "admin.key.upload", destPath, err)
        response.HandleInternalServerError(c, "保存密钥文件失败")
        return
    }

    middleware.LogOperation(c, "admin.key.upload", destPath, nil)
    response.HandleSuccess(c, http.StatusOK, "密钥上传成功", gin.H{
        "path": destPath,
    })
}

// ==================== 数据库迁移相关 Handler ====================

// testConnectionRequest 连接测试请求
type testConnectionRequest struct {
    Driver string `json:"driver" binding:"required,oneof=sqlite mysql postgres"`
    DSN    string `json:"dsn" binding:"required,max=1024"`
}

// migrationCheckRequest 迁移检查请求
type migrationCheckRequest struct {
    SourceDriver string `json:"sourceDriver"`
    SourceDSN    string `json:"sourceDSN" binding:"max=1024"`
    TargetDriver string `json:"targetDriver" binding:"required,oneof=sqlite mysql postgres"`
    TargetDSN    string `json:"targetDSN" binding:"required,max=1024"`
}

// migrateRequest 迁移请求
type migrateRequest struct {
    SourceDriver string `json:"sourceDriver"`
    SourceDSN    string `json:"sourceDSN" binding:"max=1024"`
    TargetDriver string `json:"targetDriver" binding:"required,oneof=sqlite mysql postgres"`
    TargetDSN    string `json:"targetDSN" binding:"required,max=1024"`
    Overwrite    bool   `json:"overwrite"`
    CreateBackup bool   `json:"createBackup"`
}

// TestConnection 测试数据库连接
func (h *Handler) TestConnection(c *gin.Context) {
    var req testConnectionRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求参数错误: "+err.Error(), nil)
        return
    }

    // 创建对应的迁移器
    var migrator migration.DatabaseMigrator
    var err error

    switch req.Driver {
    case "sqlite":
        migrator, err = migration.NewSQLiteMigrator(req.DSN)
    case "mysql":
        migrator, err = migration.NewMySQLMigrator(req.DSN)
    case "postgres":
        migrator, err = migration.NewPostgresMigrator(req.DSN)
    default:
        response.HandleBadRequest(c, "不支持的数据库类型: "+req.Driver, nil)
        return
    }

    if err != nil {
        response.HandleSuccess(c, http.StatusOK, "", gin.H{
            "connected":      false,
            "version":        "",
            "tables":         []string{},
            "databaseEmpty":  true,
            "estimatedSize":  "0 KB",
            "error":          err.Error(),
        })
        return
    }
    defer migrator.Close()

    // 测试连接
    info, err := migrator.TestConnection(req.DSN)
    if err != nil || !info.Connected {
        response.HandleSuccess(c, http.StatusOK, "", gin.H{
            "connected":     false,
            "version":       "",
            "tables":        []string{},
            "databaseEmpty": true,
            "estimatedSize": "0 KB",
            "error":         err.Error(),
        })
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", info)
}

// MigrationCheck 检查迁移可行性
func (h *Handler) MigrationCheck(c *gin.Context) {
    var req migrationCheckRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求参数错误: "+err.Error(), nil)
        return
    }

    // 创建迁移服务
    backupDir := appconfig.GetBackupsDir()
    migrationService := migration.NewMigrationService(h.db, backupDir)

    // 获取当前数据库配置
    currentDSN := appconfig.GlobalConfig.Database.DSN
    if req.SourceDSN != "" {
        currentDSN = req.SourceDSN
    }

    result, err := migrationService.CheckMigration(currentDSN, req.TargetDriver, req.TargetDSN)
    if err != nil {
        response.HandleErrorCompat(c, http.StatusInternalServerError, "迁移检查失败: "+err.Error(), nil)
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", result)
}

// StartMigration 开始数据库迁移 (SSE)
func (h *Handler) StartMigration(c *gin.Context) {
    var req migrateRequest
    if err := c.ShouldBindJSON(&req); err != nil {
            middleware.LogOperation(c, "admin.migration.start", fmt.Sprintf("%s -> %s", appconfig.GlobalConfig.Database.DSN, req.TargetDSN), err)
        response.HandleBadRequest(c, "请求参数错误: "+err.Error(), nil)
        return
    }

    // 获取当前数据库配置
    currentDSN := appconfig.GlobalConfig.Database.DSN
    if req.SourceDSN != "" {
        currentDSN = req.SourceDSN
    }
    if req.SourceDriver == "" {
        req.SourceDriver = appconfig.GlobalConfig.Database.Driver
    }

    // 创建迁移服务
    backupDir := appconfig.GetBackupsDir()
    migrationService := migration.NewMigrationService(h.db, backupDir)

    // 配置 SSE 响应头
    middleware.LogOperation(c, "admin.migration.start", fmt.Sprintf("%s -> %s", currentDSN, req.TargetDSN), nil)
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")
    c.Header("Transfer-Encoding", "chunked")
    c.Header("X-Accel-Buffering", "no")

    // 创建取消上下文
    ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
    defer cancel()

    // 启动迁移
    eventCh, err := migrationService.StartMigration(ctx, currentDSN, req.TargetDriver, req.TargetDSN, &migration.MigrationOptions{
        Overwrite:    req.Overwrite,
        CreateBackup: req.CreateBackup,
    })

    if err != nil {
        // 检查是否是锁冲突
        if strings.Contains(err.Error(), "已有迁移任务正在执行") {
            middleware.LogOperation(c, "admin.migration.start", fmt.Sprintf("%s -> %s", appconfig.GlobalConfig.Database.DSN, req.TargetDSN), err)
            utils.Warn("迁移锁冲突",
                utils.String("path", c.Request.URL.Path),
                utils.Err(err))
            response.HandleError(c, http.StatusConflict, 409, err.Error(), nil)
            return
        }
        middleware.LogOperation(c, "admin.migration.start", fmt.Sprintf("%s -> %s", appconfig.GlobalConfig.Database.DSN, req.TargetDSN), err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
        return
    }

    // 流式发送事件
    c.Writer.WriteHeader(http.StatusOK)
    c.Writer.Flush()

    flusher, ok := c.Writer.(http.Flusher)
    if !ok {
            middleware.LogOperation(c, "admin.migration.start", fmt.Sprintf("%s -> %s", currentDSN, req.TargetDSN), fmt.Errorf("SSE not supported"))
        response.HandleErrorCompat(c, http.StatusInternalServerError, "SSE 不支持", nil)
        return
    }

    for {
        select {
        case event, ok := <-eventCh:
            if !ok {
                return
            }
            data, _ := json.Marshal(event)
            fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
            flusher.Flush()
        case <-c.Request.Context().Done():
            // 客户端断开连接，取消迁移任务
            cancel()
            return
        }
    }
}

// GetMigrationStatus 获取迁移状态
func (h *Handler) GetMigrationStatus(c *gin.Context) {
    // 创建迁移服务
    backupDir := appconfig.GetBackupsDir()
    migrationService := migration.NewMigrationService(h.db, backupDir)

    // 查询最新迁移记录
    migrations, err := migrationService.ListMigrations(10)
    if err != nil {
        response.HandleErrorCompat(c, http.StatusInternalServerError, "获取迁移状态失败: "+err.Error(), nil)
        return
    }

    // 获取当前锁状态
    lockStatus := gin.H{
        "locked":      migration.GlobalMigrationLock.IsLocked(),
        "lockHolder":  migration.GlobalMigrationLock.GetLockHolder(),
        "lockAge":     migration.GlobalMigrationLock.GetLockAge().String(),
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "migrations": migrations,
        "lockStatus": lockStatus,
    })
}

// CancelMigration 取消迁移任务
func (h *Handler) CancelMigration(c *gin.Context) {
    // 检查是否有迁移任务在运行
    if !migration.GlobalMigrationLock.IsLocked() {
        response.HandleSuccess(c, http.StatusOK, "没有正在运行的迁移任务", nil)
        return
    }

    // 获取锁持有者
    lockHolder := migration.GlobalMigrationLock.GetLockHolder()

    // 强制释放锁
    migration.GlobalMigrationLock.ForceRelease()

    middleware.LogOperation(c, "admin.migration.cancel", lockHolder, nil)
    response.HandleSuccess(c, http.StatusOK, fmt.Sprintf("迁移任务 %s 已取消", lockHolder), nil)
    }

// ResumeMigration 继续迁移
func (h *Handler) ResumeMigration(c *gin.Context) {
    var req struct {
        MigrationID string `json:"migrationId"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.migration.resume", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusBadRequest, "无效的请求参数", err)
        return
    }

    // 检测中断的迁移
    interrupted, err := h.migrationRecoveryService.DetectInterruptedMigration()
    if err != nil {
        middleware.LogOperation(c, "admin.migration.resume", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "检测中断迁移失败", err)
        return
    }

    if interrupted == nil || interrupted.MigrationID != req.MigrationID {
        middleware.LogOperation(c, "admin.migration.resume", req.MigrationID, fmt.Errorf("not found"))
        response.HandleErrorCompat(c, http.StatusNotFound, "未找到需要恢复的迁移", nil)
        return
    }

    // 继续迁移
    _, err = h.migrationRecoveryService.ResumeMigration(c.Request.Context(), req.MigrationID, "", "")
    if err != nil {
        middleware.LogOperation(c, "admin.migration.resume", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "继续迁移失败", err)
        return
    }

    middleware.LogOperation(c, "admin.migration.resume", req.MigrationID, nil)
    response.HandleSuccess(c, http.StatusOK, "继续迁移已启动", nil)
    }

// RestartMigration 重新迁移
func (h *Handler) RestartMigration(c *gin.Context) {
    var req struct {
        MigrationID string `json:"migrationId"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.migration.restart", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusBadRequest, "无效的请求参数", err)
        return
    }

    // 重置迁移状态
    err := h.migrationRecoveryService.ResetMigrationStatus(req.MigrationID)
    if err != nil {
        middleware.LogOperation(c, "admin.migration.restart", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "重置迁移状态失败", err)
        return
    }

    // 重新迁移
    _, err = h.migrationRecoveryService.RestartMigration(c.Request.Context(), req.MigrationID, "", "")
    if err != nil {
        middleware.LogOperation(c, "admin.migration.restart", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "重新迁移失败", err)
        return
    }

    middleware.LogOperation(c, "admin.migration.restart", req.MigrationID, nil)
    response.HandleSuccess(c, http.StatusOK, "重新迁移已启动", nil)
    }

// RollbackMigration 回滚迁移
func (h *Handler) RollbackMigration(c *gin.Context) {
    var req struct {
        MigrationID string `json:"migrationId"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.migration.rollback", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusBadRequest, "无效的请求参数", err)
        return
    }

    // 执行回滚
    result, err := h.migrationRollbackService.Rollback(req.MigrationID)
    if err != nil {
        middleware.LogOperation(c, "admin.migration.rollback", req.MigrationID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "回滚失败", err)
        return
    }

    middleware.LogOperation(c, "admin.migration.rollback", req.MigrationID, nil)
    response.HandleSuccess(c, http.StatusOK, "回滚成功", result)
    }

// ==================== 备份相关 Handler ====================

// backupRequest 备份请求
type backupRequest struct {
    Description string `json:"description" binding:"max=500"`
}

// restoreRequest 恢复请求
type restoreRequest struct {
    BackupID string `json:"backupId" binding:"required,max=64"`
}

// ListBackups 列出所有备份
func (h *Handler) ListBackups(c *gin.Context) {
    backupDir := appconfig.GetBackupsDir()
    backupService := migration.NewBackupService(backupDir, appconfig.GlobalConfig.Database.Driver)

    backups, err := backupService.ListBackups()
    if err != nil {
        response.HandleErrorCompat(c, http.StatusInternalServerError, "获取备份列表失败: "+err.Error(), nil)
        return
    }

    // 转换为前端期望的格式
    result := make([]gin.H, 0, len(backups))
    for _, b := range backups {
        result = append(result, gin.H{
            "id":          b.ID,
            "fileName":    b.FileName,
            "description": b.Description,
            "size":        b.Size,
            "createdAt":   b.CreatedAt.Format("2006-01-02 15:04:05"),
            "status":      b.Status,
            "driver":      b.Driver,
        })
    }

    response.HandleSuccess(c, http.StatusOK, "", result)
}

// CreateBackup 创建备份
func (h *Handler) CreateBackup(c *gin.Context) {
    var req backupRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求参数格式错误", err.Error())
        return
    }

    backupDir := appconfig.GetBackupsDir()

    // 创建源数据库迁移器
    migrator, err := createSourceMigrator(appconfig.GlobalConfig.Database.DSN, appconfig.GlobalConfig.Database.Driver)
    if err != nil {
        middleware.LogOperation(c, "admin.backup.create", fmt.Sprintf("%s", req.Description), err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "创建迁移器失败: "+err.Error(), nil)
        return
    }
    defer migrator.Close()

    backupService := migration.NewBackupService(backupDir, appconfig.GlobalConfig.Database.Driver)

    backup, err := backupService.CreateBackup(migrator, req.Description)
    if err != nil {
        middleware.LogOperation(c, "admin.backup.create", fmt.Sprintf("%s", req.Description), err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "创建备份失败: "+err.Error(), nil)
        return
    }

    middleware.LogOperation(c, "admin.backup.create", backup.ID, nil)
    response.HandleSuccess(c, http.StatusOK, "备份创建成功", gin.H{
        "id":         backup.ID,
        "fileName":   backup.FileName,
        "size":       backup.Size,
        "createdAt":  backup.CreatedAt.Format("2006-01-02 15:04:05"),
        "status":     backup.Status,
    })
}

// RestoreBackup 恢复备份
func (h *Handler) RestoreBackup(c *gin.Context) {
    var req restoreRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        middleware.LogOperation(c, "admin.backup.restore", req.BackupID, err)
        response.HandleBadRequest(c, "请提供备份ID", nil)
        return
    }

    backupDir := appconfig.GetBackupsDir()
    backupService := migration.NewBackupService(backupDir, appconfig.GlobalConfig.Database.Driver)

    // 检查锁
    if migration.GlobalMigrationLock.IsLocked() {
        middleware.LogOperation(c, "admin.backup.restore", req.BackupID, fmt.Errorf("operation in progress"))
        utils.Warn("恢复备份锁冲突: 有其他操作正在进行",
            utils.String("path", c.Request.URL.Path))
        response.HandleError(c, http.StatusConflict, 409, "有其他操作正在进行", nil)
        return
    }

    // 获取锁
    if !migration.GlobalMigrationLock.AcquireLock("restore") {
        middleware.LogOperation(c, "admin.backup.restore", req.BackupID, fmt.Errorf("cannot acquire lock"))
        utils.Warn("恢复备份锁冲突: 无法获取操作锁",
            utils.String("path", c.Request.URL.Path))
        response.HandleError(c, http.StatusConflict, 409, "无法获取操作锁", nil)
        return
    }
    defer migration.GlobalMigrationLock.ReleaseLock("restore")

    // 创建目标数据库迁移器
    migrator, err := createSourceMigrator(appconfig.GlobalConfig.Database.DSN, appconfig.GlobalConfig.Database.Driver)
    if err != nil {
        middleware.LogOperation(c, "admin.backup.restore", req.BackupID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "创建迁移器失败: "+err.Error(), nil)
        return
    }
    defer migrator.Close()

    if err := backupService.RestoreBackup(req.BackupID, migrator); err != nil {
        middleware.LogOperation(c, "admin.backup.restore", req.BackupID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "恢复备份失败: "+err.Error(), nil)
        return
    }

    middleware.LogOperation(c, "admin.backup.restore", req.BackupID, nil)
    response.HandleSuccess(c, http.StatusOK, "备份恢复成功", nil)
    }

// DeleteBackup 删除备份
func (h *Handler) DeleteBackup(c *gin.Context) {
    backupID := c.Param("id")
    if backupID == "" {
        middleware.LogOperation(c, "admin.backup.delete", backupID, fmt.Errorf("empty id"))
        response.HandleBadRequest(c, "请提供备份ID", nil)
        return
    }

    backupDir := appconfig.GetBackupsDir()
    backupService := migration.NewBackupService(backupDir, appconfig.GlobalConfig.Database.Driver)

    if err := backupService.DeleteBackup(backupID); err != nil {
        middleware.LogOperation(c, "admin.backup.delete", backupID, err)
        response.HandleErrorCompat(c, http.StatusInternalServerError, "删除备份失败: "+err.Error(), nil)
        return
    }

    middleware.LogOperation(c, "admin.backup.delete", backupID, nil)
    response.HandleSuccess(c, http.StatusOK, "备份已删除", nil)
	}

// ClearRecordsHandler 清空指定类型的操作记录
func (h *Handler) ClearRecordsHandler(c *gin.Context) {
	var req struct {
		Action string `json:"action" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.LogOperation(c, "admin.records.clear", req.Action, err)
		response.HandleBadRequest(c, "请提供要清空的记录类型 (search/upload/download)", nil)
		return
	}

	validActions := map[string]string{
		"search":  "search",
		"upload":  "upload",
		"download": "download",
	}
	if _, ok := validActions[req.Action]; !ok {
		middleware.LogOperation(c, "admin.records.clear", req.Action, fmt.Errorf("invalid action"))
		response.HandleBadRequest(c, "无效的记录类型，有效值: search, upload, download", nil)
		return
	}

	repo := repositories.NewRecordRepository(h.db)
	count, err := repo.DeleteByAction(req.Action)
	if err != nil {
		middleware.LogOperation(c, "admin.records.clear", req.Action, err)
		response.HandleInternalServerError(c, "清空记录失败: "+err.Error())
		return
	}

	middleware.LogOperation(c, "admin.records.clear", req.Action, nil)
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"action": req.Action,
		"count":  count,
	})
}

// createSourceMigrator 创建源数据库迁移器
func createSourceMigrator(dsn, driver string) (migration.DatabaseMigrator, error) {
    switch driver {
    case "sqlite":
        return migration.NewSQLiteMigrator(dsn)
    case "mysql":
        return migration.NewMySQLMigrator(dsn)
    case "postgres":
        return migration.NewPostgresMigrator(dsn)
    default:
        return migration.NewSQLiteMigrator(dsn)
    }
}