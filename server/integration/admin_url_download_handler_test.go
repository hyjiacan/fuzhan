package integration

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/gin-gonic/gin"
    "github.com/glebarez/sqlite"
    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/admin"
    "fuzhan/internal/models"
)

func getAdminURLDownloadTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil {
        t.Fatalf("创建测试数据库失败: %v", err)
    }
    if err := db.AutoMigrate(&models.URLDownloadTask{}); err != nil {
        t.Fatalf("迁移数据库失败: %v", err)
    }
    return db
}

func seedURLDownloadTasks(t *testing.T, db *gorm.DB) {
    tasks := []models.URLDownloadTask{
        {
            ID:       "task-001",
            URL:      "https://example.com/file1.zip",
            FileName: "file1.zip",
            Status:   models.URLDownloadStatusPending,
            FileSize: 1024,
        },
        {
            ID:       "task-002",
            URL:      "https://example.com/file2.zip",
            FileName: "file2.zip",
            Status:   models.URLDownloadStatusCompleted,
            FileSize: 2048,
        },
        {
            ID:       "task-003",
            URL:      "https://example.com/file3.zip",
            FileName: "file3.zip",
            Status:   models.URLDownloadStatusFailed,
            FileSize: 4096,
            ErrorMessage: "连接超时",
        },
        {
            ID:       "task-004",
            URL:      "https://example.com/file4.zip",
            FileName: "file4.zip",
            Status:   models.URLDownloadStatusDownloading,
            FileSize: 8192,
        },
    }
    for _, tsk := range tasks {
        if err := db.Create(&tsk).Error; err != nil {
            t.Fatalf("创建测试数据失败: %v", err)
        }
    }
}

// TestAdminURLDownloadHandler_ListAll 测试获取所有任务列表
func TestAdminURLDownloadHandler_ListAll(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.GET("/admin/url-tasks", handler.ListURLTasks)

    req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?page=1&pageSize=10", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("期望状态码 200，实际 %d", w.Code)
    }

    var resp struct {
        Success bool `json:"success"`
        Data    struct {
            Items    []models.URLDownloadTask `json:"items"`
            Total    int64                    `json:"total"`
            Page     int                      `json:"page"`
            PageSize int                      `json:"pageSize"`
        } `json:"data"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }

    if !resp.Success {
        t.Error("期望 success 为 true")
    }
    if resp.Data.Total != 4 {
        t.Errorf("期望 total 为 4，实际 %d", resp.Data.Total)
    }
    if len(resp.Data.Items) != 4 {
        t.Errorf("期望 items 长度为 4，实际 %d", len(resp.Data.Items))
    }
}

// TestAdminURLDownloadHandler_ListByStatus 测试按状态过滤
func TestAdminURLDownloadHandler_ListByStatus(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.GET("/admin/url-tasks", handler.ListURLTasks)

    req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?status=failed", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("期望状态码 200，实际 %d", w.Code)
    }

    var resp struct {
        Success bool `json:"success"`
        Data    struct {
            Items []models.URLDownloadTask `json:"items"`
            Total int64                    `json:"total"`
        } `json:"data"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }

    if resp.Data.Total != 1 {
        t.Errorf("期望 total 为 1，实际 %d", resp.Data.Total)
    }
    if len(resp.Data.Items) != 1 {
        t.Errorf("期望 1 个任务，实际 %d", len(resp.Data.Items))
    }
    if len(resp.Data.Items) > 0 && resp.Data.Items[0].Status != models.URLDownloadStatusFailed {
        t.Errorf("期望状态为 failed，实际 %s", resp.Data.Items[0].Status)
    }
}

// TestAdminURLDownloadHandler_ListPagination 测试分页
func TestAdminURLDownloadHandler_ListPagination(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.GET("/admin/url-tasks", handler.ListURLTasks)

    // 每页 2 条
    req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?page=1&pageSize=2", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    var resp struct {
        Success bool `json:"success"`
        Data    struct {
            Items    []models.URLDownloadTask `json:"items"`
            Total    int64                    `json:"total"`
            Page     int                      `json:"page"`
            PageSize int                      `json:"pageSize"`
        } `json:"data"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }

    if resp.Data.Page != 1 {
        t.Errorf("期望 page 为 1，实际 %d", resp.Data.Page)
    }
    if resp.Data.PageSize != 2 {
        t.Errorf("期望 pageSize 为 2，实际 %d", resp.Data.PageSize)
    }
    if len(resp.Data.Items) != 2 {
        t.Errorf("期望 2 条记录，实际 %d", len(resp.Data.Items))
    }
    if resp.Data.Total != 4 {
        t.Errorf("期望 total 为 4，实际 %d", resp.Data.Total)
    }
}

// TestAdminURLDownloadHandler_ListDefaultParams 测试默认分页参数
func TestAdminURLDownloadHandler_ListDefaultParams(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.GET("/admin/url-tasks", handler.ListURLTasks)

    req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    var resp struct {
        Success bool `json:"success"`
        Data    struct {
            PageSize int `json:"pageSize"`
        } `json:"data"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }

    if resp.Data.PageSize != 20 {
        t.Errorf("期望默认 pageSize 为 20，实际 %d", resp.Data.PageSize)
    }
}

