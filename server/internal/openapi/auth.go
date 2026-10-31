package openapi

import (
	"net/http"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
)

// Open API 鉴权方式
const (
	// AuthModeNone 匿名模式（历史行为）；强制登录开启时会被直接拒绝
	AuthModeNone = "none"
	// AuthModeAPIKey 使用系统签发的 API Key（携带 scope），与 Web 登录态完全隔离
	AuthModeAPIKey = "api_key"
)

// 上下文键：与 auth.RBACService.RequireScope 共用，鉴权通过后统一写入
const (
	scopesContextKey   = "scopes"
	authTypeContextKey = "authType"
	userIDContextKey   = "userID"
)

// cacheControlNoStore 鉴权失败/拒绝响应统一使用的禁缓存头，避免凭证态响应被中间层缓存。
const cacheControlNoStore = "no-store"

// AuthGateMiddleware Open API 鉴权闸门（基于系统签发的 API Key + scope）。
//
// 优先级规则：强制登录（account.require_login）最高。
//   - 未开启强制登录：auth_mode=none 保持匿名（历史行为）；auth_mode=api_key 校验 API Key。
//   - 开启强制登录：auth_mode 必须为 api_key，否则直接 403（不允许匿名绕过强制登录）。
//
// 与 Web 登录态完全隔离：本中间件只接受 API Key，不接受 Web 会话 JWT，也不会签发任何 Web 凭证。
// 鉴权通过后把 scope 列表写入上下文，供 auth.RBACService.RequireScope / RequireReaderScope 复用。
func AuthGateMiddleware(rbac *auth.RBACService) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := appconfig.GetConfig()
		requireLogin := cfg.Account.RequireLogin
		mode := CurrentAuthMode()

		switch mode {
		case AuthModeNone:
			if requireLogin {
				utils.Warn("强制登录已开启，拒绝匿名 Open API 访问",
					utils.String("path", c.Request.URL.Path))
				c.Header("Cache-Control", cacheControlNoStore)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "强制登录已开启，Open API 必须启用 API Key 鉴权（open_api.auth_mode=api_key）",
				})
				return
			}
			c.Next()

		case AuthModeAPIKey:
			if rbac == nil {
				// 服务未装配：fail-closed，绝不降级为匿名
				utils.Error("Open API 鉴权服务未初始化")
				c.Header("Cache-Control", cacheControlNoStore)
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Open API 鉴权服务不可用"})
				return
			}
			token := bearerToken(c)
			if token == "" {
				c.Header("WWW-Authenticate", `Bearer realm="Open API"`)
				c.Header("Cache-Control", cacheControlNoStore)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 API Key"})
				return
			}
			userID, scopes, err := rbac.ValidateAPIKey(token)
			if err != nil {
				utils.Warn("Open API API Key 鉴权失败",
					utils.String("path", c.Request.URL.Path),
					utils.Err(err))
				c.Header("WWW-Authenticate", `Bearer realm="Open API"`)
				c.Header("Cache-Control", cacheControlNoStore)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "API Key 无效或已禁用"})
				return
			}
			c.Set(userIDContextKey, userID)
			c.Set(authTypeContextKey, AuthModeAPIKey)
			c.Set(scopesContextKey, splitScopes(scopes))
			c.Next()

		default:
			// oauth2 模式已废弃：给出明确升级提示，避免调用方误以为是临时故障
			msg := "open_api.auth_mode 配置无效，仅支持 none / api_key"
			if mode == "oauth2" {
				msg = "open_api.auth_mode=oauth2 已废弃，请改为 api_key（使用系统签发的 API Key + scope）"
			}
			utils.Warn("Open API 鉴权方式配置无效", utils.String("auth_mode", mode))
			c.Header("Cache-Control", cacheControlNoStore)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": msg})
		}
	}
}

