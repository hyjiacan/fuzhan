package middleware

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/downloadticket"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// userStatusEntry 一条用户禁用状态缓存记录
type userStatusEntry struct {
	disabled bool
	expire   time.Time
}

// userStatusCache 缓存用户禁用状态（正反两面），避免正常用户每个请求都回源查库。
// 禁用与正常结果统一走短 TTL：命中即免查库，管理员禁用/恢复在 TTL(5s) 内生效。
// 管理员在用户管理路径（禁用/启用/删除）会主动清除对应条目，做到即时生效。
type userStatusCache struct {
	mu      sync.RWMutex
	entries map[string]userStatusEntry // uuid -> 禁用状态 + 过期时间
}

var userDisabledCache = &userStatusCache{entries: make(map[string]userStatusEntry)}

// disabledCacheTTL 用户禁用状态缓存 TTL。
// 已尽可能缩短（5s）；配合管理员禁用/删除路径的主动清除，理论上可即时生效。
const disabledCacheTTL = 5 * time.Second

// init 启动缓存后台清理协程
func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			userDisabledCache.cleanExpired()
		}
	}()
}

// isDisabled 返回缓存的禁用状态；未命中或已过期时 cached=false。
func (sc *userStatusCache) isDisabled(uuid string) (disabled, cached bool) {
	sc.mu.RLock()
	e, ok := sc.entries[uuid]
	sc.mu.RUnlock()
	if !ok || utils.Now().After(e.expire) {
		return false, false
	}
	return e.disabled, true
}

// set 写入一次用户禁用状态（正反结果均可缓存）。
func (sc *userStatusCache) set(uuid string, disabled bool) {
	sc.mu.Lock()
	sc.entries[uuid] = userStatusEntry{disabled: disabled, expire: utils.Now().Add(disabledCacheTTL)}
	sc.mu.Unlock()
}

// cleanExpired 定期清理过期条目，防止用户积累
func (sc *userStatusCache) cleanExpired() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	now := utils.Now()
	for uuid, e := range sc.entries {
		if now.After(e.expire) {
			delete(sc.entries, uuid)
		}
	}
}

// invalidate 删除指定用户的缓存条目，使禁用/启用/删除即时生效。
func (sc *userStatusCache) invalidate(uuid string) {
	sc.mu.Lock()
	delete(sc.entries, uuid)
	sc.mu.Unlock()
}

// InvalidateUserDisabledCache 清除指定用户的禁用状态缓存条目。
// 由用户管理 handler（禁用/启用/删除用户）在服务端状态变更成功后调用，
// 使新状态立即对认证中间件生效，避免等待 TTL 过期。
func InvalidateUserDisabledCache(uuid string) {
	userDisabledCache.invalidate(uuid)
}

// AuthMiddleware JWT认证中间件配置
type AuthMiddleware struct {
	db *gorm.DB
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(db *gorm.DB) *AuthMiddleware {
	return &AuthMiddleware{db: db}
}

// AuthRequired 是一个JWT认证中间件，检查用户是否被禁用（实时查库）
func (am *AuthMiddleware) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 登录凭证优先取 Authorization 请求头（token 不允许出现在 URL 中）
		tokenString := c.GetHeader("Authorization")
		if tokenString != "" {
			tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		}
		// 无请求头时回退读取 HttpOnly Cookie（Vue 同源请求自动携带，统一两端会话）
		if tokenString == "" {
			tokenString = TokenFromCookie(c)
		}
		if tokenString == "" {
			// 无 Authorization：若已被 LoginGate 通过下载票据（URL 上的 t=）鉴权，
			// 则用户上下文（username/role/uuid）已由 LoginGate 注入，等价放行。
			// 用于 /api/v1/web/admin/download 等带票据的浏览器原生请求。
			if hasTicketContext(c) {
				c.Next()
				return
			}
			utils.HandleUnauthorized(c, "缺少认证信息")
			c.Abort()
			return
		}

		// 验证token
		claims, err := jwt.ParseJWT(tokenString)
		if err != nil {
			utils.HandleUnauthorized(c, "无效的认证令牌")
			c.Abort()
			return
		}

		// 检查用户是否被禁用（缓存 + 回源），被禁用则拒绝
		if am.checkUserDisabled(c, claims.UUID) {
			return
		}

		// 将用户信息存储到上下文中
		c.Set(string(constants.ContextKeyUsername), claims.Username)
		c.Set(string(constants.ContextKeyRole), claims.Role)
		c.Set(string(constants.ContextKeyUserUUID), claims.UUID)

		// 同时设置到请求头，供传统http.Handler使用
		c.Request.Header.Set("X-User-Identifier", claims.UUID)

		// 存储原始token，供后续刷新使用
		c.Set("originalToken", tokenString)
		c.Set("tokenClaims", claims)

		// 继续处理请求
		c.Next()
	}
}

