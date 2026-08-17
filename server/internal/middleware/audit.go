package middleware

import (
    "fmt"
    "time"

    "github.com/gin-gonic/gin"
    "go.uber.org/zap"
    "fuzhan/internal/constants"
    "fuzhan/internal/utils"
)

// auditCtxKeys 审计上下文键名
const (
    auditKeyStartTime  = "auditStartTime"
    auditKeyClientIP   = "auditClientIP"
    auditKeyOperator   = "auditOperator"
    auditKeyLogged     = "auditLogged"
    auditKeyRequestID  = string(constants.ContextKeyRequestID)
)

// AuditMiddleware 审计日志中间件
// 自动捕获请求开始时间、客户端IP，在请求完成后记录耗时
// 不直接产生日志，由 Handler 调用 LogOperation 触发
func AuditMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
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

    utils.Info("操作审计", fields...)
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
