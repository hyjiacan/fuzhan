package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fuzhan/internal/admin"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/notification"
	"fuzhan/internal/repositories"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func getE2EDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.URLDownloadTask{}); err != nil {
		t.Fatalf("迁移数据库失败: %v", err)
	}
	return db
}

// TestURLDownloadE2E_FullFlow 端到端测试：通知 + 管理员管理
func TestURLDownloadE2E_FullFlow(t *testing.T) {
	db := getE2EDB(t)
	repo := repositories.NewURLDownloadTaskRepository(db)

	// 创建各状态的任务（模拟 worker 创建的记录）
	now := time.Now()
	tasks := []models.URLDownloadTask{
		{
			ID:              "e2e-completed-1",
			URL:             "https://example.com/doc.pdf",
			FileName:        "doc.pdf",
			Status:          models.URLDownloadStatusCompleted,
			FileSize:        1024000,
			DownloadedBytes: 1024000,
			StorageType:     models.URLDownloadStorageTemp,
			Notified:        false,
			CreatedAt:       now.Add(-10 * time.Minute),
			CompletedAt:     &now,
		},
		{
			ID:              "e2e-failed-1",
			URL:             "https://example.com/bigfile.iso",
			FileName:        "bigfile.iso",
			Status:          models.URLDownloadStatusFailed,
			FileSize:        1073741824,
			DownloadedBytes: 524288000,
			StorageType:     models.URLDownloadStorageRegular,
			ErrorMessage:    "连接超时: 下载速度过慢",
			Notified:        false,
			CreatedAt:       now.Add(-30 * time.Minute),
		},
		{
			ID:          "e2e-pending-1",
			URL:         "https://example.com/patch.zip",
			FileName:    "patch.zip",
			Status:      models.URLDownloadStatusPending,
			FileSize:    51200,
			StorageType: models.URLDownloadStoragePrivate,
			Notified:    false,
			CreatedAt:   now,
		},
		{
			ID:              "e2e-downloading-1",
			URL:             "https://example.com/movie.mp4",
			FileName:        "movie.mp4",
			Status:          models.URLDownloadStatusDownloading,
			FileSize:        2147483648,
			DownloadedBytes: 1073741824,
			StorageType:     models.URLDownloadStorageTemp,
			Notified:        false,
			CreatedAt:       now,
		},
	}
	for _, tsk := range tasks {
		if err := repo.Create(&tsk); err != nil {
			t.Fatalf("创建测试任务失败: %v", err)
		}
	}

	t.Run("通知流程", func(t *testing.T) {
		notificationHandler := notification.NewHandler(db)

		router := gin.New()
		router.Use(middleware.NewAuthMiddleware(nil).AuthOptional())
		router.GET("/api/v1/notifications", notificationHandler.GetNotifications)
		router.POST("/api/v1/notifications/read", notificationHandler.MarkRead)

		// 1. 匿名用户获取通知（带 X-Anonymous-ID）
		t.Run("匿名用户获取未通知任务", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
			req.Header.Set("X-Anonymous-ID", "anonymous-test-id")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("期望 200，实际 %d", w.Code)
			}

			var resp struct {
				Success bool                     `json:"success"`
				Data    []models.URLDownloadTask `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("JSON 解析失败: %v", err)
			}
			if !resp.Success {
				t.Fatal("期望 success 为 true")
			}
			// 匿名用户查询未绑定用户且 anonymous_id 匹配的任务
			// 当前任务没有 anonymous_id，所以应返回空
			if len(resp.Data) != 0 {
				t.Logf("匿名用户返回 %d 条通知（预期可能为空）", len(resp.Data))
			}
		})

		// 2. 为部分任务设置匿名ID并标记未通知
		_ = db.Model(&models.URLDownloadTask{}).Where("id IN ?", []string{"e2e-completed-1", "e2e-failed-1"}).
			Update("anonymous_id", "anonymous-test-id").Error

		t.Run("匿名用户获取通知（绑定后）", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
			req.Header.Set("X-Anonymous-ID", "anonymous-test-id")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool                     `json:"success"`
				Data    []models.URLDownloadTask `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("JSON 解析失败: %v", err)
			}
			if !resp.Success {
				t.Fatal("期望 success 为 true")
			}
			if len(resp.Data) != 2 {
				t.Errorf("期望 2 条通知，实际 %d", len(resp.Data))
			}
		})

		// 3. 标记已读
		t.Run("标记通知为已读", func(t *testing.T) {
			body := `{"ids":["e2e-completed-1","e2e-failed-1"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read",
				bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Anonymous-ID", "anonymous-test-id")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("JSON 解析失败: %v", err)
			}
			if !resp.Success {
				t.Fatal("标记已读失败")
			}

			// 验证已标记
			var task models.URLDownloadTask
			db.First(&task, "id = ?", "e2e-completed-1")
			if !task.Notified {
				t.Error("任务应被标记为已通知")
			}
		})
	})

	t.Run("匿名合并流程", func(t *testing.T) {
		notificationHandler := notification.NewHandler(db)
		authMiddleware := middleware.NewAuthMiddleware(nil)

		router := gin.New()
		router.Use(authMiddleware.AuthOptional())
		router.POST("/api/v1/notifications/merge", notificationHandler.MergeAnonymous)

		// 设置 JWT 密钥以便生成测试 token
		// 使用已有工具函数

		t.Run("未认证用户合并应静默成功", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/merge", nil)
			req.Header.Set("X-Anonymous-ID", "anonymous-test-id")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 无认证也应返回 200（静默忽略）
			if w.Code != http.StatusOK {
				t.Errorf("合并未认证请求应返回 200，实际返回 %d", w.Code)
			}
		})
	})

	t.Run("管理员管理流程", func(t *testing.T) {
		adminHandler := admin.NewURLDownloadHandler(db)

		router := gin.New()
		router.GET("/admin/url-tasks", adminHandler.ListURLTasks)
		router.POST("/admin/url-tasks/:id/retry", adminHandler.RetryURLTask)
		router.DELETE("/admin/url-tasks/:id", adminHandler.DeleteURLTask)

		// 1. 列表查询 - 全部
		t.Run("获取所有任务列表", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?page=1&pageSize=10", nil)
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
			if !resp.Success {
				t.Fatal("列表查询失败")
			}
			if resp.Data.Total != 4 {
				t.Errorf("期望 4 个任务，实际 %d", resp.Data.Total)
			}
		})

		// 2. 按状态过滤
		t.Run("按状态过滤", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks?status=failed", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool `json:"success"`
				Data    struct {
					Items []models.URLDownloadTask `json:"items"`
					Total int64                    `json:"total"`
				} `json:"data"`
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Data.Total != 1 {
				t.Errorf("期望 1 个失败任务，实际 %d", resp.Data.Total)
			}
		})

		// 3. 重试失败任务
		t.Run("重试失败任务", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/url-tasks/e2e-failed-1/retry", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool `json:"success"`
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if !resp.Success {
				t.Fatal("重试失败")
			}

			// 验证状态重置
			var task models.URLDownloadTask
			db.First(&task, "id = ?", "e2e-failed-1")
			if task.Status != models.URLDownloadStatusPending {
				t.Errorf("期望状态 pending，实际 %s", task.Status)
			}
			if task.ErrorMessage != "" {
				t.Error("重试后错误消息应清空")
			}
			if task.DownloadedBytes != 0 {
				t.Error("重试后下载字节应重置为 0")
			}
		})

		// 4. 重试非失败任务（应拒绝）
		t.Run("重试非失败任务应拒绝", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/url-tasks/e2e-pending-1/retry", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("期望 400，实际 %d", w.Code)
			}
		})

		// 5. 删除任务
		t.Run("删除任务", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/admin/url-tasks/e2e-completed-1", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool `json:"success"`
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if !resp.Success {
				t.Fatal("删除失败")
			}

			// 验证已删除
			var count int64
			db.Model(&models.URLDownloadTask{}).Where("id = ?", "e2e-completed-1").Count(&count)
			if count != 0 {
				t.Error("任务应已被删除")
			}
		})

		// 6. 删除不存在任务
		t.Run("删除不存在任务返回 404", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/admin/url-tasks/non-existent", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("期望 404，实际 %d", w.Code)
			}
		})

		// 7. 验证最终状态
		t.Run("最终任务数量验证", func(t *testing.T) {
			var total int64
			db.Model(&models.URLDownloadTask{}).Count(&total)
			if total != 3 {
				t.Errorf("期望 3 个任务（删除了 1 个），实际 %d", total)
			}
		})
	})

	t.Run("空数据场景", func(t *testing.T) {
		emptyDB := getE2EDB(t)
		adminHandler := admin.NewURLDownloadHandler(emptyDB)
		notificationHandler := notification.NewHandler(emptyDB)

		router := gin.New()
		router.Use(middleware.NewAuthMiddleware(nil).AuthOptional())
		router.GET("/admin/url-tasks", adminHandler.ListURLTasks)
		router.GET("/api/v1/notifications", notificationHandler.GetNotifications)

		t.Run("空任务列表", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/url-tasks", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool `json:"success"`
				Data    struct {
					Items []interface{} `json:"items"`
					Total int64         `json:"total"`
				} `json:"data"`
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if !resp.Success {
				t.Fatal("空列表查询失败")
			}
			if resp.Data.Total != 0 {
				t.Errorf("期望 0 个任务，实际 %d", resp.Data.Total)
			}
			if len(resp.Data.Items) != 0 {
				t.Errorf("期望空数组，实际 %d 个", len(resp.Data.Items))
			}
		})

		t.Run("无通知", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
			req.Header.Set("X-Anonymous-ID", "empty-user")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var resp struct {
				Success bool          `json:"success"`
				Data    []interface{} `json:"data"`
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if !resp.Success {
				t.Fatal("通知查询失败")
			}
			if len(resp.Data) != 0 {
				t.Errorf("期望空通知，实际 %d 条", len(resp.Data))
			}
		})
	})
}

// TestURLDownloadE2E_AnonymousMarkRead 测试越权标记
func TestURLDownloadE2E_AnonymousMarkRead(t *testing.T) {
	db := getE2EDB(t)
	repo := repositories.NewURLDownloadTaskRepository(db)

	// 创建属于不同匿名用户的任务
	_ = repo.Create(&models.URLDownloadTask{
		ID:          "mark-user1",
		URL:         "https://example.com/a.txt",
		FileName:    "a.txt",
		Status:      models.URLDownloadStatusCompleted,
		AnonymousID: strPtr("user1"),
	})
	_ = repo.Create(&models.URLDownloadTask{
		ID:          "mark-user2",
		URL:         "https://example.com/b.txt",
		FileName:    "b.txt",
		Status:      models.URLDownloadStatusCompleted,
		AnonymousID: strPtr("user2"),
	})

	notificationHandler := notification.NewHandler(db)
	authMiddleware := middleware.NewAuthMiddleware(nil)

	router := gin.New()
	router.Use(authMiddleware.AuthOptional())
	router.POST("/api/v1/notifications/read", notificationHandler.MarkRead)

	t.Run("用户1不能标记用户2的任务", func(t *testing.T) {
		body := `{"ids":["mark-user2"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Anonymous-ID", "user1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var resp struct {
			Success bool `json:"success"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if !resp.Success {
			t.Fatal("标记请求本身应成功（只是没有匹配记录被更新）")
		}

		// 验证 user2 的任务仍未被标记
		var task models.URLDownloadTask
		db.First(&task, "id = ?", "mark-user2")
		if task.Notified {
			t.Error("用户2的任务不应被用户1标记为已通知")
		}
	})
}

func strPtr(s string) *string {
	return &s
}
