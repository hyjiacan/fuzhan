package services

import (
	"errors"
	"fmt"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func setupUserListTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuthRecord{}, &models.FileRecordPrivate{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

// setUserListTestConfig 设置认证配置；测试结束后恢复原值
func setUserListTestConfig(t *testing.T, ldapEnabled, oauth2Enabled bool) {
	t.Helper()
	prev := appconfig.GlobalConfig
	t.Cleanup(func() { appconfig.GlobalConfig = prev })
	appconfig.GlobalConfig = appconfig.Config{
		Auth: appconfig.AuthConfig{
			LDAP: appconfig.LDAPClientConfig{
				Enabled: ldapEnabled,
				Host:    "ldap.example.com",
				BaseDN:  "dc=example,dc=com",
			},
			OAuth2: appconfig.OAuth2Config{
				Enabled:      oauth2Enabled,
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				AuthURL:      "https://idp.example.com/authorize",
			},
		},
	}
}

type identityFixture struct {
	user   *models.User
	source string
}

// seedIdentityUsers 构造覆盖各身份状态的用户与绑定
func seedIdentityUsers(t *testing.T, db *gorm.DB) map[string]identityFixture {
	t.Helper()

	newUser := func(key, authSource, passwordHash, createdVia string) *models.User {
		u := &models.User{
			UUID:         "uuid-" + key,
			Username:     key,
			PasswordHash: passwordHash,
			Role:         "user",
			AuthSource:   authSource,
			CreatedVia:   createdVia,
		}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("创建用户 %s 失败: %v", key, err)
		}
		return u
	}
	bind := func(u *models.User, authType, provider, externalID string) {
		r := &models.AuthRecord{UserID: u.ID, AuthType: authType, ProviderName: provider, ExternalID: externalID}
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("创建绑定失败: %v", err)
		}
	}

	fixtures := map[string]identityFixture{}

	localOK := newUser("local-ok", models.AuthSourceLocal, "hash", models.CreatedViaSelfRegister)
	fixtures["local-ok"] = identityFixture{localOK, models.AuthSourceLocal}

	localBound := newUser("local-bound", models.AuthSourceLocal, "hash", models.CreatedViaAdminCreate)
	bind(localBound, models.AuthSourceLDAP, models.AuthSourceLDAP, "uid=local-bound,dc=x")
	fixtures["local-bound"] = identityFixture{localBound, models.AuthSourceLocal}

	ldapOK := newUser("ldap-ok", models.AuthSourceLDAP, "", models.CreatedViaLDAPJIT)
	bind(ldapOK, models.AuthSourceLDAP, models.AuthSourceLDAP, "uid=ldap-ok,dc=x")
	fixtures["ldap-ok"] = identityFixture{ldapOK, models.AuthSourceLDAP}

	ldapNoBind := newUser("ldap-nobind", models.AuthSourceLDAP, "", models.CreatedViaLegacy)
	fixtures["ldap-nobind"] = identityFixture{ldapNoBind, models.AuthSourceLDAP}

	ldapMismatch := newUser("ldap-mismatch", models.AuthSourceLDAP, "", models.CreatedViaLegacy)
	bind(ldapMismatch, models.AuthSourceLDAP, "ldap:old-provider", "uid=ldap-mismatch,dc=x")
	fixtures["ldap-mismatch"] = identityFixture{ldapMismatch, models.AuthSourceLDAP}

	// oauth2 provider key 当前回退为 "oauth2:"
	oauth2OK := newUser("oauth2-ok", models.AuthSourceOAuth2, "", models.CreatedViaOAuth2JIT)
	bind(oauth2OK, models.AuthSourceOAuth2, "oauth2:", "subject-ok")
	fixtures["oauth2-ok"] = identityFixture{oauth2OK, models.AuthSourceOAuth2}

	oauth2Pwd := newUser("oauth2-pwd", models.AuthSourceOAuth2, "hash", models.CreatedViaLegacy)
	bind(oauth2Pwd, models.AuthSourceOAuth2, "oauth2:", "subject-pwd")
	fixtures["oauth2-pwd"] = identityFixture{oauth2Pwd, models.AuthSourceOAuth2}

	oauth2NoBind := newUser("oauth2-nobind", models.AuthSourceOAuth2, "", models.CreatedViaLegacy)
	fixtures["oauth2-nobind"] = identityFixture{oauth2NoBind, models.AuthSourceOAuth2}

	return fixtures
}

