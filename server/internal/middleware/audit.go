package middleware

import (
	"fmt"
	"net/http"
	"time"

	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/security"
	"fuzhan/internal/utils"
	"fuzhan/internal/webhook"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// auditCtxKeys 审计上下文键名
const (
	auditKeyStartTime = "auditStartTime"
	auditKeyClientIP  = "auditClientIP"
	auditKeyOperator  = "auditOperator"
	auditKeyLogged    = "auditLogged"
	auditKeyRequestID = string(constants.ContextKeyRequestID)
)

// AuditMiddleware 审计日志中间件
// 自动捕获请求开始时间、客户端IP，在请求完成后记录耗时
// 不直接产生日志，由 Handler 调用 LogOperation 触发
func AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := utils.Now()
		clientIP := utils.GetClientIP(c)

		c.Set(auditKeyStartTime, start)
		c.Set(auditKeyClientIP, clientIP)

		// 预先设置操作者（匿名），后续 Auth 中间件会覆盖
		c.Set(auditKeyOperator, "anonymous")

		c.Next()

		// 如果请求被中间件中止（如认证失败）且没有 handler 记录审计日志，
		// 自动记录一条拒绝请求的审计日志
		if c.IsAborted() {
			if _, logged := c.Get(auditKeyLogged); !logged {
				statusCode := c.Writer.Status()
				LogOperation(c, "request.rejected",
					fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path),
					fmt.Errorf("status %d", statusCode))
			}
			// 被拒绝的风险请求（401 越权/403 禁止/429 限流）写入安全事件库
			recordRejectedSecurityEvent(c)
		}
	}
}

// getAuditOperator 获取操作者身份：已认证用户返回用户名，否则返回 anonymous
func getAuditOperator(c *gin.Context) string {
	// 优先从 Gin Context 获取已认证的用户信息
	if username, err := GetUsername(c); err == nil && username != "" {
		return username
	}
	// 回退到中间件设置的默认值
	if op, exists := c.Get(auditKeyOperator); exists {
		if s, ok := op.(string); ok {
			return s
		}
	}
	return "anonymous"
}

// safeErrString 安全获取错误字符串
func safeErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// LogOperation 记录操作审计日志
// 由 Handler 在关键业务操作处调用
//   - c: Gin 上下文
//   - operation: 操作类型，如 "file.upload", "auth.login"
//   - target: 操作对象，如文件名、路径
//   - err: 操作错误，nil 表示成功
func LogOperation(c *gin.Context, operation, target string, err error) {
	operator := getAuditOperator(c)
	clientIP := getClientIP(c)
	duration := getDuration(c)
	result := "success"
	errStr := safeErrString(err)
	if errStr != "" {
		result = "failure"
	}

	fields := []zap.Field{
		zap.Bool("audit", true),
		zap.String("operator", operator),
		zap.String("client_ip", clientIP),
		zap.String("operation", operation),
		zap.String("target", target),
		zap.String("result", result),
		zap.Int("status_code", c.Writer.Status()),
		zap.Int64("duration_ms", duration),
	}

	// 添加请求 ID（如果存在）
	if requestID, exists := c.Get(auditKeyRequestID); exists {
		if id, ok := requestID.(string); ok && id != "" {
			fields = append(fields, zap.String("request_id", id))
		}
	}

	if errStr != "" {
		fields = append(fields, zap.String("error", errStr))
	}

	// 标记已记录审计日志，避免 middleware 重复记录
	c.Set(auditKeyLogged, true)

	// 登录 / 注册事件分流到独立日志文件（login.log / register.log），
	// 便于分别审计与排查；其余操作仍写入应用日志（app.log）。
	switch operation {
	case "auth.login", "auth.oauth2.login":
		utils.Login("操作审计", fields...)
	case "auth.register":
		utils.Register("操作审计", fields...)
	default:
		utils.Info("操作审计", fields...)
	}

	// Webhook 事件派发：把 operation 映射为事件类型并异步推送。
	// 此处集中映射，避免改动 70+ 个 LogOperation 调用点。
	// config.update / search / request.rejected 明确不派发（配置变更不再推送，
	// security.event 已覆盖安全类拒绝，search 为高频只读噪音）。
	dispatchWebhookEvent(operation, err, map[string]interface{}{
		"operator":    operator,
		"clientIp":    clientIP,
		"operation":   operation,
		"target":      target,
		"result":      result,
		"statusCode":  c.Writer.Status(),
		"durationMs":  duration,
	})
}

