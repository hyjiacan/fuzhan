package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/file"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// getTestDB 创建测试数据库
func getTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	// 自动迁移表结构
	if err := db.AutoMigrate(&models.OperationRecord{}, &models.FileRecordPublic{}); err != nil {
		t.Fatalf("迁移数据库失败: %v", err)
	}
	return db
}

// TestRecentHandler_GetRecent 测试获取最近上传
func TestRecentHandler_GetRecent(t *testing.T) {
	db := getTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := file.NewRecentHandler(recordRepo, db)

	router := gin.New()
	router.GET("/api/v1/files/recent", handler.GetRecent)

	// 测试默认查询
	t.Run("默认查询参数", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/files/recent?action=upload", nil)
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

		// data 现在直接是数组
		if response["data"] == nil {
			t.Error("期望 data 存在")
		}
	})

	// 测试自定义 limit
	t.Run("自定义 limit 参数", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/files/recent?action=upload&limit=10", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("期望状态码 200，实际 %d", w.Code)
		}
	})

	// 测试无效 limit 参数
	t.Run("无效 limit 参数使用默认值", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/files/recent?action=upload&limit=-1", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("期望状态码 200，实际 %d", w.Code)
		}
	})
}

// TestRecentHandler_GetRecentCarousel 测试获取轮播数据
func TestRecentHandler_GetRecentCarousel(t *testing.T) {
	db := getTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := file.NewRecentHandler(recordRepo, db)

	router := gin.New()
	router.GET("/api/v1/files/recent/carousel", handler.GetRecentCarousel)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/recent/carousel", nil)
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

	// data 现在直接是数组
	if response["data"] == nil {
		t.Error("期望 data 存在")
	}
}
