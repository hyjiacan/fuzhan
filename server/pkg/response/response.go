// Package response 提供统一的 API 响应格式
package response

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

// 错误码定义
const (
    CodeSuccess           = 0
    CodeBadRequest        = 1004
    CodeUnauthorized      = 2001
    CodeForbidden         = 2004
    CodeNotFound          = 3001
    CodeFileTooLarge      = 3002
    CodeQuotaExceeded     = 3003
    CodeFileTypeNotAllowed = 3004
    CodeInternalError     = 5001
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
func HandleError(c *gin.Context, statusCode int, code int, message string, errors interface{}) {
    c.JSON(statusCode, ErrorResponse{
        Success: false,
        Code:    code,
        Message: message,
        Errors:  errors,
    })
}

// HandleBadRequest 处理400错误
func HandleBadRequest(c *gin.Context, message string, errors interface{}) {
    HandleError(c, http.StatusBadRequest, CodeBadRequest, message, errors)
}

// HandleUnauthorized 处理401错误
func HandleUnauthorized(c *gin.Context, message string) {
    HandleError(c, http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// HandleForbidden 处理403错误
func HandleForbidden(c *gin.Context, message string) {
    HandleError(c, http.StatusForbidden, CodeForbidden, message, nil)
}

// HandleNotFound 处理404错误
func HandleNotFound(c *gin.Context, message string) {
    HandleError(c, http.StatusNotFound, CodeNotFound, message, nil)
}

// HandleInternalServerError 处理500错误
func HandleInternalServerError(c *gin.Context, message string) {
    HandleError(c, http.StatusInternalServerError, CodeInternalError, message, nil)
}

// HandleCustomError 处理自定义错误
func HandleCustomError(c *gin.Context, statusCode int, code int, message string) {
    HandleError(c, statusCode, code, message, nil)
}

// HandleErrorCompat 兼容旧签名：自动根据状态码推断错误码
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
        return CodeNotFound
    case http.StatusRequestEntityTooLarge:
        return CodeFileTooLarge
    case http.StatusConflict:
        return CodeInternalError
    case http.StatusInternalServerError:
        return CodeInternalError
    case http.StatusBadGateway:
        return CodeInternalError
    default:
        return CodeInternalError
    }
}
