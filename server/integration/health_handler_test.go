package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
// 回归：/health 为匿名白名单端点，响应不得包含 goVersion/goroutines 与存储绝对路径。
func TestHealthCheck(t *testing.T) {
	// 准备一个可辨认的存储目录，确保存储检查走"目录正常"分支
	marker := "health-leak-marker"
	rootDir := filepath.Join(t.TempDir(), marker)
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatalf("创建测试存储目录失败: %v", err)
	}

	prevRootDirs := appconfig.GlobalConfig.Storage.Public.RootDirs
	appconfig.GlobalConfig.Storage.Public.RootDirs = []appconfig.DirectoryConfig{{Path: rootDir}}
	defer func() { appconfig.GlobalConfig.Storage.Public.RootDirs = prevRootDirs }()

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
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("期望 data 为对象，实际 %T", response["data"])
	}

	// 保留字段必须存在：删除任一字段都会让前端静默回退（如版本显示为 —）
	for _, key := range []string{"status", "version", "checks", "timestamp"} {
		if _, exists := data[key]; !exists {
			t.Errorf("响应应包含 %s 字段", key)
		}
	}

	if _, exists := data["goVersion"]; exists {
		t.Error("响应不应包含 goVersion 字段")
	}
	if _, exists := data["goroutines"]; exists {
		t.Error("响应不应包含 goroutines 字段")
	}

	// 存储绝对路径不得出现在响应中
	if body := w.Body.String(); strings.Contains(body, marker) {
		t.Errorf("响应不应包含存储绝对路径，实际响应: %s", body)
	}
}

// TestHealthCheckStorageErrorBranch 回归：存储目录不存在时 checks["storage"] 必须为 error，
// 且错误 Message 不得回带目录绝对路径（本次改动去掉了路径），瘦身字段同样不得出现。
func TestHealthCheckStorageErrorBranch(t *testing.T) {
	// 用可辨认的末级目录名做标记：JSON 会转义 Windows 路径中的反斜杠，
	// 直接匹配完整路径在 Windows 上会失效，匹配末级名则与平台无关。
	marker := "health-missing-marker"
	missingDir := filepath.Join(t.TempDir(), marker)

	prevRootDirs := appconfig.GlobalConfig.Storage.Public.RootDirs
	appconfig.GlobalConfig.Storage.Public.RootDirs = []appconfig.DirectoryConfig{{Path: missingDir}}
	defer func() { appconfig.GlobalConfig.Storage.Public.RootDirs = prevRootDirs }()

	router := gin.New()
	handler := health.NewHealthHandler()
	router.GET("/api/v1/web/health", handler.Health)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/web/health", nil))

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("期望 data 为对象，实际 %T", response["data"])
	}

	checks, ok := data["checks"].(map[string]interface{})
	if !ok {
		t.Fatalf("期望 checks 为对象，实际 %T", data["checks"])
	}

	// checks["storage"] 为 CheckResult 结构，JSON 解包后是 map 结构
	storage, ok := checks["storage"].(map[string]interface{})
	if !ok {
		t.Fatalf("期望 checks.storage 为对象，实际 %T", checks["storage"])
	}
	if storage["status"] != "error" {
		t.Errorf("目录不存在时期望 checks.storage.status 为 error，实际 %v", storage["status"])
	}

	// 错误 Message 不得包含目录路径
	if body := w.Body.String(); strings.Contains(body, marker) {
		t.Errorf("响应不应包含存储绝对路径，实际响应: %s", body)
	}

	if _, exists := data["goVersion"]; exists {
		t.Error("响应不应包含 goVersion 字段")
	}
	if _, exists := data["goroutines"]; exists {
		t.Error("响应不应包含 goroutines 字段")
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