// TestAdminURLDownloadHandler_RetrySuccess 测试重试失败任务
func TestAdminURLDownloadHandler_RetrySuccess(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.POST("/admin/url-tasks/:id/retry", handler.RetryURLTask)

    req := httptest.NewRequest(http.MethodPost, "/admin/url-tasks/task-003/retry", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("期望状态码 200，实际 %d", w.Code)
    }

    var resp struct {
        Success bool `json:"success"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }
    if !resp.Success {
        t.Error("期望 retry 成功")
    }

    // 验证状态已重置
    var task models.URLDownloadTask
    if err := db.First(&task, "id = ?", "task-003").Error; err != nil {
        t.Fatalf("查询任务失败: %v", err)
    }
    if task.Status != models.URLDownloadStatusPending {
        t.Errorf("期望状态为 pending，实际 %s", task.Status)
    }
}

// TestAdminURLDownloadHandler_RetryNonFailed 测试重试非失败状态的任务
func TestAdminURLDownloadHandler_RetryNonFailed(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.POST("/admin/url-tasks/:id/retry", handler.RetryURLTask)

    // 尝试重试 pending 状态的任务
    req := httptest.NewRequest(http.MethodPost, "/admin/url-tasks/task-001/retry", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("期望状态码 400，实际 %d", w.Code)
    }

    var resp struct {
        Success bool `json:"success"`
        Message string `json:"message"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }
    if resp.Success {
        t.Error("pending 状态任务不应允许重试")
    }
}

