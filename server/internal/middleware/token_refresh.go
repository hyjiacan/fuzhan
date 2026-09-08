package middleware

import (
	"fuzhan/internal/constants"
	"fuzhan/internal/services"
	"github.com/gin-gonic/gin"
)

// TokenRefreshMiddleware 自动刷新即将过期的 token
// 该中间件应该在 AuthRequired 之后使用
func TokenRefreshMiddleware(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 执行处理函数
		c.Next()

		// 只在成功响应时检查是否需要刷新 token
		// 如果已经有 refreshedToken 就不重复添加
		if c.IsAborted() || c.GetBool("tokenRefreshed") {
			return
		}

		// 获取用户 UUID 检查是否被禁用（复用全局 disabledCache，避免重复查库）
		uuidInterface, exists := c.Get(string(constants.ContextKeyUserUUID))
		if !exists {
			return
		}
		uuid, ok := uuidInterface.(string)
		if !ok || uuid == "" {
			return
		}

		// 复用 auth.go 中的全局 disabledCache，避免重复查库
		if disabled, _ := userDisabledCache.isDisabled(uuid); disabled {
			return // 禁用用户不刷新 token
		}

		newToken, err := authService.RefreshTokenIfNeeded(c)
		if err != nil || newToken == "" {
			return
		}
		c.Set("refreshedToken", newToken)
		c.Set("tokenRefreshed", true)
	}
}
