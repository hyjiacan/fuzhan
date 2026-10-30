package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/openapi"
	jwtpkg "fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const openAPIDocPath = "/api/open/v1/openapi.json"

// newOpenAPIDocRouter 构造只挂守卫生效链路的最小引擎：功能开关 + LoginRequiredWhenForced + 桩 handler。
// 不构造整个 runCtx，测试聚焦守卫语义（功能开关/限流/统计/响应包装与本任务无关）。
func newOpenAPIDocRouter(rbac *auth.RBACService) *gin.Engine {
	router := gin.New()
	doc := router.Group("/api/open/v1")
	doc.Use(
		openapi.FeatureGateMiddleware(true),
		openapi.LoginRequiredWhenForced(rbac),
	)
	doc.GET("/openapi.json", func(c *gin.Context) {
		c.String(http.StatusOK, `{"openapi":"3.0.0"}`)
	})
	return router
}

// setOpenAPIGuardConfig 设置守卫依赖的配置，并在用例结束后恢复，避免污染其它集成测试。
func setOpenAPIGuardConfig(t *testing.T, requireLogin bool, authMode string) {
	t.Helper()
	prevRequire := appconfig.GlobalConfig.Account.RequireLogin
	prevMode := appconfig.GlobalConfig.OpenAPI.AuthMode
	appconfig.GlobalConfig.Account.RequireLogin = requireLogin
	appconfig.GlobalConfig.OpenAPI.AuthMode = authMode
	t.Cleanup(func() {
		appconfig.GlobalConfig.Account.RequireLogin = prevRequire
		appconfig.GlobalConfig.OpenAPI.AuthMode = prevMode
	})
}

// openAPIGuardJWT 固定测试密钥并签发一个合法 Web 会话 JWT。
func openAPIGuardJWT(t *testing.T) string {
	t.Helper()
	jwtpkg.UpdateJWTSecret([]byte("0123456789abcdef0123456789abcdef"))
	token, err := jwtpkg.GenerateJWT("uuid", "testuser", "user", false)
	if err != nil {
		t.Fatalf("签发测试 JWT 失败: %v", err)
	}
	return token
}

// openAPIGuardApiKeyService 创建内存库上的 API Key 服务。
func openAPIGuardApiKeyService(t *testing.T) *auth.ApiKeyService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.ApiKey{}); err != nil {
		t.Fatalf("迁移 API Key 表失败: %v", err)
	}
	return auth.NewApiKeyService(db)
}

// openAPIGuardRBAC 返回已装配的 RBAC 服务与一个有效明文 API Key。
func openAPIGuardRBAC(t *testing.T) (*auth.RBACService, string) {
	t.Helper()
	svc := openAPIGuardApiKeyService(t)
	resp, err := svc.CreateApiKey(auth.CreateApiKeyRequest{
		Name:   "openapi-doc-guard",
		UserID: 1,
		Scopes: models.ScopeOpenAPIReader,
	})
	if err != nil {
		t.Fatalf("创建测试 API Key 失败: %v", err)
	}
	return auth.NewRBACService(svc), resp.RawKey
}

// doOpenAPIDocRequest 发起一次文档请求，可携带 Cookie JWT 或 Bearer 凭证（空串表示不携带）。
func doOpenAPIDocRequest(router *gin.Engine, cookieToken, bearerToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, openAPIDocPath, nil)
	if cookieToken != "" {
		req.AddCookie(&http.Cookie{Name: middleware.CookieTokenName, Value: cookieToken})
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestOpenAPIDocGuard_AnonymousWhenNotForced 非强制登录 + none：保持历史匿名 200。
func TestOpenAPIDocGuard_AnonymousWhenNotForced(t *testing.T) {
	setOpenAPIGuardConfig(t, false, openapi.AuthModeNone)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil), "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("非强制登录匿名访问期望 200，实际 %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `"openapi"`) {
		t.Fatalf("响应应包含 openapi 内容，实际: %s", body)
	}
}

// TestOpenAPIDocGuard_APIKeyModeRequiresKeyWithoutLogin 非强制登录 + api_key：无 Key 必须 401（防回归）。
func TestOpenAPIDocGuard_APIKeyModeRequiresKeyWithoutLogin(t *testing.T) {
	setOpenAPIGuardConfig(t, false, openapi.AuthModeAPIKey)

	// 注意：AuthGate 在服务未装配（rbac=nil）时是 fail-closed 503，故此处必须装配一个无 Key 的服务。
	rbac := auth.NewRBACService(openAPIGuardApiKeyService(t))

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(rbac), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("api_key 模式下无 Key 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginRejectsAnonymous 强制登录 + none：匿名 401 且禁缓存。
func TestOpenAPIDocGuard_ForcedLoginRejectsAnonymous(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("强制登录匿名访问期望 401，实际 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("强制登录拒绝时应返回 Cache-Control: no-store，实际 %q", cc)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAcceptsCookieJWT 强制登录 + none：Web 会话 Cookie 可读文档。
func TestOpenAPIDocGuard_ForcedLoginAcceptsCookieJWT(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	token := openAPIGuardJWT(t)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil), token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("携带有效 Cookie JWT 期望 200，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAcceptsBearerJWT 强制登录 + none：Bearer JWT 可读文档。
func TestOpenAPIDocGuard_ForcedLoginAcceptsBearerJWT(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	token := openAPIGuardJWT(t)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil), "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("携带有效 Bearer JWT 期望 200，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginRejectsInvalidJWT 强制登录 + none：无效 JWT 仍 401。
func TestOpenAPIDocGuard_ForcedLoginRejectsInvalidJWT(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil), "invalid-jwt", "invalid-jwt")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("携带无效 JWT 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAPIKeyModeRejectsAnonymous 强制登录 + api_key：无 Key 401。
func TestOpenAPIDocGuard_ForcedLoginAPIKeyModeRejectsAnonymous(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeAPIKey)
	rbac := auth.NewRBACService(openAPIGuardApiKeyService(t))

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(rbac), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("api_key 模式下无 Key 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAPIKeyModeAcceptsValidKey 强制登录 + api_key：有效 API Key 可读。
func TestOpenAPIDocGuard_ForcedLoginAPIKeyModeAcceptsValidKey(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeAPIKey)
	rbac, rawKey := openAPIGuardRBAC(t)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(rbac), "", rawKey)
	if w.Code != http.StatusOK {
		t.Fatalf("携带有效 API Key 期望 200，实际 %d", w.Code)
	}
}
