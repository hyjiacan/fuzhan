package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/models"
	"fuzhan/internal/monitor"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// getMonitorTestDB 创建测试数据库（复用 recent_handler_test 的逻辑）
func getMonitorTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.OperationRecord{}); err != nil {
		t.Fatalf("迁移数据库失败: %v", err)
	}
	return db
}

// TestMonitorHandler_Storage 测试存储统计
func TestMonitorHandler_Storage(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/storage", handler.Storage)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/storage", nil)
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

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	if data["totalSpace"] == nil {
		t.Error("totalSpace 字段应存在")
	}
	if data["usedSpace"] == nil {
		t.Error("usedSpace 字段应存在")
	}
	if data["roots"] == nil {
		t.Error("roots 字段应存在")
	}
}

// TestMonitorHandler_Access 测试访问统计
func TestMonitorHandler_Access(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/access", handler.Access)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/access", nil)
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

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	if data["totalRequests"] == nil {
		t.Error("totalRequests 字段应存在")
	}
	if data["activeUsers"] == nil {
		t.Error("activeUsers 字段应存在")
	}
}

// TestMonitorHandler_Access_WithRecords 测试有记录时的访问统计
func TestMonitorHandler_Access_WithRecords(t *testing.T) {
	db := getMonitorTestDB(t)

	// 添加测试记录
	records := []models.OperationRecord{
		{Action: "upload", FileName: "test1.txt", UserID: "user1"},
		{Action: "download", FileName: "test2.txt", UserID: "user1"},
		{Action: "upload", FileName: "test3.txt", UserID: "user2"},
	}
	for i := range records {
		db.Create(&records[i])
	}

	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/access", handler.Access)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/access", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	// 验证统计数据
	totalRequests, ok := data["totalRequests"].(float64)
	if !ok {
		t.Error("totalRequests 应为数字")
	}
	if totalRequests != 3 {
		t.Errorf("期望 totalRequests 为 3，实际 %v", totalRequests)
	}
}

// TestMonitorHandler_Keywords 测试关键词统计
func TestMonitorHandler_Keywords(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/keywords", handler.Keywords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/keywords", nil)
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

// TestMonitorHandler_Keywords_WithSearchRecords 测试有搜索记录时的关键词统计
func TestMonitorHandler_Keywords_WithSearchRecords(t *testing.T) {
	db := getMonitorTestDB(t)

	// 添加搜索记录
	records := []models.OperationRecord{
		{Action: "search", SearchQuery: "report", FileName: ""},
		{Action: "search", SearchQuery: "report", FileName: ""},
		{Action: "search", SearchQuery: "invoice", FileName: ""},
	}
	for i := range records {
		db.Create(&records[i])
	}

	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/keywords", handler.Keywords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/keywords", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("data 应为数组")
	}

	if len(data) != 2 {
		t.Errorf("期望 2 个关键词，实际 %d", len(data))
	}

	// 验证排序（report 出现 2 次应在首位）
	if len(data) > 0 {
		first, ok := data[0].(map[string]interface{})
		if ok && first["word"] != "report" {
			t.Errorf("期望第一个关键词为 report，实际 %v", first["word"])
		}
	}
}

// TestMonitorHandler_RecentKeywords 测试最近关键词
func TestMonitorHandler_RecentKeywords(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/recent", handler.RecentKeywords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/recent", nil)
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

// TestMonitorHandler_Rankings 测试排行榜
func TestMonitorHandler_Rankings(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/rankings", handler.Rankings)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/rankings", nil)
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

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	if data["recentUploads"] == nil {
		t.Error("recentUploads 字段应存在")
	}
	if data["recentDownloads"] == nil {
		t.Error("recentDownloads 字段应存在")
	}
}

// TestMonitorHandler_Rankings_WithRecords 测试有记录时的排行榜
func TestMonitorHandler_Rankings_WithRecords(t *testing.T) {
	db := getMonitorTestDB(t)

	// 添加上传和下载记录
	records := []models.OperationRecord{
		{Action: "upload", FileName: "test1.txt"},
		{Action: "download", FileName: "test2.txt"},
		{Action: "upload", FileName: "test3.txt"},
	}
	for i := range records {
		db.Create(&records[i])
	}

	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/rankings", handler.Rankings)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/rankings", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	uploads, ok := data["recentUploads"].([]interface{})
	if !ok {
		t.Fatal("recentUploads 应为数组")
	}
	if len(uploads) != 2 {
		t.Errorf("期望 2 条上传记录，实际 %d", len(uploads))
	}

	downloads, ok := data["recentDownloads"].([]interface{})
	if !ok {
		t.Fatal("recentDownloads 应为数组")
	}
	if len(downloads) != 1 {
		t.Errorf("期望 1 条下载记录，实际 %d", len(downloads))
	}
}

// TestMonitorHandler_AllEndpoints 测试所有端点同时访问
func TestMonitorHandler_AllEndpoints(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/monitor/storage", handler.Storage)
	router.GET("/api/v1/monitor/access", handler.Access)
	router.GET("/api/v1/monitor/keywords", handler.Keywords)
	router.GET("/api/v1/monitor/recent", handler.RecentKeywords)
	router.GET("/api/v1/monitor/rankings", handler.Rankings)

	endpoints := []struct {
		path string
		name string
	}{
		{"/api/v1/monitor/storage", "Storage"},
		{"/api/v1/monitor/access", "Access"},
		{"/api/v1/monitor/keywords", "Keywords"},
		{"/api/v1/monitor/recent", "RecentKeywords"},
		{"/api/v1/monitor/rankings", "Rankings"},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("%s: 期望状态码 200，实际 %d", ep.name, w.Code)
			}

			var response map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &response)

			if response["success"] != true {
				t.Errorf("%s: 期望 success 为 true", ep.name)
			}
		})
	}
}