func listUsersByState(t *testing.T, svc *AdminService, filter UserListFilter) map[string]UserItem {
	t.Helper()
	result, err := svc.ListUsers(1, 100, filter)
	if err != nil {
		t.Fatalf("获取用户列表失败: %v", err)
	}
	out := make(map[string]UserItem, len(result.Users))
	for _, item := range result.Users {
		out[item.Username] = item
	}
	return out
}

// identity_state 派生优先级：inconsistent > provider_mismatch > orphaned > ok
func TestAdminService_ListUsers_IdentityStateDerivation(t *testing.T) {
	db := setupUserListTestDB(t)
	seedIdentityUsers(t, db)
	svc := NewAdminService(db)

	setUserListTestConfig(t, true, true)
	all := listUsersByState(t, svc, UserListFilter{})

	expect := map[string]string{
		"local-ok":      models.IdentityStateOK,
		"local-bound":   models.IdentityStateInconsistent,
		"ldap-ok":       models.IdentityStateOK,
		"ldap-nobind":   models.IdentityStateInconsistent,
		"ldap-mismatch": models.IdentityStateProviderMismatch,
		"oauth2-ok":     models.IdentityStateOK,
		"oauth2-pwd":    models.IdentityStateInconsistent,
		"oauth2-nobind": models.IdentityStateInconsistent,
	}
	for username, want := range expect {
		item, ok := all[username]
		if !ok {
			t.Fatalf("用户列表缺少 %s", username)
		}
		if item.IdentityState != want {
			t.Errorf("用户 %s 的 identity_state 期望 %s，实际 %s", username, want, item.IdentityState)
		}
	}

	// 本地账号不返回绑定信息
	if all["local-ok"].ProviderName != "" || all["local-ok"].ExternalID != "" {
		t.Errorf("本地账号不应有 provider_name/external_id")
	}
	if all["ldap-ok"].ProviderName != models.AuthSourceLDAP || all["ldap-ok"].ExternalID != "uid=ldap-ok,dc=x" {
		t.Errorf("LDAP 账号绑定信息不正确: %+v", all["ldap-ok"])
	}

	// 停用 LDAP 后：正常 LDAP 用户 → orphaned；provider 不匹配仍优先于 orphaned
	setUserListTestConfig(t, false, true)
	disabled := listUsersByState(t, svc, UserListFilter{})
	if disabled["ldap-ok"].IdentityState != models.IdentityStateOrphaned {
		t.Errorf("停用 LDAP 后 ldap-ok 期望 orphaned，实际 %s", disabled["ldap-ok"].IdentityState)
	}
	if disabled["ldap-mismatch"].IdentityState != models.IdentityStateProviderMismatch {
		t.Errorf("provider_mismatch 应优先于 orphaned，实际 %s", disabled["ldap-mismatch"].IdentityState)
	}
	if disabled["ldap-nobind"].IdentityState != models.IdentityStateInconsistent {
		t.Errorf("inconsistent 应优先于 orphaned，实际 %s", disabled["ldap-nobind"].IdentityState)
	}
}

