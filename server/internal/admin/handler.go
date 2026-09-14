package admin

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/middleware"
	"fuzhan/internal/online"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler 管理员处理器
type Handler struct {
	adminService *services.AdminService
	db           *gorm.DB
}

// NewHandler 创建管理员处理器
func NewHandler(adminService *services.AdminService, db *gorm.DB) *Handler {
	return &Handler{
		adminService: adminService,
		db:           db,
	}
}

// OnlineIPs 获取当前在线 IP 列表（依据最近请求判定，与登录无关）
// 支持可选 limit 参数（在线 IP 较多时避免一次性返回全量），未传则不限制。
func (h *Handler) OnlineIPs(c *gin.Context) {
	now := utils.Now()
	entries := online.Default.Snapshot(now)
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit < len(entries) {
			entries = entries[:limit]
		}
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"online":             entries,
		"idleTimeoutSeconds": int(online.IdleTimeout.Seconds()),
		"serverTime":         now.Format(time.RFC3339),
	})
}

// UsersHandler 处理获取用户列表
func (h *Handler) UsersHandler(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	query := c.Query("query")

	result, err := h.adminService.ListUsers(page, pageSize, query)
	if err != nil {
		utils.Error("获取用户列表失败", utils.String("query", query), utils.Err(err))
		response.HandleInternalServerError(c, "获取用户列表失败")
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
		utils.Error("获取会话列表失败", utils.Err(err))
		response.HandleInternalServerError(c, "获取会话列表失败")
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
		"search":   "search",
		"upload":   "upload",
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
		utils.Error("清空操作记录失败", utils.String("action", req.Action), utils.Err(err))
		response.HandleInternalServerError(c, "清空记录失败")
		return
	}

	middleware.LogOperation(c, "admin.records.clear", req.Action, nil)
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"action": req.Action,
		"count":  count,
	})
}

// DeleteRecordHandler 删除单条操作记录（管理员）
func (h *Handler) DeleteRecordHandler(c *gin.Context) {
	var req struct {
		ID     uint   `json:"id" binding:"required"`
		Action string `json:"action" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.LogOperation(c, "admin.records.delete", fmt.Sprintf("%d/%s", req.ID, req.Action), err)
		response.HandleBadRequest(c, "请提供记录ID和记录类型 (search/upload/download)", nil)
		return
	}

	validActions := map[string]bool{
		"search":   true,
		"upload":   true,
		"download": true,
	}
	if !validActions[req.Action] {
		middleware.LogOperation(c, "admin.records.delete", fmt.Sprintf("%d/%s", req.ID, req.Action), fmt.Errorf("invalid action"))
		response.HandleBadRequest(c, "无效的记录类型，有效值: search, upload, download", nil)
		return
	}

	repo := repositories.NewRecordRepository(h.db)
	count, err := repo.DeleteByIDAndAction(req.ID, req.Action)
	if err != nil {
		middleware.LogOperation(c, "admin.records.delete", fmt.Sprintf("%d/%s", req.ID, req.Action), err)
		utils.Error("删除操作记录失败", utils.Int64("id", int64(req.ID)), utils.String("action", req.Action), utils.Err(err))
		response.HandleInternalServerError(c, "删除记录失败")
		return
	}
	if count == 0 {
		middleware.LogOperation(c, "admin.records.delete", fmt.Sprintf("%d/%s", req.ID, req.Action), fmt.Errorf("record not found"))
		response.HandleBadRequest(c, "记录不存在或类型不匹配", nil)
		return
	}

	middleware.LogOperation(c, "admin.records.delete", fmt.Sprintf("%d/%s", req.ID, req.Action), nil)
	response.HandleSuccess(c, http.StatusOK, "删除成功", gin.H{
		"id":     req.ID,
		"action": req.Action,
	})
}
