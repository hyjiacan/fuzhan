package openapi

import (
	"net/http"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/constants"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
)

// Open API 鉴权方式
const (
	// AuthModeNone 凭证模式（L6 加固后不再匿名）：需要有效 Web 会话 JWT（Cookie 或 Bearer）或有效 API Key，
	// 否则一律 401/403；是否开启强制登录均不放行匿名。
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

// AuthGateMiddleware Open API 鉴权闸门。
//
// 优先级规则：强制登录（account.require_login）最高。
//   - 未开启强制登录：auth_mode=none 也要求有效凭证（Web 会话 JWT 或 API Key），匿名一律 401；
//     auth_mode=api_key 校验 API Key。
//   - 开启强制登录：none 与 api_key 语义一致——none 表现为「至少需要 Web 会话 JWT」。
//
// 与 Web 登录态隔离：未开启强制登录时只接受 Web 会话 JWT（Cookie/Bearer）或 API Key；
// 鉴权通过后把 scope 列表写入上下文，供 auth.RBACService.RequireScope / RequireReaderScope 复用。
func AuthGateMiddleware(authMW *middleware.AuthMiddleware, rbac *auth.RBACService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mode := CurrentAuthMode()

		switch mode {
		case AuthModeNone, AuthModeAPIKey:
			if mode == AuthModeNone {
				// none 模式（L6 加固）：要求携带有效凭证——JWT 的禁用态校验优先复用
				// authMW（非空时），未装配时退化为纯验签。强制登录与否都不再匿名放行。
				if requireWebSession(c, authMW, rbac) {
					return
				}
				utils.Warn("Open API 拒绝匿名访问：需要有效凭证",
					utils.String("path", c.Request.URL.Path))
				c.Header("WWW-Authenticate", `Bearer realm="Open API"`)
				c.Header("Cache-Control", cacheControlNoStore)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要有效凭证（Web 会话 JWT 或 API Key）"})
				return
			}
			// api_key 模式：只接受 API Key
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

// requireWebSession none 模式的凭证校验：接受有效 Web 会话 JWT（Cookie 或 Bearer，JWT 优先）
// 或有效 API Key（仅形如 keyID:secret 的凭证才尝试，避免无效 JWT 触发误导性格式告警）。
// 校验通过后向上下文写入身份信息供 RequireReaderScope / RequireScope 复用，返回 true。
// authMW 非空时复用其实时禁用态校验（缓存 + 回源；被禁用直接 403 并 Abort，同样返回 true）。
// 返回 false 表示凭证无效/缺失，调用方负责写 401。
func requireWebSession(c *gin.Context, authMW *middleware.AuthMiddleware, rbac *auth.RBACService) bool {
	if token := middleware.TokenFromCookie(c); token != "" {
		if claims, err := jwt.ParseJWT(token); err == nil {
			if authMW != nil && authMW.CheckUserDisabled(c, claims.UUID) {
				return true
			}
			injectWebSession(c, claims)
			return true
		}
	}
	if token := bearerToken(c); token != "" {
		if claims, err := jwt.ParseJWT(token); err == nil {
			if authMW != nil && authMW.CheckUserDisabled(c, claims.UUID) {
				return true
			}
			injectWebSession(c, claims)
			return true
		}
		// 仅在形如 keyID:secret 的凭证上尝试 API Key 校验
		if rbac != nil && strings.Contains(token, ":") {
			if userID, scopes, err := rbac.ValidateAPIKey(token); err == nil {
				c.Set(userIDContextKey, userID)
				c.Set(authTypeContextKey, AuthModeAPIKey)
				c.Set(scopesContextKey, splitScopes(scopes))
				return true
			}
		}
	}
	return false
}

// injectWebSession 把已验签的 Web 会话 JWT 身份写入上下文（供下游 scope/身份校验复用）。
func injectWebSession(c *gin.Context, claims *jwt.Claims) {
	c.Set(string(constants.ContextKeyUsername), claims.Username)
	c.Set(string(constants.ContextKeyRole), claims.Role)
	c.Set(string(constants.ContextKeyUserUUID), claims.UUID)
	c.Set(userIDContextKey, 0)
	c.Set(authTypeContextKey, AuthModeNone)
	c.Set(scopesContextKey, []string{})
}

// LoginRequiredWhenForced OpenAPI 文档（/openapi.json、/docs）专用守卫。
//
// L6 加固后与 AuthGateMiddleware 语义完全一致：任何 auth_mode 都不再匿名放行。
// 数据端点走 AuthGateMiddleware，文档端点走本守卫；两者共享同一套 requireWebSession
// 校验逻辑，保证矩阵 case 全覆盖、无匿名漏网：
//
//	requireLogin | auth_mode | openapi.json（与数据端点一致）
//	-------------+-----------+---------------------------------------------------
//	false        | none      | 需有效凭证（Web 会话 JWT 或 API Key），否则 401
//	false        | api_key   | 需有效 API Key，否则 401
//	true         | none      | 需有效凭证（Web 会话 JWT 或 API Key），否则 401
//	true         | api_key   | 需有效 API Key，否则 401
//
// authMW 用于 JWT 会话的禁用态校验：验签通过后调用 AuthMiddleware.CheckUserDisabled()
// 实时查库（缓存 + 回源），被禁用用户直接写入 403 并 Abort，避免只验签就放行。
// authMW 为 nil（测试场景）时退化为仅验签放行。
func LoginRequiredWhenForced(authMW *middleware.AuthMiddleware, rbac *auth.RBACService) gin.HandlerFunc {
	gate := AuthGateMiddleware(authMW, rbac)
	return func(c *gin.Context) {
		gate(c)
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

// RequireReaderScope 要求具备 open_api:reader（或更高）scope，用于原本公开的列表/搜索/下载接口。
// L6 加固后 auth_mode=none 也已注入 scope 上下文（Web 会话 JWT 注入空 scope，API Key 注入其 scope）：
// 因此统一按「上下文是否带 reader scope」判定——Web 会话 / 匿名一律 403，API Key 无 reader 也 403。
func RequireReaderScope(rbac *auth.RBACService) gin.HandlerFunc {
	return func(c *gin.Context) {
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