// LoginRequiredWhenForced OpenAPI 文档（/openapi.json）专用守卫。
//
// 与 AuthGateMiddleware 的差异只在「强制登录 + auth_mode=none」这一格：
// AuthGate 会直接 403（并要求启用 api_key），因为它守护的是 Open API 数据端点，
// 不接受 Web 登录态。文档是给人看的，已登录的 Web 用户（Cookie JWT）本应能读，
// 因此这里不放宽数据端点，只对文档单开一条窄口子（同一矩阵）：
//
//	requireLogin | auth_mode | openapi.json
//	-------------+-----------+----------------------------------------
//	false        | none      | 匿名 200（历史行为）
//	false        | api_key   | 需 API Key（AuthGate 语义），无 Key 401
//	true         | none      | Cookie JWT 或 Bearer JWT/API Key，否则 401
//	true         | api_key   | 需 API Key（AuthGate 语义），无 Key 401
//
// 之所以不复用「none+requireLogin→403」：强制登录后匿名被拒仍是目标，
// 但登录用户不该被登录态本身挡在文档外；同时 api_key 模式完全沿用 AuthGate，
// 不因文档路由打开新的匿名通道。
//
// authMW 用于「强制登录 + none」分支复用 Web 会话的禁用态校验：
// 验签通过后调用 AuthMiddleware.CheckUserDisabled() 实时查库（缓存 + 回源），
// 被禁用用户直接写入 403 并 Abort，避免只验签就放行导致已禁用用户仍能读文档。
// authMW 为 nil（测试场景）时退化为仅验签放行。
func LoginRequiredWhenForced(authMW *middleware.AuthMiddleware, rbac *auth.RBACService) gin.HandlerFunc {
	apiKeyGate := AuthGateMiddleware(rbac)
	return func(c *gin.Context) {
		switch CurrentAuthMode() {
		case AuthModeAPIKey:
			// api_key 模式：完全沿用 AuthGate（必须带有效 API Key），不因文档路由开新洞
			apiKeyGate(c)
			return
		case AuthModeNone:
			// 非强制登录：保持历史匿名行为
			if !appconfig.GetConfig().Account.RequireLogin {
				c.Next()
				return
			}
			// 强制登录 + none：接受 Web 会话 Cookie 或 Bearer JWT/API Key
			if token := middleware.TokenFromCookie(c); token != "" {
				if claims, err := jwt.ParseJWT(token); err == nil {
					if authMW != nil && authMW.CheckUserDisabled(c, claims.UUID) {
						// 响应已写入并 Abort
						return
					}
					c.Next()
					return
				}
			}
			if token := bearerToken(c); token != "" {
				if claims, err := jwt.ParseJWT(token); err == nil {
					if authMW != nil && authMW.CheckUserDisabled(c, claims.UUID) {
						// 响应已写入并 Abort
						return
					}
					c.Next()
					return
				}
				// 仅在形如 keyID:secret 的凭证上尝试 API Key 校验，
				// 避免无效 JWT 进入 ValidateAPIKey 触发误导性格式告警
				if rbac != nil && strings.Contains(token, ":") {
					if _, _, err := rbac.ValidateAPIKey(token); err == nil {
						c.Next()
						return
					}
				}
			}
			utils.Warn("强制登录已开启，OpenAPI 文档拒绝匿名访问",
				utils.String("path", c.Request.URL.Path))
			c.Header("WWW-Authenticate", `Bearer realm="Open API"`)
			c.Header("Cache-Control", cacheControlNoStore)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "强制登录已开启，需要有效凭证"})
		default:
			// 未知模式交回 AuthGate 统一给出明确错误（500）
			apiKeyGate(c)
		}
	}
}

// CurrentAuthMode 读取并归一化当前 Open API 鉴权方式（空值视为 none）。
func CurrentAuthMode() string {
	mode := strings.ToLower(strings.TrimSpace(appconfig.GetConfig().OpenAPI.AuthMode))
	if mode == "" {
		return AuthModeNone
	}
	return mode
}

// RequireReaderScope 在非匿名模式下要求 open_api:reader（或更高）scope。
// 匿名模式（auth_mode=none）下保持历史公开行为，不做 scope 校验。
// 用于原本公开的列表/搜索/下载接口：一旦启用 API Key 模式，必须携带具备 reader 权限的 Key。
func RequireReaderScope(rbac *auth.RBACService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if CurrentAuthMode() == AuthModeNone {
			c.Next()
			return
		}
		if rbac == nil || !rbac.HasContextScope(c, models.ScopeOpenAPIReader) {
			c.Header("Cache-Control", cacheControlNoStore)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "权限不足，需要 scope: " + models.ScopeOpenAPIReader,
			})
			return
		}
		c.Next()
	}
}

// splitScopes 把逗号分隔的 scope 字符串拆成去空列表。
func splitScopes(scopes string) []string {
	if strings.TrimSpace(scopes) == "" {
		return nil
	}
	parts := strings.Split(scopes, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			result = append(result, s)
		}
	}
	return result
}

// bearerToken 从 Authorization 头提取 Bearer token（禁止从 query 传 key，避免落日志）。
func bearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	if strings.HasPrefix(authHeader, "bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "bearer "))
	}
	// 兼容直接传 API Key（无 Bearer 前缀）
	return strings.TrimSpace(authHeader)
}
