package utils

import (
	"strconv"

	"fuzhan/internal/constants"
	"github.com/gin-gonic/gin"
)

// UserIDForLog 从请求上下文提取用于日志的用户标识。
// JWT 认证写入 ContextKeyUserUUID（string）；API Key 认证写入 ContextKeyUserID（uint）。
// 两者统一返回字符串，供访问/异常日志的 user_id 字段使用。
func UserIDForLog(c *gin.Context) string {
	if uid, exists := c.Get(string(constants.ContextKeyUserUUID)); exists {
		if s, ok := uid.(string); ok && s != "" {
			return s
		}
	}
	if uid, exists := c.Get(string(constants.ContextKeyUserID)); exists {
		switch v := uid.(type) {
		case string:
			return v
		case uint:
			return strconv.FormatUint(uint64(v), 10)
		case int:
			return strconv.Itoa(v)
		case int64:
			return strconv.FormatInt(v, 10)
		}
	}
	return ""
}
