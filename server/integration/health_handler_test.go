package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/health"
	"fuzhan/internal/middleware"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestHealthCheck 测试健康检查
func TestHealthCheck(t *testing.T) {
	router := gin.New()
	handler := health.NewHealthHandler()
	router.GET("/api/v1/web/health", handler.Health)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}
}

// TestReadyCheck 测试就绪检查
func TestReadyCheck(t *testing.T) {
	router := gin.New()
	handler := health.NewHealthHandler()
	// /ready 已移入 web 组（经 LoginGate），注册路径为 /api/v1/web/ready
	router.GET("/api/v1/web/ready", handler.Ready)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}
}

// TestReadyRouteRequiresLoginWhenForced 回归：/ready 已移入 web 组（经 LoginGate）。
// 强制登录开启时匿名访问 /api/v1/web/ready 应被拦截，旧的 /api/v1/ready 不再注册（404）。
func TestReadyRouteRequiresLoginWhenForced(t *testing.T) {
	prev := appconfig.GlobalConfig.Account.RequireLogin
	appconfig.GlobalConfig.Account.RequireLogin = true
	defer func() { appconfig.GlobalConfig.Account.RequireLogin = prev }()

	router := gin.New()
	api := router.Group("/api/v1")
	web := api.Group("/web")
	web.Use(middleware.NewAuthMiddleware(nil).LoginGate())
	handler := health.NewHealthHandler()
	web.GET("/ready", handler.Ready)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/web/ready", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("/api/v1/web/ready 匿名访问期望 401，实际 %d", w.Code)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("/api/v1/ready 期望 404，实际 %d", w.Code)
	}
}