// 列表过滤：source / created_via / identity_state 在 SQL 层生效
func TestAdminService_ListUsers_Filters(t *testing.T) {
	db := setupUserListTestDB(t)
	seedIdentityUsers(t, db)
	svc := NewAdminService(db)

	setUserListTestConfig(t, true, true)

	ldapOnly := listUsersByState(t, svc, UserListFilter{Source: models.AuthSourceLDAP})
	if len(ldapOnly) != 3 {
		t.Fatalf("source=ldap 期望 3 个用户，实际 %d", len(ldapOnly))
	}
	for username, item := range ldapOnly {
		if item.AuthSource != models.AuthSourceLDAP {
			t.Errorf("source=ldap 过滤结果包含非 LDAP 用户 %s", username)
		}
	}

	adminCreated := listUsersByState(t, svc, UserListFilter{CreatedVia: models.CreatedViaAdminCreate})
	if len(adminCreated) != 1 || adminCreated["local-bound"].Username == "" {
		t.Fatalf("created_via=admin_create 过滤结果不正确: %+v", adminCreated)
	}

	inconsistent := listUsersByState(t, svc, UserListFilter{IdentityState: models.IdentityStateInconsistent})
	wantInconsistent := []string{"local-bound", "ldap-nobind", "oauth2-pwd", "oauth2-nobind"}
	if len(inconsistent) != len(wantInconsistent) {
		t.Fatalf("identity_state=inconsistent 期望 %d 个用户，实际 %d", len(wantInconsistent), len(inconsistent))
	}
	for _, username := range wantInconsistent {
		if _, ok := inconsistent[username]; !ok {
			t.Errorf("identity_state=inconsistent 缺少 %s", username)
		}
	}

	okUsers := listUsersByState(t, svc, UserListFilter{IdentityState: models.IdentityStateOK})
	for _, username := range []string{"local-ok", "ldap-ok", "oauth2-ok"} {
		if _, ok := okUsers[username]; !ok {
			t.Errorf("identity_state=ok 缺少 %s", username)
		}
	}
	if len(okUsers) != 3 {
		t.Errorf("identity_state=ok 期望 3 个用户，实际 %d", len(okUsers))
	}

	// 停用 LDAP：orphaned 仅命中配置完整但认证源停用的 LDAP 用户
	setUserListTestConfig(t, false, true)
	orphaned := listUsersByState(t, svc, UserListFilter{IdentityState: models.IdentityStateOrphaned})
	if len(orphaned) != 1 {
		t.Fatalf("identity_state=orphaned 期望 1 个用户，实际 %d", len(orphaned))
	}
	if _, ok := orphaned["ldap-ok"]; !ok {
		t.Errorf("identity_state=orphaned 应命中 ldap-ok")
	}

	// 组合过滤：source + identity_state
	combined := listUsersByState(t, svc, UserListFilter{Source: models.AuthSourceLDAP, IdentityState: models.IdentityStateOrphaned})
	if len(combined) != 1 {
		t.Fatalf("组合过滤期望 1 个用户，实际 %d", len(combined))
	}

	// query 分页行为不回归：按用户名模糊匹配仍可用
	searched := listUsersByState(t, svc, UserListFilter{Query: "oauth2"})
	if len(searched) != 3 {
		t.Errorf("query=oauth2 期望 3 个用户，实际 %d", len(searched))
	}
}

func TestAdminService_ListUsers_Pagination(t *testing.T) {
	db := setupUserListTestDB(t)
	for i := 0; i < 5; i++ {
		u := &models.User{
			UUID:       fmt.Sprintf("uuid-page-%d", i),
			Username:   fmt.Sprintf("page-%d", i),
			Role:       "user",
			AuthSource: models.AuthSourceLocal,
			CreatedVia: models.CreatedViaLegacy,
		}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("创建用户失败: %v", err)
		}
	}
	svc := NewAdminService(db)
	setUserListTestConfig(t, false, false)

	result, err := svc.ListUsers(2, 2, UserListFilter{})
	if err != nil {
		t.Fatalf("获取用户列表失败: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("total 期望 5，实际 %d", result.Total)
	}
	if len(result.Users) != 2 {
		t.Errorf("第 2 页期望 2 条，实际 %d", len(result.Users))
	}
}

// assertIdentityErrorStatus 断言错误是带指定状态码的业务错误
func assertIdentityErrorStatus(t *testing.T, err error, wantStatus int, wantMessage string) {
	t.Helper()
	var identityErr *UserIdentityError
	if !errors.As(err, &identityErr) {
		t.Fatalf("期望 UserIdentityError，实际: %v", err)
	}
	if identityErr.Status != wantStatus {
		t.Fatalf("期望状态码 %d，实际 %d（%s）", wantStatus, identityErr.Status, identityErr.Message)
	}
	if wantMessage != "" && identityErr.Message != wantMessage {
		t.Fatalf("期望提示 %q，实际 %q", wantMessage, identityErr.Message)
	}
}

