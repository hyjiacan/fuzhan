package middleware

import (
    "github.com/gin-gonic/gin"
)

// ResponseWrapperMiddleware 在响应中添加刷新后的 token
// 该中间件应该在 TokenRefreshMiddleware 之后使用
func ResponseWrapperMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()

        // 检查是否有刷新后的 token
        if newToken, exists := c.Get("refreshedToken"); exists {
            if tokenStr, ok := newToken.(string); ok && tokenStr != "" {
                // 添加到响应头
                c.Header("X-Refreshed-Token", tokenStr)
            }
        }
    }
}