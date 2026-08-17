package migration

import (
    "strings"
    "time"
)

// ErrorCode 错误码
type ErrorCode string

const (
    ErrorCodeConnectFailed     ErrorCode = "DB_CONNECT_FAILED"
    ErrorCodeConnectTimeout    ErrorCode = "DB_CONNECT_TIMEOUT"
    ErrorCodePermissionDenied  ErrorCode = "DB_PERMISSION_DENIED"
    ErrorCodeNotExist          ErrorCode = "DB_NOT_EXIST"
    ErrorCodeNotEmpty          ErrorCode = "DB_NOT_EMPTY"
    ErrorCodeMigrationCanceled ErrorCode = "MIGRATION_INTERRUPTED"
    ErrorCodeDiskFull          ErrorCode = "DISK_FULL"
    ErrorCodeUnknown           ErrorCode = "DB_UNKNOWN_ERROR"
)

// MigrationError 迁移错误
type MigrationError struct {
    Code               ErrorCode         `json:"code"`
    Message            string            `json:"message"`
    Details            map[string]string `json:"details,omitempty"`
    CanRetry           bool              `json:"canRetry"`
    Suggestion         string            `json:"suggestion,omitempty"`
    Stage              string            `json:"stage,omitempty"`
    RollbackAvailable  bool              `json:"rollbackAvailable"`
}

func (e *MigrationError) Error() string {
    return e.Message
}

// classifyError 将原始错误分类为 MigrationError
func classifyError(err error) *MigrationError {
    if err == nil {
        return nil
    }

    errStr := err.Error()
    errLower := strings.ToLower(errStr)

    // 连接超时
    if strings.Contains(errLower, "timeout") ||
        strings.Contains(errLower, "context deadline") ||
        strings.Contains(errLower, "i/o timeout") ||
        strings.Contains(errLower, "connection refused") {
        return &MigrationError{
            Code:       ErrorCodeConnectTimeout,
            Message:    "连接超时",
            Details:    map[string]string{"reason": errStr},
            CanRetry:   true,
            Suggestion: "请检查网络连接和防火墙设置",
        }
    }

    // 权限不足
    if strings.Contains(errLower, "access denied") ||
        strings.Contains(errLower, "permission denied") ||
        strings.Contains(errLower, "insufficient privilege") ||
        strings.Contains(errLower, "denied") {
        return &MigrationError{
            Code:       ErrorCodePermissionDenied,
            Message:    "权限不足",
            Details:    map[string]string{"reason": errStr},
            CanRetry:   false,
            Suggestion: "请确保用户拥有以下权限: CREATE, INSERT, SELECT, UPDATE, DELETE, DROP, ALTER",
        }
    }

    // 数据库不存在 (MySQL 1049, PostgreSQL 3D000)
    if strings.Contains(errLower, "unknown database") ||
        strings.Contains(errLower, "database does not exist") ||
        strings.Contains(errLower, "1049") ||
        strings.Contains(errLower, "3d000") ||
        strings.Contains(errLower, "schema") {
        return &MigrationError{
            Code:       ErrorCodeNotExist,
            Message:    "数据库不存在",
            Details:    map[string]string{"reason": errStr},
            CanRetry:   false,
            Suggestion: "请先创建数据库",
        }
    }

    // 连接失败
    if strings.Contains(errLower, "connect") ||
        strings.Contains(errLower, "connection") ||
        strings.Contains(errLower, "refused") ||
        strings.Contains(errLower, "network") {
        return &MigrationError{
            Code:       ErrorCodeConnectFailed,
            Message:    "连接失败",
            Details:    map[string]string{"reason": errStr},
            CanRetry:   true,
            Suggestion: "请检查数据库服务是否运行，以及连接信息是否正确",
        }
    }

    return &MigrationError{
        Code:       ErrorCodeUnknown,
        Message:    "未知错误",
        Details:    map[string]string{"reason": errStr},
        CanRetry:   true,
        Suggestion: "请查看详细错误信息",
    }
}

// ConnectionTimeout 连接超时时间
const ConnectionTimeout = 5 * time.Second

// DefaultRetryDelay 默认重试延迟
const DefaultRetryDelay = 2 * time.Second