// newIdentityTestUsers 创建管理员与若干普通用户
func newIdentityTestUsers(t *testing.T, db *gorm.DB, usernames ...string) (*models.User, []*models.User) {
	t.Helper()
	admin := &models.User{
		UUID:         "uuid-admin-ops",
		Username:     "admin-ops",
		PasswordHash: "admin-hash",
		Role:         "admin",
		AuthSource:   models.AuthSourceLocal,
		CreatedVia:   models.CreatedViaAdminCreate,
	}
	if err := db.Create(admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	users := make([]*models.User, 0, len(usernames))
	for _, name := range usernames {
		u := &models.User{
			UUID:         "uuid-" + name,
			Username:     name,
			PasswordHash: "old-hash",
			Role:         "user",
			AuthSource:   models.AuthSourceLocal,
			CreatedVia:   models.CreatedViaSelfRegister,
		}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("创建用户 %s 失败: %v", name, err)
		}
		users = append(users, u)
	}
	return admin, users
}

// 绑定外部身份：写绑定、切换 auth_source、清空本地密码，provider 由服务端按配置决定
func TestAdminService_BindExternalIdentity(t *testing.T) {
	db := setupUserListTestDB(t)
	setUserListTestConfig(t, true, true)
	svc := NewAdminService(db)
	admin, users := newIdentityTestUsers(t, db, "bind-target")
	target := users[0]

	if err := svc.BindExternalIdentity(admin.UUID, target.UUID, models.AuthSourceLDAP, "uid=bind-target,dc=x", "张三"); err != nil {
		t.Fatalf("绑定外部身份失败: %v", err)
	}

	var updated models.User
	if err := db.Where("uuid = ?", target.UUID).First(&updated).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if updated.AuthSource != models.AuthSourceLDAP {
		t.Errorf("auth_source 期望 ldap，实际 %s", updated.AuthSource)
	}
	if updated.PasswordHash != "" {
		t.Errorf("绑定后应清空本地密码，实际 %q", updated.PasswordHash)
	}

	var record models.AuthRecord
	if err := db.Where("user_id = ?", updated.ID).First(&record).Error; err != nil {
		t.Fatalf("查询绑定失败: %v", err)
	}
	if record.AuthType != models.AuthSourceLDAP || record.ProviderName != "ldap" {
		t.Errorf("绑定信息不正确: %+v", record)
	}
	if record.ExternalID != "uid=bind-target,dc=x" || record.ExternalName != "张三" {
		t.Errorf("外部标识/显示名不正确: %+v", record)
	}
}

// 重复绑定：同一外部身份已绑定其他用户 → 409
func TestAdminService_BindExternalIdentity_DuplicateBinding(t *testing.T) {
	db := setupUserListTestDB(t)
	setUserListTestConfig(t, true, false)
	svc := NewAdminService(db)
	admin, users := newIdentityTestUsers(t, db, "dup-a", "dup-b")

	if err := svc.BindExternalIdentity(admin.UUID, users[0].UUID, models.AuthSourceLDAP, "uid=dup,dc=x", ""); err != nil {
		t.Fatalf("首次绑定失败: %v", err)
	}
	err := svc.BindExternalIdentity(admin.UUID, users[1].UUID, models.AuthSourceLDAP, "uid=dup,dc=x", "")
	assertIdentityErrorStatus(t, err, 409, "该外部身份已绑定其他用户")
}

// 目标用户已有其他绑定 → 409，提示先转本地
func TestAdminService_BindExternalIdentity_TargetAlreadyBound(t *testing.T) {
	db := setupUserListTestDB(t)
	setUserListTestConfig(t, true, false)
	svc := NewAdminService(db)
	admin, users := newIdentityTestUsers(t, db, "bound-target")
	target := users[0]

	if err := db.Create(&models.AuthRecord{
		UserID:       target.ID,
		AuthType:     models.AuthSourceLDAP,
		ProviderName: models.AuthSourceLDAP,
		ExternalID:   "uid=old,dc=x",
	}).Error; err != nil {
		t.Fatalf("创建历史绑定失败: %v", err)
	}

	err := svc.BindExternalIdentity(admin.UUID, target.UUID, models.AuthSourceLDAP, "uid=new,dc=x", "")
	assertIdentityErrorStatus(t, err, 409, "该用户已绑定其他认证来源，请先转为本地账号")
}