// checkUserDisabled 校验用户是否被禁用（优先查短 TTL 缓存，未命中回源并回写正反结果）。
// 返回 true 表示请求已被拦截（响应已写入并 Abort），调用方应立即 return。
func (am *AuthMiddleware) checkUserDisabled(c *gin.Context, uuid string) bool {
	if disabled, cached := userDisabledCache.isDisabled(uuid); disabled {
		utils.HandleForbidden(c, "账号已被禁用")
		c.Abort()
		return true
	} else if !cached && am.db != nil {
		var user models.User
		err := am.db.Select("disabled").Where("uuid = ?", uuid).First(&user).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			// 数据库错误（非记录不存在），认证服务暂时不可用
			utils.Error("检查用户状态失败", utils.Err(err), utils.String("uuid", uuid))
			utils.HandleInternalServerError(c, "认证服务暂时不可用")
			c.Abort()
			return true
		}
		// 记录不存在视为正常，统一写入缓存，避免正常用户重复回源
		userDisabledCache.set(uuid, user.Disabled)
		if user.Disabled {
			utils.HandleForbidden(c, "账号已被禁用")
			c.Abort()
			return true
		}
	}
	return false
}

// CheckUserDisabled 校验用户是否被禁用；已禁用时写入 403 响应并 Abort，返回 true。
// 供 Web 会话凭证复用登录闸门的实时禁用检查（缓存 + 回源）。
func (am *AuthMiddleware) CheckUserDisabled(c *gin.Context, uuid string) bool {
	return am.checkUserDisabled(c, uuid)
}

// GetUsername 从上下文中获取用户名
func GetUsername(c *gin.Context) (string, error) {
	usernameInterface, exists := c.Get(string(constants.ContextKeyUsername))
	if !exists {
		return "", errors.New("用户未认证")
	}

	username, ok := usernameInterface.(string)
	if !ok || username == "" {
		return "", errors.New("无效的用户名")
	}

	return username, nil
}

// RequireAdmin 是一个管理员权限中间件
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		roleInterface, exists := c.Get(string(constants.ContextKeyRole))
		if !exists {
			utils.HandleForbidden(c, "访问被拒绝：需要认证")
			c.Abort()
			return
		}

		role, ok := roleInterface.(string)
		if !ok || role != "admin" {
			utils.HandleForbidden(c, "访问被拒绝：需要管理员权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

// AuthOptional 是一个可选JWT认证中间件，提取用户信息但不阻断匿名请求
func (am *AuthMiddleware) AuthOptional() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		tokenString := ""
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}
		// 无请求头时回退读取 HttpOnly Cookie（统一两端会话）
		if tokenString == "" {
			tokenString = TokenFromCookie(c)
		}
		if tokenString == "" {
			c.Next()
			return
		}
		claims, err := jwt.ParseJWT(tokenString)
		if err != nil {
			c.Next()
			return
		}
		// 实时查库检查禁用状态（正反结果都写短 TTL 缓存）
		if disabled, cached := userDisabledCache.isDisabled(claims.UUID); disabled {
			c.Next()
			return
		} else if !cached && am.db != nil {
			var user models.User
			if err := am.db.Select("disabled").Where("uuid = ?", claims.UUID).First(&user).Error; err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
				userDisabledCache.set(claims.UUID, user.Disabled)
				if user.Disabled {
					c.Next()
					return
				}
			}
		}
		c.Set(string(constants.ContextKeyUsername), claims.Username)
		c.Set(string(constants.ContextKeyRole), claims.Role)
		c.Set(string(constants.ContextKeyUserUUID), claims.UUID)
		c.Request.Header.Set("X-User-Identifier", claims.UUID)
		c.Set("originalToken", tokenString)
		c.Set("tokenClaims", claims)
		c.Next()
	}
}

