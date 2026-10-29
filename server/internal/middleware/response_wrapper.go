package middleware

import (
	"github.com/gin-gonic/gin"
)

// ResponseWrapperMiddleware 处理令牌刷新结果
// 该中间件应该在 TokenRefreshMiddleware 之后使用
func ResponseWrapperMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 刷新后的令牌只写回 HttpOnly Cookie，不通过响应头暴露给前端脚本。
		// Vue 与 lite 两端共用同一 Cookie 会话，脚本侧无需持有令牌。
		if newToken, exists := c.Get("refreshedToken"); exists {
			if tokenStr, ok := newToken.(string); ok && tokenStr != "" {
				SetAuthCookie(c, tokenStr)
			}
		}
	}
}
