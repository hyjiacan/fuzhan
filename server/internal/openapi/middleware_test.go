package openapi

import (
    "net/http"
    "net/http/httptest"
    "sync"
    "testing"
    "time"

    "github.com/gin-gonic/gin"
)

func init() {
    gin.SetMode(gin.TestMode)
}

// ==================== 令牌桶测试 ====================

func TestTokenBucket_Allow(t *testing.T) {
    tb := NewTokenBucket(10, 5)
    for i := 0; i < 5; i++ {
        if !tb.Allow() {
            t.Errorf("request %d should be allowed (burst)", i+1)
        }
    }
    if tb.Allow() {
        t.Error("6th request should be denied")
    }
}

func TestTokenBucket_Refill(t *testing.T) {
    tb := NewTokenBucket(100, 5)
    for i := 0; i < 5; i++ {
        tb.Allow()
    }
    time.Sleep(50 * time.Millisecond)
    if !tb.Allow() {
        t.Log("Tokens should have been refilled after 50ms")
    }
}

func TestTokenBucket_Concurrent(t *testing.T) {
    tb := NewTokenBucket(1000, 100)
    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            tb.Allow()
        }()
    }
    wg.Wait()
}

// ==================== IP 访问控制测试 ====================

func testRequest(handler gin.HandlerFunc, remoteAddr string) *httptest.ResponseRecorder {
    r := gin.New()
    r.Use(handler)
    r.GET("/", func(c *gin.Context) {
        c.JSON(200, gin.H{"ok": true})
    })
    req := httptest.NewRequest("GET", "/", nil)
    req.RemoteAddr = remoteAddr
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    return w
}

func TestIPAccessMiddleware_Whitelist(t *testing.T) {
    w := testRequest(IPAccessMiddleware(&IPAccessConfig{
        Mode: "whitelist", CIDRList: []string{"10.0.0.0/8"},
    }), "10.0.0.1:12345")
    if w.Code != http.StatusOK {
        t.Errorf("expected 200 for whitelisted IP, got %d", w.Code)
    }
}

func TestIPAccessMiddleware_WhitelistDeny(t *testing.T) {
    w := testRequest(IPAccessMiddleware(&IPAccessConfig{
        Mode: "whitelist", CIDRList: []string{"10.0.0.0/8"},
    }), "192.168.1.1:12345")
    if w.Code != http.StatusForbidden {
        t.Errorf("expected 403 for non-whitelist IP, got %d", w.Code)
    }
}

func TestIPAccessMiddleware_Blacklist(t *testing.T) {
    w := testRequest(IPAccessMiddleware(&IPAccessConfig{
        Mode: "blacklist", CIDRList: []string{"10.0.0.0/8"},
    }), "10.0.0.1:12345")
    if w.Code != http.StatusForbidden {
        t.Errorf("expected 403 for blacklisted IP, got %d", w.Code)
    }
}

func TestIPAccessMiddleware_BlacklistAllow(t *testing.T) {
    w := testRequest(IPAccessMiddleware(&IPAccessConfig{
        Mode: "blacklist", CIDRList: []string{"10.0.0.0/8"},
    }), "192.168.1.1:12345")
    if w.Code != http.StatusOK {
        t.Errorf("expected 200 for non-blacklist IP, got %d", w.Code)
    }
}

func TestIPAccessMiddleware_Disabled(t *testing.T) {
    w := testRequest(IPAccessMiddleware(&IPAccessConfig{Mode: "disable"}), "10.0.0.1:12345")
    if w.Code != http.StatusOK {
        t.Errorf("expected 200 when disabled, got %d", w.Code)
    }
}

func TestIPAccessMiddleware_NilConfig(t *testing.T) {
    w := testRequest(IPAccessMiddleware(nil), "10.0.0.1:12345")
    if w.Code != http.StatusOK {
        t.Errorf("expected 200 with nil config, got %d", w.Code)
    }
}

// ==================== 频率限制器测试 ====================

func TestRateLimiter_ConfigUpdate(t *testing.T) {
    rl := NewRateLimiter(&RateLimitConfig{
        Enabled: true, RequestsPerMinute: 60, BurstSize: 5,
    })
    bucket := rl.GetBucket("10.0.0.1")
    if bucket == nil {
        t.Fatal("expected token bucket")
    }
    rl.UpdateConfig(&RateLimitConfig{
        Enabled: false, RequestsPerMinute: 30, BurstSize: 10,
    })
    bucket2 := rl.GetBucket("10.0.0.2")
    if bucket2 == nil {
        t.Fatal("expected token bucket after config update")
    }
}

// ==================== 调用统计测试 ====================

func TestAPICallStat_Record(t *testing.T) {
    stat := NewAPICallStat(100)
    stat.Record(CallRecord{IP: "10.0.0.1", Endpoint: "/files/list", Method: "GET", StatusCode: 200, Duration: 50})
    stat.Record(CallRecord{IP: "10.0.0.2", Endpoint: "/files/list", Method: "GET", StatusCode: 200, Duration: 30})
    stat.Record(CallRecord{IP: "10.0.0.1", Endpoint: "/files/search", Method: "GET", StatusCode: 404, Duration: 10})

    summary := stat.GetSummary()
    if summary.TotalCalls != 3 {
        t.Errorf("expected 3 calls, got %d", summary.TotalCalls)
    }
    if summary.ByEndpoint["/files/list"] != 2 {
        t.Errorf("expected 2 calls to /files/list, got %d", summary.ByEndpoint["/files/list"])
    }
    if summary.ByStatusCode[404] != 1 {
        t.Errorf("expected 1 404, got %d", summary.ByStatusCode[404])
    }
}

func TestAPICallStat_Trim(t *testing.T) {
    stat := NewAPICallStat(20)
    for i := 0; i < 30; i++ {
        stat.Record(CallRecord{IP: "10.0.0.1", Endpoint: "/test", Method: "GET", StatusCode: 200, Duration: 1})
    }
    summary := stat.GetSummary()
    if summary.TotalCalls > 21 {
        t.Errorf("expected trimmed calls <= 20, got %d", summary.TotalCalls)
    }
}

func TestAPICallStat_Empty(t *testing.T) {
    stat := NewAPICallStat(100)
    summary := stat.GetSummary()
    if summary.TotalCalls != 0 {
        t.Errorf("expected 0 calls, got %d", summary.TotalCalls)
    }
}
