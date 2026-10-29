package middleware

import (
	"net/http"

	"fuzhan/internal/accessguard"
	"fuzhan/internal/constants"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"

	"github.com/gin-gonic/gin"
)

// DownloadRateLimit 下载数据面限流：仅对匿名请求按 IP 做滑动窗口限制。
// 登录用户（含票据放行注入的身份）不受限，避免正常批量下载被误伤；
// 阈值复用 download.rate_limit 配置，max_requests<=0 表示不限制，支持热生效。
func DownloadRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, authenticated := c.Get(string(constants.ContextKeyUserUUID)); authenticated {
			c.Next()
			return
		}
		if !accessguard.Acquire(accessguard.SCOPE_DOWNLOAD, utils.GetClientIP(c)) {
			response.HandleError(c, http.StatusTooManyRequests, response.CodeBadRequest, "下载过于频繁，请稍后再试", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

// TicketRateLimit 换票接口限流：按 IP 限制票据签发频率，缓解凭证批量签发滥用。
// 换票本身是轻量请求，但每次都会签发短时效票据，需要独立计数。
func TicketRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !accessguard.Acquire(accessguard.SCOPE_TICKET, utils.GetClientIP(c)) {
			response.HandleError(c, http.StatusTooManyRequests, response.CodeBadRequest, "获取下载地址过于频繁，请稍后再试", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}
