package openapi

import (
    "fmt"
    "net"
    "net/http"
    "sync"
    "time"

    "github.com/gin-gonic/gin"
    "fuzhan/pkg/response"
    "fuzhan/internal/utils"
)

// FeatureGateMiddleware 功能开关中间件
// 当 Open API 功能关闭时，返回 404 而不是暴露接口存在
func FeatureGateMiddleware(enabled bool) gin.HandlerFunc {
    return func(c *gin.Context) {
        if !enabled {
            response.HandleNotFound(c, "接口未找到")
            c.Abort()
            return
        }
        c.Next()
    }
}

// NewIPAccessConfigFromAppConfig 从 appconfig 的 OpenAPIConfig 创建 IPAccessConfig
func NewIPAccessConfigFromAppConfig(mode string, whitelist, blacklist []string) *IPAccessConfig {
    cfg := &IPAccessConfig{Mode: mode}
    switch mode {
    case "whitelist":
        cfg.CIDRList = whitelist
    case "blacklist":
        cfg.CIDRList = blacklist
    default:
        cfg.Mode = "disable"
    }
    if len(cfg.CIDRList) == 0 {
        cfg.CIDRList = []string{}
    }
    return cfg
}

// NewRateLimitConfigFromAppConfig 从 appconfig 的 OpenAPIConfig 创建 RateLimitConfig
func NewRateLimitConfigFromAppConfig(enabled bool, rpm, burst int) *RateLimitConfig {
    return &RateLimitConfig{
        Enabled:           enabled,
        RequestsPerMinute: rpm,
        BurstSize:         burst,
    }
}

// ==================== IP 访问控制 ====================

// IPAccessConfig IP 访问控制配置
type IPAccessConfig struct {
    Mode     string   `json:"mode" yaml:"mode"`
    CIDRList []string `json:"cidrList" yaml:"cidr_list"`
}

// IPAccessMiddleware IP 访问控制中间件
func IPAccessMiddleware(cfg *IPAccessConfig) gin.HandlerFunc {
    if cfg == nil || cfg.Mode == "disable" {
        return func(c *gin.Context) { c.Next() }
    }

    var parsedNets []*net.IPNet
    for _, cidr := range cfg.CIDRList {
        _, ipNet, err := net.ParseCIDR(cidr)
        if err == nil {
            parsedNets = append(parsedNets, ipNet)
        }
    }

    return func(c *gin.Context) {
        // 使用 utils.GetRealIP 获取客户端 IP：当 TrustProxy 配置为 false 时，
        // 严格取 RemoteAddr，避免攻击者伪造 X-Forwarded-For/X-Real-IP 绕过 IP 白名单。
        // gin 的 c.ClientIP() 默认信任所有代理，直接使用存在白名单绕过风险。
        clientIP := net.ParseIP(utils.GetRealIP(c.Request))
        if clientIP == nil {
            response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden, "无法解析客户端 IP")
            c.Abort()
            return
        }

        allowed := false
        if cfg.Mode == "whitelist" {
            allowed = len(parsedNets) == 0
            for _, ipNet := range parsedNets {
                if ipNet.Contains(clientIP) {
                    allowed = true
                    break
                }
            }
            if !allowed {
                response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden, "IP 不在白名单中")
                c.Abort()
                return
            }
        } else if cfg.Mode == "blacklist" {
            for _, ipNet := range parsedNets {
                if ipNet.Contains(clientIP) {
                    response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden, "IP 在黑名单中")
                    c.Abort()
                    return
                }
            }
        }

        c.Next()
    }
}

// ==================== 频率限制（令牌桶） ====================

// TokenBucket 令牌桶
type TokenBucket struct {
    mu        sync.Mutex
    rate      float64
    burst     int
    tokens    float64
    lastCheck time.Time
}

// NewTokenBucket 创建令牌桶
func NewTokenBucket(rate float64, burst int) *TokenBucket {
    return &TokenBucket{
        rate:      rate,
        burst:     burst,
        tokens:    float64(burst),
        lastCheck: time.Now(),
    }
}

// Allow 检查是否允许请求
func (tb *TokenBucket) Allow() bool {
    tb.mu.Lock()
    defer tb.mu.Unlock()

    now := time.Now()
    elapsed := now.Sub(tb.lastCheck).Seconds()
    tb.lastCheck = now

    tb.tokens += elapsed * tb.rate
    if tb.tokens > float64(tb.burst) {
        tb.tokens = float64(tb.burst)
    }

    if tb.tokens >= 1 {
        tb.tokens--
        return true
    }
    return false
}

// RateLimitConfig 频率限制配置
type RateLimitConfig struct {
    Enabled           bool `json:"enabled" yaml:"enabled"`
    RequestsPerMinute int  `json:"requestsPerMinute" yaml:"requests_per_minute"`
    BurstSize         int  `json:"burstSize" yaml:"burst_size"`
}

// RateLimiter 基于 IP 的频率限制器
type RateLimiter struct {
    mu      sync.RWMutex
    buckets map[string]*TokenBucket
    config  *RateLimitConfig
}

// NewRateLimiter 创建频率限制器
func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
    return &RateLimiter{
        buckets: make(map[string]*TokenBucket),
        config:  config,
    }
}

// UpdateConfig 更新配置
func (rl *RateLimiter) UpdateConfig(cfg *RateLimitConfig) {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    rl.config = cfg
}

