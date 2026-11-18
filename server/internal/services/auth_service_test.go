package services

import (
	"errors"
	"testing"

	"fuzhan/internal/models"
	"fuzhan/pkg/jwt"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// fakeLDAPAuthenticator 记录调用次数，用于断言本地账号登录不会访问目录。
type fakeLDAPAuthenticator struct {
	enabled    bool
	autoCreate bool
	calls      int
	user       *models.User
	err        error
}

func (f *fakeLDAPAuthenticator) IsEnabled() bool        { return f.enabled }
func (f *fakeLDAPAuthenticator) AllowsAutoCreate() bool { return f.autoCreate }

func (f *fakeLDAPAuthenticator) Authenticate(username, password string) (*models.User, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

func setupAuthTestDB(t *testing.T) *gorm.DB {
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

func createTestUser(t *testing.T, db *gorm.DB, username, password, authSource string) models.User {
	t.Helper()
	hash := ""
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("生成密码哈希失败: %v", err)
		}
		hash = string(h)
	}
	user := models.User{
		UUID:         "uuid-" + username,
		Username:     username,
		PasswordHash: hash,
		Role:         "user",
		AuthSource:   authSource,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return user
}

// TestLoginByPassword_LocalUserSkipsLDAP 本地账号只校验本地密码，绝不调用 LDAP。
func TestLoginByPassword_LocalUserSkipsLDAP(t *testing.T) {
	db := setupAuthTestDB(t)
	createTestUser(t, db, "local_user", "correct-password", models.AuthSourceLocal)

	ldap := &fakeLDAPAuthenticator{enabled: true, autoCreate: true}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	user, method, err := svc.loginByPassword("local_user", "correct-password")
	if err != nil {
		t.Fatalf("本地账号登录失败: %v", err)
	}
	if user.Username != "local_user" || method != LoginMethodLocal {
		t.Errorf("登录结果不正确: user=%s method=%s", user.Username, method)
	}
	if ldap.calls != 0 {
		t.Errorf("本地账号登录不应调用 LDAP，实际调用 %d 次", ldap.calls)
	}

	if _, _, err := svc.loginByPassword("local_user", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("本地密码错误应返回通用凭据错误，实际 %v", err)
	}
	if ldap.calls != 0 {
		t.Errorf("本地密码校验失败也不应调用 LDAP，实际调用 %d 次", ldap.calls)
	}
}

// TestLoginByPassword_CaseInsensitiveUsername 用户名大小写变体应路由到同一账号。
func TestLoginByPassword_CaseInsensitiveUsername(t *testing.T) {
	db := setupAuthTestDB(t)
	createTestUser(t, db, "ZhangSan", "correct-password", models.AuthSourceLocal)

	svc := NewAuthService(db)
	user, method, err := svc.loginByPassword("zhangsan", "correct-password")
	if err != nil {
		t.Fatalf("大小写不敏感登录失败: %v", err)
	}
	if user.Username != "ZhangSan" || method != LoginMethodLocal {
		t.Errorf("登录结果不正确: user=%s method=%s", user.Username, method)
	}
}

// TestLoginByPassword_LDAPUserGoesLDAP LDAP 账号只走目录认证，成功后记录真实来源。
func TestLoginByPassword_LDAPUserGoesLDAP(t *testing.T) {
	db := setupAuthTestDB(t)
	existing := createTestUser(t, db, "ldap_user", "", models.AuthSourceLDAP)

	ldap := &fakeLDAPAuthenticator{enabled: true, user: &existing}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	user, method, err := svc.loginByPassword("ldap_user", "dir-password")
	if err != nil {
		t.Fatalf("LDAP 账号登录失败: %v", err)
	}
	if method != LoginMethodLDAP || user.Username != "ldap_user" {
		t.Errorf("LDAP 登录来源不正确: method=%s user=%s", method, user.Username)
	}
	if ldap.calls != 1 {
		t.Errorf("LDAP 账号应调用目录认证 1 次，实际 %d", ldap.calls)
	}
}

// TestLoginByPassword_LDAPDisabledFailsClosed 认证源停用时 LDAP 账号直接失败，不回退本地密码。
func TestLoginByPassword_LDAPDisabledFailsClosed(t *testing.T) {
	db := setupAuthTestDB(t)
	createTestUser(t, db, "ldap_user", "local-password", models.AuthSourceLDAP)

	ldap := &fakeLDAPAuthenticator{enabled: false}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	if _, _, err := svc.loginByPassword("ldap_user", "local-password"); !errors.Is(err, ErrAccountUnavailable) {
		t.Errorf("认证源停用应返回账号不可用，实际 %v", err)
	}
	if ldap.calls != 0 {
		t.Errorf("认证源停用时不应发起目录认证，实际 %d 次", ldap.calls)
	}
}

// TestLoginByPassword_OAuth2RejectsPassword 外部 OAuth2 账号不允许密码登录。
func TestLoginByPassword_OAuth2RejectsPassword(t *testing.T) {
	db := setupAuthTestDB(t)
	createTestUser(t, db, "oauth_user", "local-password", models.AuthSourceOAuth2)

	ldap := &fakeLDAPAuthenticator{enabled: true, autoCreate: true}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	if _, _, err := svc.loginByPassword("oauth_user", "local-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("OAuth2 账号密码登录应返回通用凭据错误，实际 %v", err)
	}
	if ldap.calls != 0 {
		t.Errorf("OAuth2 账号不应调用 LDAP，实际 %d 次", ldap.calls)
	}
}

// TestLoginByPassword_UnknownUserJIT 本地不存在时仅在 LDAP 启用且允许建号时走目录认证。
func TestLoginByPassword_UnknownUserJIT(t *testing.T) {
	db := setupAuthTestDB(t)

	jitUser := models.User{UUID: "uuid-jit", Username: "jit_user", Role: "user", AuthSource: models.AuthSourceLDAP}
	ldap := &fakeLDAPAuthenticator{enabled: true, autoCreate: true, user: &jitUser}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	user, method, err := svc.loginByPassword("jit_user", "dir-password")
	if err != nil {
		t.Fatalf("JIT 登录失败: %v", err)
	}
	if method != LoginMethodLDAP || user.Username != "jit_user" {
		t.Errorf("JIT 登录来源不正确: method=%s user=%s", method, user.Username)
	}
}

// TestLoginByPassword_UnknownUserNoAutoCreate 未允许自动建号时目录认证不发起，返回通用失败。
func TestLoginByPassword_UnknownUserNoAutoCreate(t *testing.T) {
	db := setupAuthTestDB(t)

	ldap := &fakeLDAPAuthenticator{enabled: true, autoCreate: false}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	if _, _, err := svc.loginByPassword("nobody", "any-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("未知账号应返回通用凭据错误，实际 %v", err)
	}
	if ldap.calls != 0 {
		t.Errorf("未允许自动建号时不应调用 LDAP，实际 %d 次", ldap.calls)
	}
}

// TestLoginByPassword_LDAPUnavailableMapped LDAP 不可达时对外统一为账号不可用。
func TestLoginByPassword_LDAPUnavailableMapped(t *testing.T) {
	db := setupAuthTestDB(t)
	createTestUser(t, db, "ldap_user", "", models.AuthSourceLDAP)

	ldap := &fakeLDAPAuthenticator{enabled: true, err: errors.New("连接 LDAP 服务器失败")}
	svc := NewAuthService(db)
	svc.SetLDAPService(ldap)

	if _, _, err := svc.loginByPassword("ldap_user", "dir-password"); !errors.Is(err, ErrAccountUnavailable) {
		t.Errorf("目录不可用应映射为账号不可用，实际 %v", err)
	}
}

// TestLogin_ReturnsMethod Login 返回实际认证来源，供两端写 login_logs.method。
func TestLogin_ReturnsMethod(t *testing.T) {
	if err := jwt.InitJWTSecret(); err != nil {
		t.Fatalf("初始化 JWT 密钥失败: %v", err)
	}
	db := setupAuthTestDB(t)
	createTestUser(t, db, "local_user", "correct-password", models.AuthSourceLocal)

	svc := NewAuthService(db)
	resp, method, err := svc.Login(&LoginRequest{Username: "local_user", Password: "correct-password"})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if resp == nil || resp.Token == "" {
		t.Fatal("登录成功应返回令牌")
	}
	if method != LoginMethodLocal {
		t.Errorf("Login method = %q, want %q", method, LoginMethodLocal)
	}
}

// TestUserFacingLoginError 对外提示只保留明确语义，其余统一为通用凭据错误。
func TestUserFacingLoginError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{ErrAccountDisabled, ErrAccountDisabled.Error()},
		{ErrAccountUnavailable, ErrAccountUnavailable.Error()},
		{ErrInvalidCredentials, ErrInvalidCredentials.Error()},
		{errors.New("LDAP 服务账号绑定失败"), ErrInvalidCredentials.Error()},
	}
	for _, tc := range cases {
		if got := UserFacingLoginError(tc.err); got != tc.want {
			t.Errorf("UserFacingLoginError(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
