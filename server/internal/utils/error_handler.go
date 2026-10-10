package utils

import (
	"net/http"

	"fuzhan/internal/constants"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 错误码定义
const (
	CodeSuccess            = 0
	CodeBadRequest         = 1004
	CodeUnauthorized       = 2001
	CodeForbidden          = 2004
	CodeNotFound           = 3001
	CodeFileTooLarge       = 3002
	CodeQuotaExceeded      = 3003
	CodeFileTypeNotAllowed = 3004
	CodeInternalError      = 5001
)

// ErrorResponse 统一错误响应结构
type ErrorResponse struct {
	Success bool        `json:"success"`
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Errors  interface{} `json:"errors,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// SuccessResponse 统一成功响应结构
type SuccessResponse struct {
	Success bool        `json:"success"`
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// HandleSuccess 处理成功响应
func HandleSuccess(c *gin.Context, statusCode int, message string, data interface{}) {
	c.JSON(statusCode, SuccessResponse{
		Success: true,
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
	})
}

// HandleError 处理错误响应
// 5xx 错误会自动记录服务端日志
func HandleError(c *gin.Context, statusCode int, code int, message string, errors interface{}) {
	if statusCode >= http.StatusInternalServerError {
		fields := []zap.Field{
			Int("status", statusCode),
			String("path", c.Request.URL.Path),
			String("method", c.Request.Method),
			String("client_ip", GetClientIP(c)),
			String("message", message),
		}
		if requestID, exists := c.Get("requestID"); exists {
			if id, ok := requestID.(string); ok && id != "" {
				fields = append(fields, String("request_id", id))
			}
		}
		// 补充身份与请求细节，便于按用户/参数定位问题
		if uid := UserIDForLog(c); uid != "" {
			fields = append(fields, String("user_id", uid))
		}
		if user, exists := c.Get(string(constants.ContextKeyUsername)); exists {
			if s, ok := user.(string); ok && s != "" {
				fields = append(fields, String("username", s))
			}
		}
		// 查询串可能含 token 等敏感参数，落盘前脱敏
		if q := SanitizeQuery(c.Request.URL.RawQuery); q != "" {
			fields = append(fields, String("query", q))
		}
		// 按请求渠道分流：WebDAV/API 的异常同步写入各自渠道日志，便于单独跟踪，
		// 同时仍写入 error.log 汇总；其余走通用错误日志。
		switch {
		case IsWebDAVPath(c.Request.URL.Path):
			WebDAVError("请求处理异常", fields...)
		case IsAPIPath(c.Request.URL.Path):
			APIError("请求处理异常", fields...)
		default:
			Error("请求处理异常", fields...)
		}
	}
	c.JSON(statusCode, ErrorResponse{
		Success: false,
		Code:    code,
		Message: message,
		Errors:  errors,
	})
}

// HandleBadRequest 处理400错误
func HandleBadRequest(c *gin.Context, message string, errors interface{}) {
	Debug("请求参数错误",
		String("path", c.Request.URL.Path),
		String("method", c.Request.Method),
		String("client_ip", GetClientIP(c)),
		String("message", message))
	HandleError(c, http.StatusBadRequest, CodeBadRequest, message, errors)
}

// HandleUnauthorized 处理401错误
func HandleUnauthorized(c *gin.Context, message string) {
	Warn("认证失败",
		String("path", c.Request.URL.Path),
		String("method", c.Request.Method),
		String("client_ip", GetClientIP(c)),
		String("message", message))
	HandleError(c, http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// HandleForbidden 处理403错误
func HandleForbidden(c *gin.Context, message string) {
	Warn("权限不足",
		String("path", c.Request.URL.Path),
		String("method", c.Request.Method),
		String("client_ip", GetClientIP(c)),
		String("message", message))
	HandleError(c, http.StatusForbidden, CodeForbidden, message, nil)
}

// HandleNotFound 处理404错误
func HandleNotFound(c *gin.Context, message string) {
	Debug("资源不存在",
		String("path", c.Request.URL.Path),
		String("method", c.Request.Method),
		String("client_ip", GetClientIP(c)),
		String("message", message))
	HandleError(c, http.StatusNotFound, CodeNotFound, message, nil)
}

// HandleInternalServerError 处理500错误
func HandleInternalServerError(c *gin.Context, message string) {
	HandleError(c, http.StatusInternalServerError, CodeInternalError, message, nil)
}

// HandleCustomError 处理自定义错误
// 5xx 错误会自动记录服务端日志（由 HandleError 内部处理）
func HandleCustomError(c *gin.Context, statusCode int, code int, message string) {
	HandleError(c, statusCode, code, message, nil)
}

// HandleErrorCompat 兼容旧签名：自动根据状态码推断错误码
// 5xx 错误会自动记录服务端日志（由 HandleError 内部处理）
func HandleErrorCompat(c *gin.Context, statusCode int, message string, errors interface{}) {
	code := inferErrorCode(statusCode)
	HandleError(c, statusCode, code, message, errors)
}

// inferErrorCode 根据 HTTP 状态码推断错误码
func inferErrorCode(statusCode int) int {
	switch statusCode {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusGone:
		return CodeNotFound // 文件过期映射到 NOT_FOUND
	case http.StatusRequestEntityTooLarge:
		return CodeFileTooLarge
	case http.StatusConflict:
		return CodeInternalError // 409 冲突暂用通用码
	case http.StatusInternalServerError:
		return CodeInternalError
	case http.StatusBadGateway:
		return CodeInternalError
	default:
		return CodeInternalError
	}
}
