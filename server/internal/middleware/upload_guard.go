package middleware

import (
	"net/http"

	"fuzhan/internal/accessguard"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// UploadRateLimit 上传频率限流中间件
// 挂载在上传控制面入口（创建会话 / 完成上传 / URL 上传提交），按客户端 IP
// 在滑动窗口内限制操作次数；阈值实时读取 upload.rate_limit 配置，支持热生效。
// MaxRequests<=0（未配置）时不做限制。分片数据面请求（chunk 上传）不受本限制，
// 避免大文件正常分片被误伤。
func UploadRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := utils.GetClientIP(c)
		if !accessguard.AcquireUpload(ip) {
			response.HandleError(c, http.StatusTooManyRequests, response.CodeBadRequest, "上传操作过于频繁，请稍后再试", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}
