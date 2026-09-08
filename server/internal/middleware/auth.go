package middleware

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// disabledCache 缓存已禁用用户的 UUID，避免每请求都查库
type disabledCache struct {
	mu      sync.RWMutex
	entries map[string]time.Time // uuid -> 缓存过期时间
}

var userDisabledCache = &disabledCache{entries: make(map[string]time.Time)}

const disabledCacheTTL = 30 * time.Second

// init 启动 disabledCache 后台清理协程
func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			userDisabledCache.cleanExpired()
		}
	}()
}

func (dc *disabledCache) isDisabled(uuid string) (bool, bool) {
	dc.mu.RLock()
	expire, ok := dc.entries[uuid]
	dc.mu.RUnlock()
	if !ok || utils.Now().After(expire) {
		return false, false
	}
	return true, true // 已缓存为禁用状态
}

func (dc *disabledCache) setDisabled(uuid string) {
	dc.mu.Lock()
	dc.entries[uuid] = utils.Now().Add(disabledCacheTTL)
	dc.mu.Unlock()
}

// cleanExpired 定期清理过期条目，防止禁用用户积累
func (dc *disabledCache) cleanExpired() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	now := utils.Now()
	for uuid, expire := range dc.entries {
		if now.After(expire) {
			delete(dc.entries, uuid)
		}
	}
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
		// 检查Authorization头部
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			utils.HandleUnauthorized(c, "缺少认证信息")
			c.Abort()
			return
		}

		// 检查Bearer token格式
		if !strings.HasPrefix(authHeader, "Bearer ") {
			utils.HandleUnauthorized(c, "认证格式错误")
			c.Abort()
			return
		}

		// 提取token
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// 验证token
		claims, err := jwt.ParseJWT(tokenString)
		if err != nil {
			utils.HandleUnauthorized(c, "无效的认证令牌")
			c.Abort()
			return
		}

		// 检查用户是否被禁用（优先查缓存，未命中则查库）
		if disabled, cached := userDisabledCache.isDisabled(claims.UUID); disabled {
			utils.HandleForbidden(c, "账号已被禁用")
			c.Abort()
			return
		} else if !cached && am.db != nil {
			var user models.User
			err := am.db.Select("disabled").Where("uuid = ?", claims.UUID).First(&user).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				// 数据库错误（非记录不存在），认证服务暂时不可用
				utils.Error("检查用户状态失败", utils.Err(err), utils.String("uuid", claims.UUID))
				utils.HandleInternalServerError(c, "认证服务暂时不可用")
				c.Abort()
				return
			}
			if user.Disabled {
				userDisabledCache.setDisabled(claims.UUID)
				utils.HandleForbidden(c, "账号已被禁用")
				c.Abort()
				return
			}
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
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.Next()
			return
		}
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := jwt.ParseJWT(tokenString)
		if err != nil {
			c.Next()
			return
		}
		// 实时查库检查禁用状态
		if disabled, cached := userDisabledCache.isDisabled(claims.UUID); disabled {
			c.Next()
			return
		} else if !cached && am.db != nil {
			var user models.User
			if err := am.db.Select("disabled").Where("uuid = ?", claims.UUID).First(&user).Error; err == nil && user.Disabled {
				userDisabledCache.setDisabled(claims.UUID)
				c.Next()
				return
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
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")

		// 内容安全策略
		// 'unsafe-eval' 用于 WASM 编译场景，删除前需确认无 WASM 相关功能
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:")

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
