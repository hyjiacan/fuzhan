package auth

import (
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "fuzhan/pkg/response"
)

// RBACService RBAC 权限检查服务
type RBACService struct {
    apiKeySvc *ApiKeyService
}

// NewRBACService 创建 RBAC 服务
func NewRBACService(apiKeySvc *ApiKeyService) *RBACService {
    return &RBACService{apiKeySvc: apiKeySvc}
}

// ScopePermission scope 到 API 端点的映射
var ScopeEndpoints = map[string][]string{
    "open_api:reader": {
        "/api/open/v1/files/list",
        "/api/open/v1/files/search",
        "/api/open/v1/files/download",
    },
    "open_api:writer": {
        "/api/open/v1/files/list",
        "/api/open/v1/files/search",
        "/api/open/v1/files/download",
        "/api/open/v1/files/upload",
    },
    "open_api:admin": {}, // 空表示所有端点
}

// ScopeMethods 每个 scope 允许的 HTTP 方法
var ScopeMethods = map[string][]string{
    "open_api:reader": {"GET", "HEAD"},
    "open_api:writer": {"GET", "HEAD", "POST", "PUT", "DELETE"},
    "open_api:admin":  {"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH"},
}

// RequireScope 检查请求是否有指定的 scope 权限
func (s *RBACService) RequireScope(requiredScope string) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 尝试从 header 获取 API Key
        authHeader := c.GetHeader("Authorization")

        if authHeader == "" {
            // 尝试从查询参数获取
            authHeader = c.Query("api_key")
            if authHeader != "" {
                authHeader = "Bearer " + authHeader
            }
        }

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

// scopeHierarchy 定义 scope 的层级关系
var rbacScopeHierarchy = map[string][]string{
    "open_api:admin":  {"open_api:reader", "open_api:writer", "open_api:admin"},
    "open_api:writer": {"open_api:reader", "open_api:writer"},
    "open_api:reader": {"open_api:reader"},
}

// hasRequiredScope 检查 scopes 中是否有需要的 scope（含层级关系）
func (s *RBACService) hasRequiredScope(scopes, required string) bool {
    if scopes == "" {
        return false
    }
    for _, s := range strings.Split(scopes, ",") {
        s = strings.TrimSpace(s)
        if s == required || s == "*" {
            return true
        }
        if allowed, ok := rbacScopeHierarchy[s]; ok {
            for _, a := range allowed {
                if a == required {
                    return true
                }
            }
        }
    }
    return false
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
