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
// 不构造整个 runCtx，测试聚焦守卫语义（限流/统计/响应包装与本任务无关）。
// authMW 可为 nil（仅验签放行）；需要验证禁用态校验时传入带 users 表的真实 AuthMiddleware。
func newOpenAPIDocRouter(authMW *middleware.AuthMiddleware, rbac *auth.RBACService) *gin.Engine {
	router := gin.New()
	doc := router.Group("/api/open/v1")
	doc.Use(
		openapi.FeatureGateMiddleware(true),
		openapi.LoginRequiredWhenForced(authMW, rbac),
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
// 用例结束后恢复原密钥，避免测试密钥污染后续集成测试。
// 注意：UpdateJWTSecret 会把旧密钥压入历史列表（保留最近 3 个），恢复后测试密钥
// 仍可能在进程内被 ParseJWT 接受；这是测试进程内的已知取舍——不为此新增导出 API
// 重置历史密钥列表，只保证「当前密钥」被恢复。
func openAPIGuardJWT(t *testing.T) string {
	t.Helper()
	return openAPIGuardJWTFor(t, "uuid", false)
}

// openAPIGuardJWTFor 以指定 uuid/禁用标记签发测试 JWT，用于「禁用用户」真实校验用例。
func openAPIGuardJWTFor(t *testing.T, uuid string, disabled bool) string {
	t.Helper()
	prev := jwtpkg.GetJWTSecret()
	jwtpkg.UpdateJWTSecret([]byte("0123456789abcdef0123456789abcdef"))
	t.Cleanup(func() {
		// 测试密钥可能残留在 UpdateJWTSecret 的旧密钥列表，仅测试进程内有效
		jwtpkg.UpdateJWTSecret(append([]byte(nil), prev...))
	})
	token, err := jwtpkg.GenerateJWT(uuid, "testuser", "user", disabled)
	if err != nil {
		t.Fatalf("签发测试 JWT 失败: %v", err)
	}
	return token
}

// openAPIGuardUserDB 创建内存库并迁移 users 表；uuid 非空时插入指定禁用状态的用户。
func openAPIGuardUserDB(t *testing.T, uuid string, disabled bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	// :memory: 按连接隔离，限制单连接避免迁移后的表在其它连接上看不到
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层数据库连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("迁移用户表失败: %v", err)
	}
	if uuid != "" {
		user := models.User{
			UUID:         uuid,
			Username:     "disabled-test-user",
			PasswordHash: "test-hash",
			Role:         "user",
			Disabled:     disabled,
		}
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("写入测试用户失败: %v", err)
		}
	}
	return db
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

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), "", "")
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

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, rbac), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("api_key 模式下无 Key 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginRejectsAnonymous 强制登录 + none：匿名 401 且禁缓存。
func TestOpenAPIDocGuard_ForcedLoginRejectsAnonymous(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), "", "")
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

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("携带有效 Cookie JWT 期望 200，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAcceptsBearerJWT 强制登录 + none：Bearer JWT 可读文档。
func TestOpenAPIDocGuard_ForcedLoginAcceptsBearerJWT(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	token := openAPIGuardJWT(t)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("携带有效 Bearer JWT 期望 200，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginRejectsInvalidJWT 强制登录 + none：无效 JWT 仍 401。
func TestOpenAPIDocGuard_ForcedLoginRejectsInvalidJWT(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), "invalid-jwt", "invalid-jwt")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("携带无效 JWT 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAPIKeyModeRejectsAnonymous 强制登录 + api_key：无 Key 401。
func TestOpenAPIDocGuard_ForcedLoginAPIKeyModeRejectsAnonymous(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeAPIKey)
	rbac := auth.NewRBACService(openAPIGuardApiKeyService(t))

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, rbac), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("api_key 模式下无 Key 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAPIKeyModeAcceptsValidKey 强制登录 + api_key：有效 API Key 可读。
func TestOpenAPIDocGuard_ForcedLoginAPIKeyModeAcceptsValidKey(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeAPIKey)
	rbac, rawKey := openAPIGuardRBAC(t)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, rbac), "", rawKey)
	if w.Code != http.StatusOK {
		t.Fatalf("携带有效 API Key 期望 200，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginNoneAcceptsBearerAPIKey 强制登录 + none：Bearer 有效 API Key 可读文档
// （覆盖 LoginRequiredWhenForced 内的 rbac.ValidateAPIKey 分支）。
func TestOpenAPIDocGuard_ForcedLoginNoneAcceptsBearerAPIKey(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	rbac, rawKey := openAPIGuardRBAC(t)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, rbac), "", rawKey)
	if w.Code != http.StatusOK {
		t.Fatalf("强制登录 + none 携带有效 API Key 期望 200，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_UnknownAuthModeRejects 未知 auth_mode：沿用 AuthGate 的明确 500 错误。
func TestOpenAPIDocGuard_UnknownAuthModeRejects(t *testing.T) {
	setOpenAPIGuardConfig(t, false, "oauth2")

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), "", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("未知 auth_mode 期望 500，实际 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("配置错误拒绝时应返回 Cache-Control: no-store，实际 %q", cc)
	}
}

// TestOpenAPIDocGuard_ForcedLoginNoneRejectsInvalidAPIKey 强制登录 + none：格式合法但无效的 API Key 仍 401。
func TestOpenAPIDocGuard_ForcedLoginNoneRejectsInvalidAPIKey(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	rbac := auth.NewRBACService(openAPIGuardApiKeyService(t))
	// keyID:secret 形态（8 字符 + ":" + 64 字符，共 73 字符）通过格式校验，但库中不存在
	fakeKey := strings.Repeat("a", 8) + ":" + strings.Repeat("b", 64)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, rbac), "", fakeKey)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("强制登录 + none 携带无效 API Key 期望 401，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_APIKeyModeWithoutRBACReturns503 api_key 模式且鉴权服务未装配：fail-closed 503 且禁缓存。
func TestOpenAPIDocGuard_APIKeyModeWithoutRBACReturns503(t *testing.T) {
	setOpenAPIGuardConfig(t, false, openapi.AuthModeAPIKey)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(nil, nil), "", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("api_key 模式未装配 RBAC 期望 503，实际 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("服务不可用拒绝时应返回 Cache-Control: no-store，实际 %q", cc)
	}
}

// TestOpenAPIDocGuard_ForcedLoginRejectsDisabledUser 强制登录 + none：JWT 有效但用户被禁用 → 拒绝。
// 使用真实 AuthMiddleware + sqlite users 表验证复用 CheckUserDisabled 的禁用态校验。
// 注意：禁用用户返回 403（与 Web 登录态语义一致），故此处断言 403 而非 401，
// 关键是不得放行（200）。
func TestOpenAPIDocGuard_ForcedLoginRejectsDisabledUser(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	const uuid = "openapi-guard-disabled-uuid"
	db := openAPIGuardUserDB(t, uuid, true)
	authMW := middleware.NewAuthMiddleware(db)
	token := openAPIGuardJWTFor(t, uuid, true)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(authMW, nil), "", token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("有效 JWT 但用户被禁用期望 403，实际 %d", w.Code)
	}
}

// TestOpenAPIDocGuard_ForcedLoginAcceptsJWTForActiveUser 强制登录 + none：真实 AuthMiddleware + 未禁用用户可读。
func TestOpenAPIDocGuard_ForcedLoginAcceptsJWTForActiveUser(t *testing.T) {
	setOpenAPIGuardConfig(t, true, openapi.AuthModeNone)
	const uuid = "openapi-guard-active-uuid"
	db := openAPIGuardUserDB(t, uuid, false)
	authMW := middleware.NewAuthMiddleware(db)
	token := openAPIGuardJWTFor(t, uuid, false)

	w := doOpenAPIDocRequest(newOpenAPIDocRouter(authMW, nil), "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("未禁用用户携带有效 JWT 期望 200，实际 %d", w.Code)
	}
}
