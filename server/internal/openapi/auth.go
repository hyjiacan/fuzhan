package openapi

import (
	"net/http"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"

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
				c.Header("Cache-Control", "no-store")
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
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Open API 鉴权服务不可用"})
				return
			}
			token := bearerToken(c)
			if token == "" {
				c.Header("WWW-Authenticate", `Bearer realm="Open API"`)
				c.Header("Cache-Control", "no-store")
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 API Key"})
				return
			}
			userID, scopes, err := rbac.ValidateAPIKey(token)
			if err != nil {
				utils.Warn("Open API API Key 鉴权失败",
					utils.String("path", c.Request.URL.Path),
					utils.Err(err))
				c.Header("WWW-Authenticate", `Bearer realm="Open API"`)
				c.Header("Cache-Control", "no-store")
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "API Key 无效或已禁用"})
				return
			}
			c.Set(userIDContextKey, userID)
			c.Set(authTypeContextKey, "api_key")
			c.Set(scopesContextKey, splitScopes(scopes))
			c.Next()

		default:
			// oauth2 模式已废弃：给出明确升级提示，避免调用方误以为是临时故障
			msg := "open_api.auth_mode 配置无效，仅支持 none / api_key"
			if mode == "oauth2" {
				msg = "open_api.auth_mode=oauth2 已废弃，请改为 api_key（使用系统签发的 API Key + scope）"
			}
			utils.Warn("Open API 鉴权方式配置无效", utils.String("auth_mode", mode))
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": msg})
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
			c.Header("Cache-Control", "no-store")
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
