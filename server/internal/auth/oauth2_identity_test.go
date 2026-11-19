package auth

import (
	"errors"
	"testing"

	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupOAuth2TestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuthRecord{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

// TestOAuth2ProviderName 绑定使用稳定 provider key：oauth2:<provider_id>。
func TestOAuth2ProviderName(t *testing.T) {
	s := &OAuth2Service{}
	if got := s.providerName(&OAuth2Config{ProviderID: "keycloak-prod"}); got != "oauth2:keycloak-prod" {
		t.Errorf("providerName = %q", got)
	}
	if got := s.providerName(&OAuth2Config{ProviderName: "企业 SSO"}); got != "oauth2" {
		t.Errorf("缺少 provider_id 时不应使用展示名，实际 %q", got)
	}
}

// TestOAuth2FindOrCreateUser_BindingHit 绑定命中直接返回对应用户。
func TestOAuth2FindOrCreateUser_BindingHit(t *testing.T) {
	db := setupOAuth2TestDB(t)
	user := models.User{UUID: "uuid-oauth", Username: "oauth_user", Role: "user", AuthSource: models.AuthSourceOAuth2}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := db.Create(&models.AuthRecord{
		UserID: user.ID, AuthType: models.AuthSourceOAuth2,
		ProviderName: "oauth2:keycloak-prod", ExternalID: "sub-1",
	}).Error; err != nil {
		t.Fatalf("创建绑定失败: %v", err)
	}

	svc := NewOAuth2Service(db, &OAuth2Config{Enabled: true, ProviderID: "keycloak-prod"})
	got, err := svc.findOrCreateUser(&oauthUserInfo{Subject: "sub-1", Username: "oauth_user"})
	if err != nil {
		t.Fatalf("绑定命中应登录成功: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("应返回绑定用户，实际 id=%d", got.ID)
	}
}

// TestOAuth2FindOrCreateUser_JIT 本地不存在且允许建号时创建 oauth2 用户与绑定。
func TestOAuth2FindOrCreateUser_JIT(t *testing.T) {
	db := setupOAuth2TestDB(t)
	svc := NewOAuth2Service(db, &OAuth2Config{
		Enabled: true, ProviderID: "keycloak-prod", AutoCreateUser: true,
	})

	got, err := svc.findOrCreateUser(&oauthUserInfo{Subject: "sub-2", Username: "new_oauth", DisplayName: "新用户"})
	if err != nil {
		t.Fatalf("JIT 建号失败: %v", err)
	}
	if got.AuthSource != models.AuthSourceOAuth2 || got.CreatedVia != models.CreatedViaOAuth2JIT {
		t.Errorf("JIT 用户来源/渠道不正确: %+v", got)
	}
	if got.PasswordHash != "" {
		t.Error("外部账号不应写本地密码")
	}
	var record models.AuthRecord
	if err := db.Where("user_id = ? AND auth_type = ? AND provider_name = ? AND external_id = ?",
		got.ID, models.AuthSourceOAuth2, "oauth2:keycloak-prod", "sub-2").First(&record).Error; err != nil {
		t.Errorf("JIT 未写入绑定: %v", err)
	}
}

// TestOAuth2FindOrCreateUser_SameNameRejected 同名本地账号一律拒绝，不写绑定、不改字段；
// 返回可区分的冲突错误供登录回调签发确认票据。
func TestOAuth2FindOrCreateUser_SameNameRejected(t *testing.T) {
	db := setupOAuth2TestDB(t)
	local := models.User{UUID: "uuid-local", Username: "same_name", PasswordHash: "hash", Role: "user", AuthSource: models.AuthSourceLocal}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("创建本地用户失败: %v", err)
	}

	svc := NewOAuth2Service(db, &OAuth2Config{
		Enabled: true, ProviderID: "keycloak-prod", AutoCreateUser: true,
	})
	_, err := svc.findOrCreateUser(&oauthUserInfo{Subject: "sub-3", Username: "same_name", DisplayName: "张三"})
	if !errors.Is(err, services.ErrAccountUnavailable) {
		t.Fatalf("同名本地账号应拒绝登录，实际 err=%v", err)
	}
	var conflict *services.IdentityConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("应返回 IdentityConflictError，实际 %T", err)
	}
	if conflict.Username != "same_name" || conflict.LocalUserID != local.ID ||
		conflict.LocalAuthSource != models.AuthSourceLocal || conflict.ProviderID != "keycloak-prod" ||
		conflict.ProviderName != "oauth2:keycloak-prod" || conflict.ExternalID != "sub-3" {
		t.Errorf("冲突错误字段不正确: %+v", conflict)
	}

	var count int64
	db.Model(&models.User{}).Where("username = ?", "same_name").Count(&count)
	if count != 1 {
		t.Errorf("冲突不得创建后缀账号，实际同名/近似账号 %d 个", count)
	}
	db.Model(&models.AuthRecord{}).Where("user_id = ?", local.ID).Count(&count)
	if count != 0 {
		t.Errorf("冲突不得写入绑定，实际 %d 条", count)
	}
}

// TestOAuth2FindOrCreateUser_NoAutoCreate 未允许自动建号且本地不存在时拒绝登录。
func TestOAuth2FindOrCreateUser_NoAutoCreate(t *testing.T) {
	db := setupOAuth2TestDB(t)
	svc := NewOAuth2Service(db, &OAuth2Config{Enabled: true, ProviderID: "keycloak-prod"})

	if _, err := svc.findOrCreateUser(&oauthUserInfo{Subject: "sub-4", Username: "unknown_oauth"}); !errors.Is(err, services.ErrAccountUnavailable) {
		t.Fatalf("未允许自动建号时应拒绝，实际 err=%v", err)
	}
}
