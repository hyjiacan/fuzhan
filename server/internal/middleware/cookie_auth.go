package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CookieTokenName 轻量版（/lite）服务器渲染页面使用的 HttpOnly Cookie 登录凭证名。
// 轻量版兼容低版本/低性能设备：页面无脚本，无法携带 Authorization 请求头，
// 因此登录后把与 web 同源的 JWT 令牌写入 HttpOnly Cookie，由 LoginGate 解析放行。
// 下载身份鉴别与 web 一致：Cookie 只让 LoginGate 放行，下载仍走「换票 + URL 票据」链路的
// 业务标识（username/role/uuid 上下文）与 web 完全相同。
const CookieTokenName = "fuzhan_token"

// cookieMaxAge Cookie 有效期（秒），与 JWT 令牌默认 TTL（1 天）保持一致。
const cookieMaxAge = 86400

// SetAuthCookie 把 JWT 令牌写入 HttpOnly Cookie（全站路径）。
// HttpOnly + SameSite=Lax：阻止脚本读取；Lax 下跨站 POST 不携带该 Cookie（防 CSRF 登录滥用），
// 顶级导航 GET（如 /download 链接）仍携带，保证 lite 点击下载链接可被 LoginGate 放行。
// HTTPS（含 X-Forwarded-Proto 透传的反向代理场景）下额外加 Secure。
func SetAuthCookie(c *gin.Context, token string) {
	v := &http.Cookie{
		Name:     CookieTokenName,
		Value:    token,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		v.Secure = true
	}
	http.SetCookie(c.Writer, v)
}

// ClearAuthCookie 清除登录 Cookie（登出时调用）。
func ClearAuthCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieTokenName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// TokenFromCookie 读取 Cookie 中的 JWT 令牌，不存在或为空时返回空串。
// 供 LoginGate 在无 Authorization 请求头时回退使用（浏览器原生请求/轻量版页面）。
func TokenFromCookie(c *gin.Context) string {
	return tokenFromRequest(c.Request)
}

// TokenFromRequest 读取 http.Request 的 Cookie 中的 JWT 令牌（纯 http.Handler 场景，
// 如 /lite 服务器渲染页在渲染登录态时使用），不存在或为空时返回空串。
func TokenFromRequest(r *http.Request) string {
	return tokenFromRequest(r)
}

// tokenFromRequest 读取请求 Cookie 中的 JWT 令牌（核心实现）。
func tokenFromRequest(r *http.Request) string {
	ck, err := r.Cookie(CookieTokenName)
	if err != nil || ck == nil || ck.Value == "" {
		return ""
	}
	return ck.Value
}
