package auth

import (
	"fmt"
	"net/http"
	"strings"

	"fuzhan/internal/models"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// RBACService RBAC 权限检查服务
type RBACService struct {
	apiKeySvc *ApiKeyService
}

// NewRBACService 创建 RBAC 服务
func NewRBACService(apiKeySvc *ApiKeyService) *RBACService {
	return &RBACService{apiKeySvc: apiKeySvc}
}

// RequireScope 检查请求是否有指定的 scope 权限
func (s *RBACService) RequireScope(requiredScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 鉴权闸门（OpenAPI AuthGate）已校验并写入 scopes 时直接复用，避免重复校验。
		if _, ok := c.Get("scopes"); ok {
			if !s.HasContextScope(c, requiredScope) {
				response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden, "权限不足，需要 scope: "+requiredScope)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 兼容路径：未经 AuthGate 的接口，直接从 Authorization 头校验 API Key
		// （凭证不允许走 URL 查询参数，避免落日志/Referer）
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			response.HandleCustomError(c, http.StatusUnauthorized, response.CodeUnauthorized, "缺少认证信息")
			c.Abort()
			return
		}

		// 提取 Bearer Token
		var token string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else if strings.HasPrefix(authHeader, "bearer ") {
			token = strings.TrimPrefix(authHeader, "bearer ")
		} else {
			token = authHeader
		}

		// 验证 API Key
		userID, scopes, err := s.apiKeySvc.ValidateApiKey(token)
		if err != nil {
			response.HandleCustomError(c, http.StatusUnauthorized, response.CodeUnauthorized, "API Key 无效: "+err.Error())
			c.Abort()
			return
		}

		// 检查 scope 权限
		if !s.hasRequiredScope(scopes, requiredScope) {
			response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden, "权限不足，需要 scope: "+requiredScope)
			c.Abort()
			return
		}

		// 设置用户信息到上下文
		c.Set("userID", userID)
		c.Set("scopes", scopes)
		c.Set("authType", "api_key")

		c.Next()
	}
}

// RequireScopeOptional 可选 scope 检查（没有认证信息也可以继续）
func (s *RBACService) RequireScopeOptional(requiredScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			authHeader = c.Query("api_key")
			if authHeader != "" {
				authHeader = "Bearer " + authHeader
			}
		}

		if authHeader == "" {
			c.Next()
			return
		}

		var token string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			token = authHeader
		}

		userID, scopes, err := s.apiKeySvc.ValidateApiKey(token)
		if err == nil && s.hasRequiredScope(scopes, requiredScope) {
			c.Set("userID", userID)
			c.Set("scopes", scopes)
			c.Set("authType", "api_key")
		}

		c.Next()
	}
}

// hasRequiredScope 检查 scopes 中是否有需要的 scope（含层级关系）。
// 层级定义统一放在 models.ScopeContains，避免 RBAC 与 API Key 两套规则分叉。
func (s *RBACService) hasRequiredScope(scopes, required string) bool {
	return models.ScopeContains(scopes, required)
}

// scopeContains 检查逗号分隔的 scope 列表是否包含某个 scope
func (s *RBACService) scopeContains(scopes, scope string) bool {
	if scopes == scope {
		return true
	}
	for _, s := range strings.Split(scopes, ",") {
		s = strings.TrimSpace(s)
		if s == scope {
			return true
		}
	}
	return false
}

// ValidateAPIKey 校验 API Key 并返回所属用户与 scope（供 Open API 鉴权闸门复用）。
func (s *RBACService) ValidateAPIKey(rawKey string) (uint, string, error) {
	if s == nil || s.apiKeySvc == nil {
		return 0, "", fmt.Errorf("API Key 服务未初始化")
	}
	return s.apiKeySvc.ValidateApiKey(rawKey)
}

// HasContextScope 判断鉴权中间件写入上下文的 scope 是否满足 required。
// 支持 []string（AuthGate 写入）与逗号分隔字符串（兼容旧写法）。
func (s *RBACService) HasContextScope(c *gin.Context, required string) bool {
	v, ok := c.Get("scopes")
	if !ok {
		return false
	}
	switch scopes := v.(type) {
	case []string:
		return s.hasRequiredScope(strings.Join(scopes, ","), required)
	case string:
		return s.hasRequiredScope(scopes, required)
	default:
		return false
	}
}