// LoginGate 是一个动态登录闸门中间件：仅当「强制登录」开关生效时拦截匿名请求，
// 开关关闭时直接放行，保持原有匿名访问行为。
// whiteList 可选，命中（返回 true）的请求直接放行，用于保留登录接口、临时文件分享等匿名例外。
// 认证方式两套：登录 token 只走 Authorization 请求头；携带一次性下载票据（URL 上的 t= 参数）
// 的请求（浏览器原生标签/新窗口/外部下载工具发起的下载与内联预览）经票据验签后等价放行，
// 并把票据内嵌的用户上下文注入，供下游 AuthRequired / RequireAdmin 识别。
// 未认证请求按类型分流：CLI（curl/wget）与 XHR/API 返回 401 JSON，
// 浏览器导航型返回 302 重定向到登录页（携带 redirect 便于登录后回跳）。
func (am *AuthMiddleware) LoginGate(whiteList ...func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 未开启强制登录：放行所有请求（保持匿名访问兼容）
		if !appconfig.GlobalConfig.Account.RequireLogin {
			c.Next()
			return
		}
		// 命中白名单：匿名放行（登录接口、临时分享、初始化引导等）
		for _, w := range whiteList {
			if w(c) {
				c.Next()
				return
			}
		}

		// 下载票据短路：URL 携带 t= 时无需登录 token。
		// 覆盖浏览器原生标签 <a>/<img>/<video>、window.open、迅雷等外部工具
		// （它们只能使用 URL、无法携带 custom header）。票据校验失败按未认证分流。
		// 票据按时限生效（不做一次性消费），可重复读取以兼容 Range/断点续传。
		if ticket := c.Query(downloadticket.TicketParamName); ticket != "" {
			payload, err := downloadticket.Validate(ticket)
			if err != nil {
				utils.Warn("下载票据校验失败", utils.String("path", c.Request.URL.Path), utils.Err(err))
			} else {
				// 端点种类必须匹配（管理下载票不能用于普通下载，反之亦然）；
				// 文件身份（路径/大小/修改时间/用途）由下载服务解析真实文件后比对。
				kind, _, ok := downloadticket.SplitRequestPath(c.Request.URL.Path)
				if ok && kind == payload.Kind {
					// 票据有效期内同样校验用户禁用状态（与登录 token 路径保持一致）
					if am.checkUserDisabled(c, payload.D) {
						return
					}
					c.Set(string(constants.ContextKeyUsername), payload.U)
					c.Set(string(constants.ContextKeyRole), payload.R)
					c.Set(string(constants.ContextKeyUserUUID), payload.D)
					c.Request.Header.Set("X-User-Identifier", payload.D)
					// 将票据载荷挂到 request context，供下载服务做文件绑定校验
					c.Request = c.Request.WithContext(downloadticket.WithPayload(c.Request.Context(), payload))
					c.Next()
					return
				}
				utils.Warn("下载票据端点不匹配", utils.String("ticket_kind", payload.Kind), utils.String("request_path", c.Request.URL.Path))
			}
		}

		// 登录凭证优先接受 Authorization 请求头（token 不允许出现在 URL 中）；
		// 无该头（浏览器原生请求、轻量版 /lite 页面）时回退读取 HttpOnly 会话 Cookie。
		tokenString := c.GetHeader("Authorization")
		if tokenString != "" {
			tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		} else {
			tokenString = TokenFromCookie(c)
		}
		if tokenString == "" {
			am.handleUnauthenticated(c)
			c.Abort()
			return
		}

		// 验证 token
		claims, err := jwt.ParseJWT(tokenString)
		if err != nil {
			am.handleUnauthenticated(c)
			c.Abort()
			return
		}

		// 检查用户是否被禁用（缓存 + 回源），被禁用则拒绝
		if am.checkUserDisabled(c, claims.UUID) {
			return
		}

		// 将用户信息存储到上下文中
		c.Set(string(constants.ContextKeyUsername), claims.Username)
		c.Set(string(constants.ContextKeyRole), claims.Role)
		c.Set(string(constants.ContextKeyUserUUID), claims.UUID)
		c.Request.Header.Set("X-User-Identifier", claims.UUID)
		c.Set("originalToken", tokenString)
		c.Set("tokenClaims", claims)

		c.Next()
	}
}

// hasTicketContext 判断请求是否已由 LoginGate 通过下载票据（URL 上 t=）鉴权并注入用户上下文。
// 票据验签通过后 LoginGate 会注入 ContextKeyUserUUID 等上下文，AuthRequired 据此放行。
func hasTicketContext(c *gin.Context) bool {
	_, ok := c.Get(string(constants.ContextKeyUserUUID))
	return ok
}

