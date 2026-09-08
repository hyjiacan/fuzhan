package utils

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// SecurityConfig 安全配置（避免循环导入）
type SecurityConfig struct {
	TrustProxy     bool
	AllowedOrigins []string
}

// 全局安全配置访问器
var (
	securityConfigGetter func() SecurityConfig
	securityConfigMu     sync.RWMutex
)

// SetSecurityConfigGetter 设置安全配置获取器（由 main.go 在启动时调用）
func SetSecurityConfigGetter(getter func() SecurityConfig) {
	securityConfigMu.Lock()
	securityConfigGetter = getter
	securityConfigMu.Unlock()
}

// GetSecurityConfig 安全获取配置
func getSecurityConfig() SecurityConfig {
	securityConfigMu.RLock()
	defer securityConfigMu.RUnlock()
	if securityConfigGetter != nil {
		return securityConfigGetter()
	}
	// 默认配置
	return SecurityConfig{AllowedOrigins: []string{"*"}}
}

// EncodeResponse 编码并发送JSON响应
func EncodeResponse(w http.ResponseWriter, data interface{}, err string, statusCode int) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(map[string]interface{}{
		"success": err == "",
		"message": err,
		"data":    data,
	})
}

// PrintRequestInfo 打印请求信息
func PrintRequestInfo(r *http.Request) {
	Info("收到请求", String("remote_addr", r.RemoteAddr), String("method", r.Method), String("url", r.URL.String()))
}

// isProxyHeaderTrusted 检查是否应该信任代理头
func isProxyHeaderTrusted() bool {
	return getSecurityConfig().TrustProxy
}

// getRemoteAddrFromRequest 从 RemoteAddr 获取 IP
func getRemoteAddrFromRequest(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

// GetRealIP 获取真实的客户端IP地址
func GetRealIP(r *http.Request) string {
	// 只有在启用信任代理时才读取 X-Forwarded-For 和 X-Real-IP
	if isProxyHeaderTrusted() {
		// 检查 X-Forwarded-For 头部
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				ip := strings.TrimSpace(ips[0])
				if net.ParseIP(ip) != nil {
					return ip
				}
			}
		}

		// 检查 X-Real-IP 头部
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if net.ParseIP(xri) != nil {
				return xri
			}
		}
	}

	// 使用 RemoteAddr
	return getRemoteAddrFromRequest(r)
}

// GetClientIP 获取gin框架的客户端真实IP
func GetClientIP(c *gin.Context) string {
	// 只有在启用信任代理时才读取代理头
	if isProxyHeaderTrusted() {
		ip := c.GetHeader("X-Forwarded-For")
		if ip != "" {
			if strings.Contains(ip, ",") {
				ip = strings.Split(ip, ",")[0]
			}
			ip = strings.TrimSpace(ip)
			if net.ParseIP(ip) != nil {
				return ip
			}
		}

		ip = c.GetHeader("X-Real-IP")
		if ip != "" && net.ParseIP(ip) != nil {
			return ip
		}
	}

	// 使用 gin 的 ClientIP 方法
	return c.ClientIP()
}