// 认证源未启用或配置不完整 → 400
func TestAdminService_BindExternalIdentity_SourceUnavailable(t *testing.T) {
	db := setupUserListTestDB(t)
	setUserListTestConfig(t, false, false)
	svc := NewAdminService(db)
	admin, users := newIdentityTestUsers(t, db, "no-source")

	err := svc.BindExternalIdentity(admin.UUID, users[0].UUID, models.AuthSourceLDAP, "uid=x,dc=x", "")
	assertIdentityErrorStatus(t, err, 400, "认证源未启用或配置不完整")

	err = svc.BindExternalIdentity(admin.UUID, users[0].UUID, "local", "uid=x,dc=x", "")
	assertIdentityErrorStatus(t, err, 400, "认证类型无效，可选值：ldap、oauth2")
}

// 转为本地：删除全部绑定、写入新密码、auth_source=local，之后可用本地密码登录
func TestAdminService_ConvertUserToLocal(t *testing.T) {
	db := setupUserListTestDB(t)
	setUserListTestConfig(t, true, false)
	svc := NewAdminService(db)
	admin, users := newIdentityTestUsers(t, db, "convert-me")
	target := users[0]

	if err := svc.BindExternalIdentity(admin.UUID, target.UUID, models.AuthSourceLDAP, "uid=convert,dc=x", ""); err != nil {
		t.Fatalf("绑定外部身份失败: %v", err)
	}

	const newPassword = "newpass123"
	if err := svc.ConvertUserToLocal(admin.UUID, target.UUID, newPassword); err != nil {
		t.Fatalf("转为本地账号失败: %v", err)
	}

	var updated models.User
	if err := db.Where("uuid = ?", target.UUID).First(&updated).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if updated.AuthSource != models.AuthSourceLocal {
		t.Errorf("auth_source 期望 local，实际 %s", updated.AuthSource)
	}
	if updated.PasswordHash == "" {
		t.Error("转为本地后应写入密码")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte(newPassword)); err != nil {
		t.Errorf("密码校验失败: %v", err)
	}

	var bindingCount int64
	if err := db.Model(&models.AuthRecord{}).Where("user_id = ?", updated.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("统计绑定失败: %v", err)
	}
	if bindingCount != 0 {
		t.Errorf("转为本地后应删除全部绑定，实际剩余 %d 条", bindingCount)
	}

	// 本地密码登录链路可正常认证
	if _, err := NewAuthService(db).LoginByPassword(updated.Username, newPassword); err != nil {
		t.Errorf("转为本地后本地密码登录失败: %v", err)
	}

	// 已是本地账号 → 400
	err := svc.ConvertUserToLocal(admin.UUID, target.UUID, newPassword)
	assertIdentityErrorStatus(t, err, 400, "该用户已是本地账号")
}

// 重置密码限制：外部认证账号 → 400；本地账号保持可用
func TestAdminService_ResetPassword_ExternalAccountRejected(t *testing.T) {
	db := setupUserListTestDB(t)
	setUserListTestConfig(t, true, false)
	svc := NewAdminService(db)
	admin, users := newIdentityTestUsers(t, db, "ldap-reset", "local-reset")

	if err := svc.BindExternalIdentity(admin.UUID, users[0].UUID, models.AuthSourceLDAP, "uid=reset,dc=x", ""); err != nil {
		t.Fatalf("绑定外部身份失败: %v", err)
	}

	err := svc.ResetPassword(admin.UUID, users[0].UUID, "newpass123")
	assertIdentityErrorStatus(t, err, 400, "请先将该用户转为本地账号")

	if err := svc.ResetPassword(admin.UUID, users[1].UUID, "newpass123"); err != nil {
		t.Errorf("本地账号重置密码不应失败: %v", err)
	}
}
