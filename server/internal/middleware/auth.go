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

// userStatusEntry 一条用户禁用状态缓存记录
type userStatusEntry struct {
	disabled bool
	expire   time.Time
}

// userStatusCache 缓存用户禁用状态（正反两面），避免正常用户每个请求都回源查库。
// 禁用与正常结果统一走短 TTL：命中即免查库，管理员禁用/恢复在 TTL(30s) 内生效。
type userStatusCache struct {
	mu      sync.RWMutex
	entries map[string]userStatusEntry // uuid -> 禁用状态 + 过期时间
}

var userDisabledCache = &userStatusCache{entries: make(map[string]userStatusEntry)}

const disabledCacheTTL = 30 * time.Second

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
		// 优先从 Authorization 头部读取；缺失时尝试 query token（供下载/新窗口等无法携带请求头的场景）
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			tokenString = c.Query("token")
		}
		if tokenString == "" {
			utils.HandleUnauthorized(c, "缺少认证信息")
			c.Abort()
			return
		}
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")

		// 验证token
		claims, err := jwt.ParseJWT(tokenString)
		if err != nil {
			utils.HandleUnauthorized(c, "无效的认证令牌")
			c.Abort()
			return
		}

		// 检查用户是否被禁用（优先查缓存，未命中则查库，正反结果都写短 TTL 缓存）
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
			// 记录不存在视为正常，统一写入缓存，避免正常用户重复回源
			userDisabledCache.set(claims.UUID, user.Disabled)
			if user.Disabled {
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