// handleUnauthenticated 未认证请求分流：
//   - CLI（curl/wget）：返回 401 JSON（命令行避免被 302 重定向误导）
//   - 浏览器主导航型（Sec-Fetch-Mode: navigate 或 Accept 期望 HTML）：
//     302 重定向到登录页并携带 redirect=原请求地址，登录成功后自动回跳执行；
//     /lite 前缀的请求跳转到轻量版自带的服务器渲染登录页（低版本/低性能设备兼容），
//     其余跳转到 Vue 前端登录页。
//   - 其余（前端 XHR/fetch/API）：返回 401 JSON，由前端统一跳转登录页
func (am *AuthMiddleware) handleUnauthenticated(c *gin.Context) {
	if isCLINavigation(c) {
		utils.HandleUnauthorized(c, "缺少认证信息或登录已失效，请先登录")
		return
	}
	if isBrowserNavigation(c) {
		redirect := url.QueryEscape(c.Request.URL.RequestURI())
		if strings.HasPrefix(c.Request.URL.Path, "/lite") {
			c.Redirect(http.StatusFound, "/lite/login?redirect="+redirect)
			return
		}
		// 使用相对地址跳转：避免用请求 Host 拼绝对 URL（Host 头可被伪造，
		// 可能把登录回跳指向攻击者域名）。
		c.Redirect(http.StatusFound, "/#/login?redirect="+redirect)
		return
	}
	utils.HandleUnauthorized(c, "缺少认证信息或登录已失效")
}

// isCLINavigation 判断请求是否来自命令行工具（curl/wget）
func isCLINavigation(c *gin.Context) bool {
	low := strings.ToLower(c.GetHeader("User-Agent"))
	return strings.HasPrefix(low, "curl") || strings.HasPrefix(low, "wget")
}

// isBrowserNavigation 判断请求是否由浏览器发起页面导航。
// 现代浏览器主导航请求会带 Sec-Fetch-Mode: navigate；旧浏览器无该头时，
// 以 Accept 期望 HTML 作为兜底判断。前端 XHR/fetch 通常 Accept 为 application/json。
func isBrowserNavigation(c *gin.Context) bool {
	if sf := c.GetHeader("Sec-Fetch-Mode"); sf != "" {
		return sf == "navigate"
	}
	accept := c.GetHeader("Accept")
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml+xml")
}

// CORSMiddleware 处理跨域请求
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		secConfig := appconfig.GetSecurityConfig()

		// 检查来源是否被允许
		allowOrigin := ""
		if len(secConfig.AllowedOrigins) == 1 && secConfig.AllowedOrigins[0] == "*" {
			// 通配符模式
			allowOrigin = "*"
		} else {
			// 检查来源是否在白名单中
			for _, allowed := range secConfig.AllowedOrigins {
				if allowed == origin || allowed == "*" {
					allowOrigin = origin
					break
				}
			}
		}

		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With, X-User-Identifier, X-Request-ID, X-Anonymous-ID")
			// Credentials 为 true 时不允许 Allow-Origin 为 *
			if allowOrigin != "*" {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// SecurityHeadersMiddleware 添加安全相关的HTTP头
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 防止XSS攻击
		c.Header("X-Content-Type-Options", "nosniff")
		// 文件预览（preview=true，如 PDF/HTML）需要嵌入预览弹窗的 iframe，
		// 放宽为 SAMEORIGIN 仅允许同源页面嵌入；其余请求保持 DENY 防点击劫持
		// frameOptions := "DENY"
		// if c.Query("preview") == "true" {
		// 	frameOptions = "SAMEORIGIN"
		// }
		// c.Header("X-Frame-Options", frameOptions)
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("X-XSS-Protection", "1; mode=block")

		// 内容安全策略
		// 'unsafe-eval' 用于 WASM 编译场景，删除前需确认无 WASM 相关功能
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:")
		// 禁止 Referer 外泄：下载/预览 URL 上的票据（t=）是短时效凭证，
		// 避免内联预览页面引用外链时把票据经 Referer 发给第三方
		c.Header("Referrer-Policy", "no-referrer")

		// 预览响应单独收紧 CSP：禁止脚本执行并在沙箱中渲染，
		// 防止上传 .html/.svg 等可执行内容被内联渲染后形成同源存储型 XSS
		if c.Query("preview") == "true" {
			c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
		}

		// 严格传输安全（仅在HTTPS环境下使用）
		// c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		c.Next()
	}
}

// RequestIDMiddleware 为每个请求添加唯一的请求ID
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 使用 crypto/rand 生成安全的随机 ID
		requestID := generateSecureRequestID()

		// 添加到响应头
		c.Header("X-Request-ID", requestID)

		// 将请求ID存储到上下文中
		c.Set(string(constants.ContextKeyRequestID), requestID)

		c.Next()
	}
}

// generateSecureRequestID 生成安全的随机请求ID
func generateSecureRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵源不可用，回退到纳秒时间戳 + 自增计数器
		requestIDCounter++
		timestamp := uint64(utils.Now().UnixNano())
		binary.BigEndian.PutUint64(b[0:8], timestamp)
		binary.BigEndian.PutUint64(b[8:16], requestIDCounter)
		utils.Warn("crypto/rand.Read 失败，使用回退方案生成 RequestID", utils.Err(err))
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var requestIDCounter uint64
