package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupIdentityOpsTest 创建内存数据库与带管理员上下文的路由
func setupIdentityOpsTest(t *testing.T) (*gin.Engine, *gorm.DB, models.User, models.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuthRecord{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}

	prev := appconfig.GlobalConfig
	t.Cleanup(func() { appconfig.GlobalConfig = prev })
	appconfig.GlobalConfig = appconfig.Config{
		Auth: appconfig.AuthConfig{
			LDAP: appconfig.LDAPClientConfig{
				Enabled: true,
				Host:    "ldap.example.com",
				BaseDN:  "dc=example,dc=com",
			},
		},
	}

	adminUser := models.User{
		UUID:         "uuid-admin-handler",
		Username:     "admin-handler",
		PasswordHash: "admin-hash",
		Role:         "admin",
		AuthSource:   models.AuthSourceLocal,
		CreatedVia:   models.CreatedViaAdminCreate,
	}
	if err := db.Create(&adminUser).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	target := models.User{
		UUID:         "uuid-target-handler",
		Username:     "target-handler",
		PasswordHash: "old-hash",
		Role:         "user",
		AuthSource:   models.AuthSourceLocal,
		CreatedVia:   models.CreatedViaSelfRegister,
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	h := NewHandler(services.NewAdminService(db), db, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(constants.ContextKeyUserUUID), adminUser.UUID)
		c.Next()
	})
	r.POST("/api/v1/web/admin/users/:id/auth-bindings", h.BindExternalIdentityHandler)
	r.POST("/api/v1/web/admin/users/:id/convert-local", h.ConvertUserToLocalHandler)
	r.PUT("/api/v1/web/admin/users/:id/reset-password", h.ResetPasswordHandler)
	return r, db, adminUser, target
}

// doJSON 发起 JSON 请求并返回响应记录器
func doJSON(t *testing.T, r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求失败: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func decodeMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v，body=%s", err, w.Body.String())
	}
	return resp.Message
}

// 同一外部身份绑定第二个用户 → 409
func TestHandler_BindExternalIdentity_DuplicateConflict(t *testing.T) {
	r, db, _, target := setupIdentityOpsTest(t)
	other := models.User{
		UUID:       "uuid-other-handler",
		Username:   "other-handler",
		Role:       "user",
		AuthSource: models.AuthSourceLocal,
		CreatedVia: models.CreatedViaSelfRegister,
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("创建第二个用户失败: %v", err)
	}

	body := map[string]string{
		"auth_type":     models.AuthSourceLDAP,
		"external_id":   "uid=dup,dc=example,dc=com",
		"external_name": "张三",
	}
	if w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/"+target.UUID+"/auth-bindings", body); w.Code != http.StatusOK {
		t.Fatalf("首次绑定期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/"+other.UUID+"/auth-bindings", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("跨用户重复绑定期望 409，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if msg := decodeMessage(t, w); msg != "该外部身份已绑定其他用户" {
		t.Errorf("冲突提示不正确: %s", msg)
	}
}

// 目标用户已有绑定 → 409
func TestHandler_BindExternalIdentity_TargetBoundConflict(t *testing.T) {
	r, db, _, target := setupIdentityOpsTest(t)
	if err := db.Create(&models.AuthRecord{
		UserID:       target.ID,
		AuthType:     models.AuthSourceLDAP,
		ProviderName: models.AuthSourceLDAP,
		ExternalID:   "uid=old,dc=example,dc=com",
	}).Error; err != nil {
		t.Fatalf("创建历史绑定失败: %v", err)
	}

	w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/"+target.UUID+"/auth-bindings", map[string]string{
		"auth_type":   models.AuthSourceLDAP,
		"external_id": "uid=new,dc=example,dc=com",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("目标用户已有绑定期望 409，实际 %d，body=%s", w.Code, w.Body.String())
	}
}

// 外部认证账号重置密码 → 400
func TestHandler_ResetPassword_ExternalAccountRejected(t *testing.T) {
	r, db, _, target := setupIdentityOpsTest(t)
	if err := db.Model(&models.User{}).Where("id = ?", target.ID).Updates(map[string]interface{}{
		"auth_source":   models.AuthSourceLDAP,
		"password_hash": "",
	}).Error; err != nil {
		t.Fatalf("更新用户失败: %v", err)
	}

	w := doJSON(t, r, http.MethodPut, "/api/v1/web/admin/users/"+target.UUID+"/reset-password", map[string]string{
		"new_password": "newpass123",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("外部账号重置密码期望 400，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if msg := decodeMessage(t, w); msg != "请先将该用户转为本地账号" {
		t.Errorf("提示文案不正确: %s", msg)
	}
}

// 已是本地账号再转本地 → 400
func TestHandler_ConvertUserToLocal_AlreadyLocal(t *testing.T) {
	r, _, _, target := setupIdentityOpsTest(t)

	w := doJSON(t, r, http.MethodPost, "/api/v1/web/admin/users/"+target.UUID+"/convert-local", map[string]string{
		"new_password": "newpass123",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("已是本地账号期望 400，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if msg := decodeMessage(t, w); msg != "该用户已是本地账号" {
		t.Errorf("提示文案不正确: %s", msg)
	}
}
