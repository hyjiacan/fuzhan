package webdav

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/accessguard"
	xxh3pkg "fuzhan/pkg/xxh3"
	"github.com/gin-gonic/gin"
)

// UserAuthenticator verifies a username/password and returns the user's UUID
// and whether the user is disabled. Return err for invalid credentials.
type UserAuthenticator func(username, password string) (userUUID string, disabled bool, err error)

// contextKey is used for storing values in request contexts.
type contextKey string

const (
	// ContextKeyUserUUID is the context key for the authenticated user's UUID.
	ContextKeyUserUUID contextKey = "webdav_user_uuid"

	// ContextKeyClientIP is the context key for the client IP address.
	ContextKeyClientIP contextKey = "access_client_ip"

	// ContextKeyMethod is the context key for the HTTP method.
	ContextKeyMethod contextKey = "access_method"
)

// authCacheEntry holds a cached authentication result.
type authCacheEntry struct {
	userUUID  string
	disabled  bool
	expiresAt time.Time
}

// authCache is a simple TTL-based cache for bcrypt results.
// Key format: xxh3 hex digest of "username:password"
type authCache struct {
	mu      sync.RWMutex
	entries map[string]*authCacheEntry
	ttl     time.Duration
	maxSize int
}

func newAuthCache(ttl time.Duration, maxSize int) *authCache {
	return &authCache{
		entries: make(map[string]*authCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func (c *authCache) get(key string) (*authCacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		// Re-check entry after acquiring write lock (TOCTOU mitigation)
		entry, ok = c.entries[key]
		if !ok {
			c.mu.Unlock()
			return nil, false
		}
		if !time.Now().After(entry.expiresAt) {
			c.mu.Unlock()
			return entry, true
		}
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry, true
}

func (c *authCache) set(key string, entry *authCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Default expiration if not set
	if entry.expiresAt.IsZero() {
		entry.expiresAt = time.Now().Add(c.ttl)
	}
	if len(c.entries) >= c.maxSize {
		// Evict expired entries first
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		// If still full, evict one entry
		if len(c.entries) >= c.maxSize {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}
	c.entries[key] = entry
}

// globalAuthCache is the shared authentication cache for WebDAV Basic Auth.
var globalAuthCache = newAuthCache(5*time.Minute, 1000)

// BasicAuthMiddleware returns a Gin middleware that authenticates requests
// using HTTP Basic Authentication. The authenticator function is called to
// verify credentials against the user database.
func BasicAuthMiddleware(authenticate UserAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		userUUID, err := authenticateRequest(c.Request, authenticate)
		if err != nil {
			c.Header("WWW-Authenticate", `Basic realm="WebDAV Private Storage"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		// Store user UUID in request context for the PrivateFileSystem
		ctx := context.WithValue(c.Request.Context(), ContextKeyUserUUID, userUUID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// OptionalBasicAuthMiddleware returns a Gin middleware that optionally authenticates
// requests using HTTP Basic Authentication. If no credentials are provided, the request
// proceeds as anonymous (userUUID = ""). Invalid credentials still return 401.
func OptionalBasicAuthMiddleware(authenticate UserAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Store client IP for operation recording
		clientIP := c.ClientIP()
		ctx := context.WithValue(c.Request.Context(), ContextKeyClientIP, clientIP)
		ctx = context.WithValue(ctx, ContextKeyMethod, c.Request.Method)

		// 失败锁定预检：该 IP 触发过 Basic Auth 失败锁定（M3）则直接拒绝
		if accessguard.IsLocked(accessguard.SCOPE_WEBDAV, clientIP) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "访问过于频繁，请稍后再试"})
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No credentials → anonymous
			ctx = context.WithValue(ctx, ContextKeyUserUUID, "")
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		userUUID, err := authenticateRequest(c.Request, authenticate)
		if err != nil {
			// Invalid credentials → 401，并累计失败（达到阈值即锁定该 IP）
			accessguard.Fail(accessguard.SCOPE_WEBDAV, clientIP)
			c.Header("WWW-Authenticate", `Basic realm="WebDAV"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		ctx = context.WithValue(ctx, ContextKeyUserUUID, userUUID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// authenticateRequest parses the Authorization header and authenticates the user.
func authenticateRequest(r *http.Request, authenticate UserAuthenticator) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errUnauthorized
	}

	username, password, err := parseBasicAuth(authHeader)
	if err != nil {
		return "", errUnauthorized
	}

	// Check cache first (hashed to avoid storing plaintext passwords)
	cacheKey := cacheKeyHash(username + ":" + password)
	if entry, ok := globalAuthCache.get(cacheKey); ok {
		if entry.disabled {
			return "", errAccountDisabled
		}
		return entry.userUUID, nil
	}

	// Authenticate against database
	userUUID, disabled, err := authenticate(username, password)
	if err != nil {
		return "", errUnauthorized
	}

	// Cache the result
	globalAuthCache.set(cacheKey, &authCacheEntry{
		userUUID:  userUUID,
		disabled:  disabled,
		expiresAt: time.Now().Add(5 * time.Minute),
	})

	if disabled {
		return "", errAccountDisabled
	}

	return userUUID, nil
}

// parseBasicAuth parses an HTTP Basic Authentication header.
func parseBasicAuth(authHeader string) (username, password string, err error) {
	if !strings.HasPrefix(authHeader, "Basic ") {
		return "", "", errUnauthorized
	}

	payload, err := base64.StdEncoding.DecodeString(authHeader[6:])
	if err != nil {
		return "", "", errUnauthorized
	}

	pair := string(payload)
	colon := strings.IndexByte(pair, ':')
	if colon < 0 {
		return "", "", errUnauthorized
	}

	return pair[:colon], pair[colon+1:], nil
}

// cacheKeyHash returns the xxh3 hex digest of a string for cache key.
func cacheKeyHash(s string) string {
	return xxh3pkg.HashString(s)
}

var errUnauthorized = &authError{"未授权访问"}
var errAccountDisabled = &authError{"账户已被禁用"}

type authError struct {
	message string
}

func (e *authError) Error() string {
	return e.message
}

var _ error = (*authError)(nil)
