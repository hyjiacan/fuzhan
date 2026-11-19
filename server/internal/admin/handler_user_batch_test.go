package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupUserBatchTest 创建批量用户操作测试环境：管理员、普通用户、外部认证用户
func setupUserBatchTest(t *testing.T) (*gin.Engine, *gorm.DB, models.User, []models.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.AuthRecord{},
		&models.TempFile{}, &models.UploadSession{}, &models.UploadedChunk{},
		&models.URLDownloadTask{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{},
		&models.ApiKey{}, &models.OAuthClient{},
	); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}

	adminUser := models.User{
		UUID:         "uuid-batch-admin",
		Username:     "admin-batch",
		PasswordHash: "hash",
		Role:         "admin",
		AuthSource:   models.AuthSourceLocal,
	}
	if err := db.Create(&adminUser).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	targets := []models.User{
		{UUID: "uuid-batch-local", Username: "local-user", PasswordHash: "hash", Role: "user", AuthSource: models.AuthSourceLocal},
		{UUID: "uuid-batch-ldap", Username: "ldap-user", PasswordHash: "", Role: "user", AuthSource: models.AuthSourceLDAP},
		{UUID: "uuid-batch-disabled", Username: "disabled-user", PasswordHash: "hash", Role: "user", AuthSource: models.AuthSourceLocal, Disabled: true},
	}
	if err := db.Create(&targets).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}

	h := NewHandler(services.NewAdminService(db), db, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(constants.ContextKeyUserUUID), adminUser.UUID)
		c.Next()
	})
	r.POST("/api/v1/web/admin/users/batch-delete", h.BatchDeleteUsersHandler)
	r.POST("/api/v1/web/admin/users/batch-disable", h.BatchDisableUsersHandler)
	r.POST("/api/v1/web/admin/users/batch-convert-local", h.BatchConvertUsersToLocalHandler)
	return r, db, adminUser, targets
}

// batchResults 解析批量接口返回的逐条结果
func batchResults(t *testing.T, w *httptest.ResponseRecorder) []services.BatchUserResult {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d，响应体 %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Results []services.BatchUserResult `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v，响应体 %s", err, w.Body.String())
	}
	if !resp.Success {
		t.Fatalf("响应 success=false: %s", w.Body.String())
	}
	return resp.Data.Results
}

// findBatchResult 按 uuid 取逐条结果
func findBatchResult(results []services.BatchUserResult, uuid string) services.BatchUserResult {
	for _, item := range results {
		if item.UUID == uuid {
			return item
		}
	}
	return services.BatchUserResult{}
}

// TestBatchDisableUsersHandler 校验批量禁用：已禁用跳过、管理员不可操作、正常用户被禁用
func TestBatchDisableUsersHandler(t *testing.T) {
	r, db, adminUser, targets := setupUserBatchTest(t)

	w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/batch-disable", map[string]interface{}{
		"uuids": []string{targets[0].UUID, targets[2].UUID, adminUser.UUID, "uuid-not-exist"},
	})
	results := batchResults(t, w)

	if got := findBatchResult(results, targets[0].UUID); got.Status != "ok" {
		t.Errorf("正常用户应禁用成功，实际 %+v", got)
	}
	if got := findBatchResult(results, targets[2].UUID); got.Status != "skipped" {
		t.Errorf("已禁用用户应跳过，实际 %+v", got)
	}
	if got := findBatchResult(results, adminUser.UUID); got.Status != "skipped" {
		t.Errorf("管理员账号应跳过，实际 %+v", got)
	}
	if got := findBatchResult(results, "uuid-not-exist"); got.Status != "skipped" {
		t.Errorf("不存在用户应跳过，实际 %+v", got)
	}

	var disabled models.User
	if err := db.Where("uuid = ?", targets[0].UUID).First(&disabled).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if !disabled.Disabled {
		t.Error("正常用户禁用状态未写入数据库")
	}
}

// TestBatchConvertUsersToLocalHandler 校验批量转本地：外部账号被转换、本地账号跳过、密码强度校验
func TestBatchConvertUsersToLocalHandler(t *testing.T) {
	r, db, _, targets := setupUserBatchTest(t)

	w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/batch-convert-local", map[string]interface{}{
		"uuids":        []string{targets[1].UUID, targets[0].UUID},
		"new_password": "new-password-123",
	})
	results := batchResults(t, w)

	if got := findBatchResult(results, targets[1].UUID); got.Status != "ok" {
		t.Errorf("外部账号应转换成功，实际 %+v", got)
	}
	if got := findBatchResult(results, targets[0].UUID); got.Status != "skipped" {
		t.Errorf("本地账号应跳过，实际 %+v", got)
	}

	var converted models.User
	if err := db.Where("uuid = ?", targets[1].UUID).First(&converted).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if converted.AuthSource != models.AuthSourceLocal {
		t.Errorf("auth_source 应为 local，实际 %s", converted.AuthSource)
	}
	if converted.PasswordHash == "" {
		t.Error("转为本地账号后应写入密码哈希")
	}

	// 密码过短应整体拒绝
	w = doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/batch-convert-local", map[string]interface{}{
		"uuids":        []string{targets[0].UUID},
		"new_password": "short",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("密码过短应返回 400，实际 %d", w.Code)
	}
}

// TestBatchDeleteUsersHandler 校验批量删除：管理员跳过、普通用户被删除、空 uuids 返回 400
func TestBatchDeleteUsersHandler(t *testing.T) {
	r, db, adminUser, targets := setupUserBatchTest(t)

	w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/batch-delete", map[string]interface{}{
		"uuids": []string{targets[0].UUID, adminUser.UUID},
	})
	results := batchResults(t, w)

	if got := findBatchResult(results, targets[0].UUID); got.Status != "ok" {
		t.Errorf("普通用户应删除成功，实际 %+v", got)
	}
	if got := findBatchResult(results, adminUser.UUID); got.Status != "skipped" {
		t.Errorf("管理员账号应跳过，实际 %+v", got)
	}

	var count int64
	db.Model(&models.User{}).Where("uuid = ?", targets[0].UUID).Count(&count)
	if count != 0 {
		t.Error("普通用户未被删除")
	}

	// 空 uuids 应返回 400
	w = doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/batch-delete", map[string]interface{}{
		"uuids": []string{},
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("空 uuids 应返回 400，实际 %d", w.Code)
	}
}
