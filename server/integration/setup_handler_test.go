package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/setup"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestSetupStatus 测试初始化状态检查
func TestSetupStatus(t *testing.T) {
	router := gin.New()
	handler := setup.NewSetupHandler()
	router.GET("/api/v1/setup/status", handler.IsInitialized)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
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

// TestSetupValidateDirectory 测试目录验证
func TestSetupValidateDirectory(t *testing.T) {
	router := gin.New()
	handler := setup.NewSetupHandler()
	router.POST("/api/v1/setup/validate-dir", handler.ValidateDirectory)

	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"空路径", `{"path":""}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/validate-dir", bytes.NewBufferString(tt.path))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("期望状态码 %d，实际 %d", tt.wantCode, w.Code)
			}
		})
	}
}

// TestSetupSaveConfig 测试保存配置
func TestSetupSaveConfig(t *testing.T) {
	router := gin.New()
	handler := setup.NewSetupHandler()
	router.POST("/api/v1/setup/save", handler.SaveConfig)

	// 测试空目录列表
	t.Run("空目录列表应返回错误", func(t *testing.T) {
		body := `{"appName":"测试","host":"0.0.0.0","port":8080,"rootDirs":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/save", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("期望状态码 400，实际 %d", w.Code)
		}
	})

	// 测试无效 JSON
	t.Run("无效 JSON 应返回错误", func(t *testing.T) {
		body := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/save", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("期望状态码 400，实际 %d", w.Code)
		}
	})
}
