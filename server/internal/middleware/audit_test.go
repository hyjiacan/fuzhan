package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/security"

	"github.com/gin-gonic/gin"
)

// auditFakeStore 内存安全事件存储，仅实现 CreateBatch
type auditFakeStore struct {
	repositories.SecurityEventStore
	mu     sync.Mutex
	events []*models.SecurityEvent
}

func (f *auditFakeStore) CreateBatch(evs []*models.SecurityEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, evs...)
	return nil
}

// collectRejected 构造「被拒即入库」的完整中间件链，执行一次请求并返回刷盘后的安全事件。
// markGuard=true 模拟 WebDAV 中间件 abort 前打上下文标记的行为。
func collectRejected(t *testing.T, method, path string, markGuard bool, status int) []*models.SecurityEvent {
	t.Helper()
	return collectRejectedWith(t, method, path, markGuard, status, "")
}

// collectRejectedWith 在 collectRejected 基础上指定 401 原因（missing/expired/invalid），
// 模拟认证中间件 abort 前写入 ContextKeyAuthFailureReason 的行为。
func collectRejectedWith(t *testing.T, method, path string, markGuard bool, status int, reason string) []*models.SecurityEvent {
	t.Helper()
	store := &auditFakeStore{}
	security.InitWriter(store, 1, 64)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditMiddleware())
	h := func(c *gin.Context) {
		if markGuard {
			c.Set(string(constants.ContextKeySecurityGuard), models.GuardWebDAV)
		}
		if reason != "" {
			c.Set(string(constants.ContextKeyAuthFailureReason), reason)
		}
		c.AbortWithStatus(status)
	}
	r.Handle(method, path, h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.9:12345"
	r.ServeHTTP(w, req)
	if w.Code != status {
		t.Fatalf("请求状态码应为 %d，got %d", status, w.Code)
	}
	security.StopWriter()
	return store.events
}

func TestAudit_ReadUnauthorized_NotRecorded(t *testing.T) {
	// 普通读取请求的 401（令牌过期、未登录浏览）不构成安全事件
	for _, path := range []string{
		"/api/v1/admin/security/events/unread-summary", // 轮询类
		"/api/v1/files", // 普通读取
	} {
		if evs := collectRejected(t, "GET", path, false, http.StatusUnauthorized); len(evs) != 0 {
			t.Fatalf("GET %s 401 不应入库，got %d 条", path, len(evs))
		}
	}
}

func TestAudit_AdminUnauthorized_Recorded(t *testing.T) {
	// 未认证访问管理接口属于越过认证边界，单次即入库
	evs := collectRejected(t, "GET", "/api/v1/web/admin/users", false, http.StatusUnauthorized)
	if len(evs) != 1 || evs[0].EventType != models.EventAuthUnauthorized {
		t.Fatalf("未认证访问管理接口应入库 1 条 auth.unauthorized，got %+v", evs)
	}
}

func TestAudit_SessionMaintenanceUnauthorized_NotRecorded(t *testing.T) {
	// 会话维护接口的 401 属于令牌过期，不升级为安全事件
	evs := collectRejected(t, "POST", "/api/v1/web/auth/refresh", false, http.StatusUnauthorized)
	if len(evs) != 0 {
		t.Fatalf("会话维护接口 401 不应入库，got %+v", evs)
	}
}

func TestAudit_WriteUnauthorized_StillRecorded(t *testing.T) {
	evs := collectRejected(t, "POST", "/api/v1/auth/login", false, http.StatusUnauthorized)
	if len(evs) != 1 || evs[0].EventType != models.EventAuthUnauthorized {
		t.Fatalf("写操作 401 必须保留入库，got %+v", evs)
	}
	if evs[0].Guard != models.GuardAuth {
		t.Fatalf("无上下文标记时 guard 应缺省 auth，got %q", evs[0].Guard)
	}
}

func TestAudit_ForbiddenAndLimited_Recorded(t *testing.T) {
	evs := collectRejected(t, "GET", "/api/v1/files", false, http.StatusForbidden)
	if len(evs) != 1 || evs[0].EventType != models.EventAccessForbidden {
		t.Fatalf("403 应入库 access.forbidden，got %+v", evs)
	}
	evs = collectRejected(t, "GET", "/api/v1/files", false, http.StatusTooManyRequests)
	if len(evs) != 1 || evs[0].EventType != models.EventRateLimited {
		t.Fatalf("429 应入库 rate.limited，got %+v", evs)
	}
}

func TestAudit_GuardContextMarker_OverridesAuth(t *testing.T) {
	evs := collectRejected(t, "GET", "/api/v1/webdav/x", true, http.StatusUnauthorized)
	if len(evs) != 1 {
		t.Fatalf("应入库 1 条，got %d", len(evs))
	}
	if evs[0].Guard != models.GuardWebDAV {
		t.Fatalf("上下文标记应覆盖缺省 guard，got %q", evs[0].Guard)
	}
}

func TestAudit_MissingCredentialUnauthorized_NotRecorded(t *testing.T) {
	// 未携带凭证（正常未登录）访问管理接口/写接口：日志降级，且不单次入库
	security.ResetAuthFailureState()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/web/admin/users"},
		{"POST", "/api/v1/web/private/uploads/sessions"},
	} {
		evs := collectRejectedWith(t, tc.method, tc.path, false, http.StatusUnauthorized, constants.AuthFailureMissing)
		if len(evs) != 0 {
			t.Fatalf("%s %s 未带凭证的 401 不应入库，got %+v", tc.method, tc.path, evs)
		}
	}
}

func TestAudit_ExpiredCredentialUnauthorized_NotRecorded(t *testing.T) {
	// 凭证过期属于登录态自然失效，同样不单次入库
	security.ResetAuthFailureState()
	evs := collectRejectedWith(t, "GET", "/api/v1/web/admin/users", false, http.StatusUnauthorized, constants.AuthFailureExpired)
	if len(evs) != 0 {
		t.Fatalf("凭证过期的 401 不应入库，got %+v", evs)
	}
}

func TestAudit_InvalidCredentialUnauthorized_Recorded(t *testing.T) {
	// 携带伪造/篡改凭证的 401 属于可疑行为，单次即入库
	security.ResetAuthFailureState()
	evs := collectRejectedWith(t, "GET", "/api/v1/web/admin/users", false, http.StatusUnauthorized, constants.AuthFailureInvalid)
	if len(evs) != 1 || evs[0].EventType != models.EventAuthUnauthorized {
		t.Fatalf("伪造凭证的 401 应入库 1 条 auth.unauthorized，got %+v", evs)
	}
}

func TestAudit_MissingCredentialThresholdAggregates(t *testing.T) {
	// 未带凭证的 401 不逐条入库，但同一 IP 超阈值仍聚合 1 条，保留批量探测检测能力
	security.ResetAuthFailureState()
	var total int
	var aggregated []*models.SecurityEvent
	for i := 0; i < 80; i++ {
		evs := collectRejectedWith(t, "GET", "/api/v1/files", false, http.StatusUnauthorized, constants.AuthFailureMissing)
		total += len(evs)
		if len(evs) > 0 {
			aggregated = evs
		}
	}
	if total != 1 {
		t.Fatalf("同 IP 大量未登录访问应只聚合 1 条事件，got %d", total)
	}
	if aggregated[0].EventType != models.EventAuthUnauthorized || aggregated[0].Severity != models.SeverityMedium {
		t.Fatalf("聚合事件类型/级别不符，got %+v", aggregated[0])
	}
}
