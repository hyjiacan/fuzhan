package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/health"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestHealthCheck 测试健康检查
func TestHealthCheck(t *testing.T) {
	router := gin.New()
	handler := health.NewHealthHandler()
	router.GET("/api/v1/health", handler.Health)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
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
	router.GET("/api/v1/ready", handler.Ready)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
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
