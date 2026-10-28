package lite

import (
	"github.com/gin-gonic/gin"
)

// LiteAuthWhiteList 返回 /lite 组「强制登录」闸门的匿名例外汇总规则。
// 仅放行轻量版自身的认证入口：login / register 的 GET 渲染与 POST 提交（匿名可访问），
// 以及 logout 的 POST（登出无需认证，公开给任何携带会话/未携带会话的客户端调用）。
// 其余 /lite 路径（浏览列表、搜索、回跳目标）一律受 LoginGate 保护：
// 未登录的浏览器导航由 LoginGate 分流至 /lite/login 渲染轻量版登录页。
func LiteAuthWhiteList() []func(*gin.Context) bool {
	isPath := func(p string) func(*gin.Context) bool {
		return func(c *gin.Context) bool {
			return c.Request.URL.Path == p
		}
	}
	return []func(*gin.Context) bool{
		isPath("/lite/login"),
		isPath("/lite/register"),
		isPath("/lite/logout"),
	}
}