// TestAdminURLDownloadHandler_RetryNotFound 测试重试不存在的任务
func TestAdminURLDownloadHandler_RetryNotFound(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.POST("/admin/url-tasks/:id/retry", handler.RetryURLTask)

    req := httptest.NewRequest(http.MethodPost, "/admin/url-tasks/non-existent/retry", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusNotFound {
        t.Errorf("期望状态码 404，实际 %d", w.Code)
    }
}

// TestAdminURLDownloadHandler_DeleteSuccess 测试删除任务
func TestAdminURLDownloadHandler_DeleteSuccess(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.DELETE("/admin/url-tasks/:id", handler.DeleteURLTask)

    req := httptest.NewRequest(http.MethodDelete, "/admin/url-tasks/task-001", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("期望状态码 200，实际 %d", w.Code)
    }

    var resp struct {
        Success bool `json:"success"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }
    if !resp.Success {
        t.Error("期望删除成功")
    }

    // 验证已删除
    var count int64
    db.Model(&models.URLDownloadTask{}).Where("id = ?", "task-001").Count(&count)
    if count != 0 {
        t.Error("任务应该已被删除")
    }
}

// TestAdminURLDownloadHandler_DeleteNotFound 测试删除不存在的任务
func TestAdminURLDownloadHandler_DeleteNotFound(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.DELETE("/admin/url-tasks/:id", handler.DeleteURLTask)

    req := httptest.NewRequest(http.MethodDelete, "/admin/url-tasks/non-existent", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusNotFound {
        t.Errorf("期望状态码 404，实际 %d", w.Code)
    }
}

// TestAdminURLDownloadHandler_DeleteWithTempFile 测试删除含临时文件的任务
func TestAdminURLDownloadHandler_DeleteWithTempFile(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)

    // 创建临时文件
    tmpDir := t.TempDir()
    tmpFile := filepath.Join(tmpDir, "test-download.tmp")
    if err := os.WriteFile(tmpFile, []byte("test content"), 0644); err != nil {
        t.Fatalf("创建临时文件失败: %v", err)
    }

    task := models.URLDownloadTask{
        ID:           "task-with-temp",
        URL:          "https://example.com/file.zip",
        FileName:     "file.zip",
        Status:       models.URLDownloadStatusFailed,
        TempFilePath: tmpFile,
        StorageType:  models.URLDownloadStorageTemp,
        TargetPath:   "temp/file.zip",
    }
    if err := db.Create(&task).Error; err != nil {
        t.Fatalf("创建测试数据失败: %v", err)
    }

    // 设置临时文件目录（为测试目录，不影响）
    appconfig.GlobalConfig.Storage.Temp.Path = tmpDir

    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.DELETE("/admin/url-tasks/:id", handler.DeleteURLTask)

    req := httptest.NewRequest(http.MethodDelete, "/admin/url-tasks/task-with-temp", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("期望状态码 200，实际 %d", w.Code)
    }

    // 验证临时文件已被删除
    if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
        t.Error("临时文件应该已被删除")
    }
}

// TestAdminURLDownloadHandler_RetryCleansTempFile 测试重试时清理临时文件
func TestAdminURLDownloadHandler_RetryCleansTempFile(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)

    tmpDir := t.TempDir()
    tmpFile := filepath.Join(tmpDir, "retry-test.tmp")
    if err := os.WriteFile(tmpFile, []byte("test content"), 0644); err != nil {
        t.Fatalf("创建临时文件失败: %v", err)
    }

    task := models.URLDownloadTask{
        ID:           "task-retry-clean",
        URL:          "https://example.com/file.zip",
        FileName:     "file.zip",
        Status:       models.URLDownloadStatusFailed,
        TempFilePath: tmpFile,
        ErrorMessage: "下载超时",
    }
    if err := db.Create(&task).Error; err != nil {
        t.Fatalf("创建测试数据失败: %v", err)
    }

    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.POST("/admin/url-tasks/:id/retry", handler.RetryURLTask)

    req := httptest.NewRequest(http.MethodPost, "/admin/url-tasks/task-retry-clean/retry", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("期望状态码 200，实际 %d", w.Code)
    }

    // 验证临时文件已被清理
    if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
        t.Error("重试时临时文件应该被清理")
    }

    // 验证错误消息已清除
    var updatedTask models.URLDownloadTask
    if err := db.First(&updatedTask, "id = ?", "task-retry-clean").Error; err != nil {
        t.Fatalf("查询任务失败: %v", err)
    }
    if updatedTask.ErrorMessage != "" {
        t.Error("重试后错误消息应被清除")
    }
    if updatedTask.DownloadedBytes != 0 {
        t.Error("重试后 downloadedBytes 应重置为 0")
    }
}

// TestAdminURLDownloadHandler_InvalidQueryParams 测试无效的分页参数
func TestAdminURLDownloadHandler_InvalidQueryParams(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.GET("/admin/url-tasks", handler.ListURLTasks)

    // pageSize 超过上限
    req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?page=1&pageSize=200", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    var resp struct {
        Data struct {
            PageSize int `json:"pageSize"`
        } `json:"data"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }
    if resp.Data.PageSize > 100 {
        t.Errorf("pageSize 不应超过 100，实际 %d", resp.Data.PageSize)
    }

    // page 小于 1
    req2 := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?page=0", nil)
    w2 := httptest.NewRecorder()
    router.ServeHTTP(w2, req2)

    json.Unmarshal(w2.Body.Bytes(), &resp)
    // 这种情况下 page 应该被纠正为 1
    var pageResp struct {
        Data struct {
            Page int `json:"page"`
        } `json:"data"`
    }
    json.Unmarshal(w2.Body.Bytes(), &pageResp)
    if pageResp.Data.Page < 1 {
        t.Errorf("page 应被修正为至少 1，实际 %d", pageResp.Data.Page)
    }
}

// TestAdminURLDownloadHandler_EmptyStatusFilter 测试空状态过滤
func TestAdminURLDownloadHandler_EmptyStatusFilter(t *testing.T) {
    db := getAdminURLDownloadTestDB(t)
    seedURLDownloadTasks(t, db)
    handler := admin.NewURLDownloadHandler(db)

    router := gin.New()
    router.GET("/admin/url-tasks", handler.ListURLTasks)

    // 用不存在的状态过滤
    req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?status=invalid_status", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    var resp struct {
        Success bool `json:"success"`
        Data    struct {
            Items []models.URLDownloadTask `json:"items"`
            Total int64                    `json:"total"`
        } `json:"data"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("JSON 解析失败: %v", err)
    }

    // 不存在的状态应返回空结果
    if resp.Data.Total != 0 {
        t.Errorf("不存在的状态应返回 0 条结果，实际 %d", resp.Data.Total)
    }
}