// dispatchWebhookEvent 将审计 operation 映射为 Webhook 事件并异步派发。
// 事件名代表「已完成」的动作，除登录外仅在 err == nil 时派发；
// 登录成功/失败分别派发，便于接收方准确告警。
func dispatchWebhookEvent(operation string, err error, payload map[string]interface{}) {
	if !webhook.Enabled() {
		return
	}
	// 明确不派发的 operation
	switch operation {
	case "config.update", "search", "request.rejected":
		return
	}

	// operation -> 事件类型 映射
	var event string
	ok := false
	switch operation {
	case "upload.session.create":
		event, ok = "file.upload_started", true
	case "upload.session.cancel":
		event, ok = "file.upload_canceled", true
	case "upload.session.finalize":
		event, ok = "file.uploaded", true
	case "file.rename":
		event, ok = "file.renamed", true
	case "file.move":
		event, ok = "file.moved", true
	case "file.delete":
		event, ok = "file.deleted", true
	case "private.download":
		event, ok = "private.downloaded", true
	case "private.delete":
		event, ok = "private.deleted", true
	case "temp.download":
		event, ok = "temp.downloaded", true
	case "temp.delete":
		event, ok = "temp.deleted", true
	case "temp.upload.session.cancel":
		event, ok = "temp.upload_canceled", true
	case "upload.url.cancel":
		event, ok = "url_task.canceled", true
	case "upload.url.retry":
		event, ok = "url_task.retried", true
	case "upload.url.delete":
		event, ok = "url_task.deleted", true
	case "upload.url.checksum":
		event, ok = "url_task.checksummed", true
	case "auth.register":
		event, ok = "user.registered", true
	case "admin.user.create":
		event, ok = "user.created", true
	case "admin.user.delete":
		event, ok = "user.deleted", true
	case "admin.user.set_disabled", "admin.user.disable", "admin.user.enable":
		event, ok = "user.status_changed", true
	case "auth.change_password", "admin.user.reset_password":
		event, ok = "user.password_changed", true
	case "auth.login", "auth.oauth2.login":
		// 登录成功/失败分别派发，便于接收方准确告警（不受下方成功才派发规则约束）
		event := "auth.login_success"
		if err != nil {
			event = "auth.login_failed"
		}
		webhook.Dispatch(event, "audit", payload)
		return
	}
	if !ok {
		return
	}

	// 非登录事件：仅成功时派发（事件名代表已完成动作）
	if err != nil {
		return
	}
	webhook.Dispatch(event, "audit", payload)
}

// getClientIP 从 Gin Context 获取客户端 IP
func getClientIP(c *gin.Context) string {
	if ip, exists := c.Get(auditKeyClientIP); exists {
		if s, ok := ip.(string); ok {
			return s
		}
	}
	return utils.GetClientIP(c)
}

// getDuration 获取请求耗时（毫秒）
func getDuration(c *gin.Context) int64 {
	if start, exists := c.Get(auditKeyStartTime); exists {
		if t, ok := start.(time.Time); ok {
			return time.Since(t).Milliseconds()
		}
	}
	return 0
}

// recordRejectedSecurityEvent 将被拒绝（abort）的越权/禁止/限流请求写入安全事件库。
// 仅对状态码命中风险集合（401/403/429）的请求登记，普通响应不产生安全事件以避免噪音。
func recordRejectedSecurityEvent(c *gin.Context) {
	status := c.Writer.Status()
	// 降噪：前端 token 过期导致的自动轮询 GET 401 不是攻击行为，不入库
	// （写操作 401、登录/注册、WebDAV 401、403/429 全部保留）
	if status == http.StatusUnauthorized && isNonActivityRequest(c.Request.Method, c.Request.URL.Path) {
		return
	}
	var eventType, severity string
	switch status {
	case http.StatusUnauthorized:
		eventType = models.EventAuthUnauthorized
		severity = models.SeverityMedium
	case http.StatusForbidden:
		eventType = models.EventAccessForbidden
		severity = models.SeverityMedium
	case http.StatusTooManyRequests:
		eventType = models.EventRateLimited
		severity = models.SeverityLow
	default:
		return
	}

	// 防护渠道：优先取被拒中间件打的上下文标记（如 WebDAV），缺省 auth
	guard := models.GuardAuth
	if v, exists := c.Get(string(constants.ContextKeySecurityGuard)); exists {
		if s, ok := v.(string); ok && s != "" {
			guard = s
		}
	}

	ev := &models.SecurityEvent{
		EventType: eventType,
		Severity:  severity,
		Target:    c.Request.Method + " " + c.Request.URL.Path,
		Guard:     guard,
		Reason:    fmt.Sprintf("请求被拒绝（HTTP %d）", status),
	}
	security.RecordFromGin(c, ev)
}