// configSnapshot 在锁保护下读取当前限流配置引用
func (rl *RateLimiter) configSnapshot() *RateLimitConfig {
    rl.mu.RLock()
    defer rl.mu.RUnlock()
    return rl.config
}

// GetBucket 获取或创建 IP 对应的令牌桶
func (rl *RateLimiter) GetBucket(ip string) *TokenBucket {
    rl.mu.RLock()
    bucket, exists := rl.buckets[ip]
    rl.mu.RUnlock()

    if exists {
        return bucket
    }

    rl.mu.Lock()
    defer rl.mu.Unlock()

    if bucket, exists = rl.buckets[ip]; exists {
        return bucket
    }

    cfg := rl.config
    rate := float64(cfg.RequestsPerMinute) / 60.0
    bucket = NewTokenBucket(rate, cfg.BurstSize)
    rl.buckets[ip] = bucket

    return bucket
}

// RateLimitMiddleware 频率限制中间件
func RateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 通过锁安全读取配置，避免与 UpdateConfig 并发写入产生数据竞态
        cfg := rl.configSnapshot()
        if cfg == nil || !cfg.Enabled {
            c.Next()
            return
        }

        // 使用 utils.GetRealIP 获取客户端 IP，防止伪造代理头绕过限流
        clientIP := utils.GetRealIP(c.Request)
        bucket := rl.GetBucket(clientIP)

        if !bucket.Allow() {
            retryAfterSec := int(60.0 / float64(cfg.RequestsPerMinute))
            if retryAfterSec < 1 {
                retryAfterSec = 1
            }
            c.Header("Retry-After", fmt.Sprintf("%d", retryAfterSec))
            c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", cfg.RequestsPerMinute))
            response.HandleCustomError(c, http.StatusTooManyRequests, 4001, "请求过于频繁，请稍后再试")
            c.Abort()
            return
        }

        c.Next()
    }
}

// ==================== 调用量统计 ====================

// CallRecord 单次调用记录
type CallRecord struct {
    Timestamp  time.Time `json:"timestamp"`
    IP         string    `json:"ip"`
    Endpoint   string    `json:"endpoint"`
    Method     string    `json:"method"`
    StatusCode int       `json:"statusCode"`
    Duration   int64     `json:"duration"`
}

// APICallStat 调用统计
type APICallStat struct {
    mu         sync.RWMutex
    records    []CallRecord
    maxRecords int
}

// NewAPICallStat 创建调用统计
func NewAPICallStat(maxRecords int) *APICallStat {
    if maxRecords <= 0 {
        maxRecords = 10000
    }
    return &APICallStat{
        records:    make([]CallRecord, 0, maxRecords),
        maxRecords: maxRecords,
    }
}

// Record 记录一次调用
func (s *APICallStat) Record(call CallRecord) {
    s.mu.Lock()
    defer s.mu.Unlock()

    if len(s.records) >= s.maxRecords {
        trimIdx := s.maxRecords / 10
        s.records = s.records[trimIdx:]
    }
    s.records = append(s.records, call)
}

// StatsSummary 统计摘要
type StatsSummary struct {
    TotalCalls      int                    `json:"totalCalls"`
    ByEndpoint      map[string]int         `json:"byEndpoint"`
    ByStatusCode    map[int]int            `json:"byStatusCode"`
    TopIPs          []IPCallCount           `json:"topIPs"`
    HourlyBreakdown []HourlyCount           `json:"hourlyBreakdown"`
}

// IPCallCount IP 调用次数
type IPCallCount struct {
    IP    string `json:"ip"`
    Count int    `json:"count"`
}

// HourlyCount 每小时统计
type HourlyCount struct {
    Hour  string `json:"hour"`
    Count int    `json:"count"`
}

// GetSummary 获取统计摘要
func (s *APICallStat) GetSummary() *StatsSummary {
    s.mu.RLock()
    defer s.mu.RUnlock()

    summary := &StatsSummary{
        TotalCalls:      len(s.records),
        ByEndpoint:      make(map[string]int),
        ByStatusCode:    make(map[int]int),
        HourlyBreakdown: make([]HourlyCount, 0),
    }

    ipCounts := make(map[string]int)
    hourlyCounts := make(map[string]int)

    for _, r := range s.records {
        summary.ByEndpoint[r.Endpoint]++
        summary.ByStatusCode[r.StatusCode]++
        ipCounts[r.IP]++
        hour := r.Timestamp.Format("2006-01-02 15:00")
        hourlyCounts[hour]++
    }

    for ip, count := range ipCounts {
        summary.TopIPs = append(summary.TopIPs, IPCallCount{IP: ip, Count: count})
    }

    for hour, count := range hourlyCounts {
        summary.HourlyBreakdown = append(summary.HourlyBreakdown, HourlyCount{Hour: hour, Count: count})
    }

    return summary
}

// CallStatMiddleware 调用统计中间件
func CallStatMiddleware(stat *APICallStat) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()

        duration := time.Since(start).Milliseconds()
        stat.Record(CallRecord{
            Timestamp:  start,
            IP:         utils.GetRealIP(c.Request),
            Endpoint:   c.Request.URL.Path,
            Method:     c.Request.Method,
            StatusCode: c.Writer.Status(),
            Duration:   duration,
        })
    }
}
