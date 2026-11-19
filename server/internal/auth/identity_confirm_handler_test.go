package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const testJWTSecret = "0123456789abcdef0123456789abcdef"

// setupIdentityConfirmTest 构建内存库、认证服务与已初始化 JWT 的 Handler。
func setupIdentityConfirmTest(t *testing.T) (*gorm.DB, *Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	jwt.UpdateJWTSecret([]byte(testJWTSecret))

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuthRecord{}, &models.LoginLog{}, &models.IdentityConflict{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	// 确认接口会按当前配置复核，统一置为自助模式，避免受其它用例影响。
	origMode := appconfig.GlobalConfig.Auth.IdentityConflictMode
	appconfig.GlobalConfig.Auth.IdentityConflictMode = appconfig.IdentityConflictModeSelfService
	t.Cleanup(func() { appconfig.GlobalConfig.Auth.IdentityConflictMode = origMode })
	return db, NewHandler(services.NewAuthService(db), db)
}

// createLocalTestUser 创建带本地密码的普通用户。
func createLocalTestUser(t *testing.T, db *gorm.DB, username, password, role string) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	user := models.User{
		UUID:         "uuid-" + username,
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		AuthSource:   models.AuthSourceLocal,
		CreatedVia:   models.CreatedViaSelfRegister,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return &user
}

// newAuthTestContext 构造带 JSON body、客户端 IP 与 Cookie 的 gin 上下文。
func newAuthTestContext(method, path, ip string, body any, cookies ...*http.Cookie) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":12345"
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	c.Request = req
	return c, w
}

func identityCookie(token string) *http.Cookie {
	return &http.Cookie{Name: middleware.IdentityConfirmCookieName, Value: token}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, ck := range cookies {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

func issueTestTicket(t *testing.T, h *Handler, ticket services.IdentityConfirmTicket) string {
	t.Helper()
	token, err := h.identityConfirm.Issue(ticket)
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	return token
}

// TestIssueIdentityConfirmTicket 自助模式签发票据；admin 模式或管理员账号不签发。
func TestIssueIdentityConfirmTicket(t *testing.T) {
	_, h := setupIdentityConfirmTest(t)
	origMode := appconfig.GlobalConfig.Auth.IdentityConflictMode
	t.Cleanup(func() { appconfig.GlobalConfig.Auth.IdentityConflictMode = origMode })

	conflict := &services.IdentityConflictError{
		Username: "alice", LocalUserID: 1, LocalRole: "user",
		LocalAuthSource: models.AuthSourceLocal,
		AuthType:        models.AuthSourceOAuth2,
		ProviderID:      "keycloak-prod", ProviderName: "oauth2:keycloak-prod",
		ExternalID: "sub-1", ExternalName: "Alice",
	}

	appconfig.GlobalConfig.Auth.IdentityConflictMode = appconfig.IdentityConflictModeSelfService
	c, w := newAuthTestContext(http.MethodGet, "/api/v1/web/auth/identity-confirm", "10.1.0.1", nil)
	if !h.issueIdentityConfirmTicket(c, conflict) {
		t.Fatal("自助模式下应签发票据")
	}
	if loc := w.Header().Get("Location"); loc != "/#/oauth2/callback?conflict=1" {
		t.Errorf("跳转地址 = %q", loc)
	}
	if findCookie(w.Result().Cookies(), middleware.IdentityConfirmCookieName) == nil {
		t.Error("应写入确认票据 Cookie")
	}

	// admin 模式维持拒绝
	appconfig.GlobalConfig.Auth.IdentityConflictMode = appconfig.IdentityConflictModeAdmin
	c2, w2 := newAuthTestContext(http.MethodGet, "/api/v1/web/auth/identity-confirm", "10.1.0.2", nil)
	if h.issueIdentityConfirmTicket(c2, conflict) {
		t.Fatal("admin 模式下不应签发票据")
	}
	if findCookie(w2.Result().Cookies(), middleware.IdentityConfirmCookieName) != nil {
		t.Error("admin 模式下不应写 Cookie")
	}

	// 管理员同名账号不开放自助
	appconfig.GlobalConfig.Auth.IdentityConflictMode = appconfig.IdentityConflictModeSelfService
	adminConflict := *conflict
	adminConflict.LocalRole = "admin"
	c3, w3 := newAuthTestContext(http.MethodGet, "/api/v1/web/auth/identity-confirm", "10.1.0.3", nil)
	if h.issueIdentityConfirmTicket(c3, &adminConflict) {
		t.Fatal("管理员账号不应签发票据")
	}
	if findCookie(w3.Result().Cookies(), middleware.IdentityConfirmCookieName) != nil {
		t.Error("管理员账号不应写确认 Cookie")
	}
}

// TestIdentityConfirmStatus 有票据返回 pending 信息，无票据返回 pending=false。
func TestIdentityConfirmStatus(t *testing.T) {
	_, h := setupIdentityConfirmTest(t)

	c, w := newAuthTestContext(http.MethodGet, "/api/v1/web/auth/identity-confirm", "10.1.1.1", nil)
	h.IdentityConfirmStatus(c)
	var empty struct {
		Data struct {
			Pending bool `json:"pending"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if empty.Data.Pending {
		t.Error("无票据时 pending 应为 false")
	}

	// 展示用 provider_name 取配置中的提供方名称，缺省回退 provider 标识
	origProviderName := appconfig.GlobalConfig.Auth.OAuth2.ProviderName
	appconfig.GlobalConfig.Auth.OAuth2.ProviderName = "企业 SSO"
	t.Cleanup(func() { appconfig.GlobalConfig.Auth.OAuth2.ProviderName = origProviderName })

	token := issueTestTicket(t, h, services.IdentityConfirmTicket{
		UserID: 1, Username: "alice", AuthType: models.AuthSourceOAuth2,
		ProviderID: "keycloak-prod", ProviderName: "oauth2:keycloak-prod", ExternalName: "Alice",
	})
	c2, w2 := newAuthTestContext(http.MethodGet, "/api/v1/web/auth/identity-confirm", "10.1.1.2", nil, identityCookie(token))
	h.IdentityConfirmStatus(c2)
	var data struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &data); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if data.Data["pending"] != true || data.Data["username"] != "alice" ||
		data.Data["provider_id"] != "keycloak-prod" || data.Data["provider_name"] != "企业 SSO" ||
		data.Data["auth_type"] != "oauth2" {
		t.Errorf("确认状态数据不正确: %+v", data.Data)
	}
}

// TestBindIdentityConfirm_Success 输入本地密码后绑定、清空密码并建立会话。
func TestBindIdentityConfirm_Success(t *testing.T) {
	db, h := setupIdentityConfirmTest(t)
	user := createLocalTestUser(t, db, "alice", "secret123", "user")
	token := issueTestTicket(t, h, services.IdentityConfirmTicket{
		UserID: user.ID, Username: user.Username, Role: "user",
		AuthType: models.AuthSourceOAuth2, ProviderID: "keycloak-prod",
		ProviderName: "oauth2:keycloak-prod", ExternalID: "sub-1",
		ExternalUsername: "alice", ExternalName: "Alice",
	})

	c, w := newAuthTestContext(http.MethodPost, "/api/v1/web/auth/identity-confirm/bind", "10.2.0.1",
		gin.H{"password": "secret123"}, identityCookie(token))
	h.BindIdentityConfirm(c)

	if w.Code != http.StatusOK {
		t.Fatalf("绑定应成功，状态码 %d，body=%s", w.Code, w.Body.String())
	}
	var updated models.User
	if err := db.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if updated.AuthSource != models.AuthSourceOAuth2 || updated.PasswordHash != "" {
		t.Errorf("绑定后账号状态不正确: %+v", updated)
	}
	var record models.AuthRecord
	if err := db.Where("user_id = ? AND auth_type = ? AND provider_name = ? AND external_id = ?",
		user.ID, models.AuthSourceOAuth2, "oauth2:keycloak-prod", "sub-1").First(&record).Error; err != nil {
		t.Errorf("未写入绑定: %v", err)
	}
	if cookie := findCookie(w.Result().Cookies(), middleware.CookieTokenName); cookie == nil || cookie.Value == "" {
		t.Error("应写入登录 Cookie")
	}
	if _, ok := h.identityConfirm.Lookup(token); ok {
		t.Error("票据应已被消费")
	}
	// 绑定后本地密码失效：oauth2 账号不允许密码登录
	if _, err := h.authService.LoginByPassword(user.Username, "secret123"); err == nil {
		t.Error("绑定后不应再能用本地密码登录")
	}
}

// TestBindIdentityConfirm_AdminRejected 管理员同名账号拒绝自助绑定。
func TestBindIdentityConfirm_AdminRejected(t *testing.T) {
	db, h := setupIdentityConfirmTest(t)
	user := createLocalTestUser(t, db, "root", "secret123", "admin")
	token := issueTestTicket(t, h, services.IdentityConfirmTicket{
		UserID: user.ID, Username: user.Username, Role: "admin",
		AuthType: models.AuthSourceOAuth2, ProviderID: "keycloak-prod", ProviderName: "oauth2:keycloak-prod",
		ExternalID: "sub-admin", ExternalUsername: user.Username,
	})

	c, w := newAuthTestContext(http.MethodPost, "/api/v1/web/auth/identity-confirm/bind", "10.2.1.1",
		gin.H{"password": "secret123"}, identityCookie(token))
	h.BindIdentityConfirm(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("管理员账号应返回 403，实际 %d", w.Code)
	}
	var count int64
	db.Model(&models.AuthRecord{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 0 {
		t.Errorf("管理员账号不应写入绑定，实际 %d 条", count)
	}
}

// TestBindIdentityConfirm_WrongPassword 本地密码错误返回 401；连续错误达到上限后票据失效。
func TestBindIdentityConfirm_WrongPassword(t *testing.T) {
	db, h := setupIdentityConfirmTest(t)
	user := createLocalTestUser(t, db, "bob", "secret123", "user")
	token := issueTestTicket(t, h, services.IdentityConfirmTicket{
		UserID: user.ID, Username: user.Username, Role: "user",
		AuthType: models.AuthSourceOAuth2, ProviderID: "keycloak-prod", ProviderName: "oauth2:keycloak-prod",
		ExternalID: "sub-bob", ExternalUsername: user.Username,
	})
	ip := "10.2.2.9"
	t.Cleanup(func() { resetLoginFailures(ip) })

	for i := 0; i < services.IdentityConfirmMaxPasswordFailure; i++ {
		c, w := newAuthTestContext(http.MethodPost, "/api/v1/web/auth/identity-confirm/bind", ip,
			gin.H{"password": "wrong"}, identityCookie(token))
		h.BindIdentityConfirm(c)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次错误密码应返回 401，实际 %d", i+1, w.Code)
		}
	}
	if _, ok := h.identityConfirm.Lookup(token); ok {
		t.Error("错误次数达到上限后票据应失效")
	}
}

// TestCreateAccountIdentityConfirm 独立账号命名：外部用户名@provider_id，冲突追加序号。
func TestCreateAccountIdentityConfirm(t *testing.T) {
	t.Run("基础命名", func(t *testing.T) {
		db, h := setupIdentityConfirmTest(t)
		createLocalTestUser(t, db, "same_name", "secret123", "user")

		ticket := services.IdentityConfirmTicket{
			Username: "same_name", Role: "user",
			AuthType: models.AuthSourceOAuth2, ProviderID: "keycloak-prod",
			ProviderName: "oauth2:keycloak-prod", ExternalID: "sub-new",
			ExternalUsername: "same_name", ExternalName: "Same Name",
		}
		token := issueTestTicket(t, h, ticket)
		c, w := newAuthTestContext(http.MethodPost, "/api/v1/web/auth/identity-confirm/create-account", "10.3.0.1", nil, identityCookie(token))
		h.CreateAccountIdentityConfirm(c)
		if w.Code != http.StatusOK {
			t.Fatalf("创建独立账号应成功，状态码 %d，body=%s", w.Code, w.Body.String())
		}
		if got := responseUsername(t, w.Body.Bytes()); got != "same_name@keycloak-prod" {
			t.Errorf("独立账号用户名 = %q", got)
		}
		var created models.User
		if err := db.Where("username = ?", "same_name@keycloak-prod").First(&created).Error; err != nil {
			t.Fatalf("新账号未创建: %v", err)
		}
		if created.AuthSource != models.AuthSourceOAuth2 || created.CreatedVia != models.CreatedViaOAuth2JIT || created.PasswordHash != "" {
			t.Errorf("新账号属性不正确: %+v", created)
		}
	})

	t.Run("重名追加序号", func(t *testing.T) {
		db, h := setupIdentityConfirmTest(t)
		createLocalTestUser(t, db, "same_name", "secret123", "user")
		createLocalTestUser(t, db, "same_name@keycloak-prod", "secret123", "user")

		ticket := services.IdentityConfirmTicket{
			Username: "same_name", Role: "user",
			AuthType: models.AuthSourceOAuth2, ProviderID: "keycloak-prod",
			ProviderName: "oauth2:keycloak-prod", ExternalID: "sub-new-2",
			ExternalUsername: "same_name", ExternalName: "Same Name",
		}
		token := issueTestTicket(t, h, ticket)
		c, w := newAuthTestContext(http.MethodPost, "/api/v1/web/auth/identity-confirm/create-account", "10.3.0.2", nil, identityCookie(token))
		h.CreateAccountIdentityConfirm(c)
		if w.Code != http.StatusOK {
			t.Fatalf("创建独立账号应成功，状态码 %d，body=%s", w.Code, w.Body.String())
		}
		if got := responseUsername(t, w.Body.Bytes()); got != "same_name@keycloak-prod_2" {
			t.Errorf("冲突时应追加序号，实际 %q", got)
		}
	})
}

// responseUsername 解析登录响应中的用户名。
func responseUsername(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Data struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	return resp.Data.Username
}

// fakeLDAPVerifier 测试用 LDAP 凭据校验实现。
type fakeLDAPVerifier struct {
	enabled bool
	dn      string
	name    string
	err     error
}

func (f *fakeLDAPVerifier) IsEnabled() bool { return f.enabled }

func (f *fakeLDAPVerifier) VerifyCredentials(username, password string) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	return f.dn, f.name, nil
}

// TestBindLDAPIdentity_Cases 覆盖成功、密码错误、非本地账号与未启用场景。
func TestBindLDAPIdentity_Cases(t *testing.T) {
	db, h := setupIdentityConfirmTest(t)
	orig := appconfig.GlobalConfig.Auth.LDAP
	t.Cleanup(func() { appconfig.GlobalConfig.Auth.LDAP = orig })
	appconfig.GlobalConfig.Auth.LDAP.Enabled = true

	// 成功
	h.SetLDAPService(&fakeLDAPVerifier{enabled: true, dn: "uid=carol,dc=example,dc=com", name: "Carol"})
	user := createLocalTestUser(t, db, "carol", "secret123", "user")
	c, w := newAuthTestContext(http.MethodPost, "/api/v1/web/user/auth-bindings/ldap", "10.4.0.1", gin.H{"password": "dirpass"})
	c.Set("userUUID", user.UUID)
	h.BindLDAPIdentity(c)
	if w.Code != http.StatusOK {
		t.Fatalf("LDAP 绑定应成功，状态码 %d，body=%s", w.Code, w.Body.String())
	}
	var updated models.User
	if err := db.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if updated.AuthSource != models.AuthSourceLDAP || updated.PasswordHash != "" {
		t.Errorf("绑定后账号状态不正确: %+v", updated)
	}
	var record models.AuthRecord
	if err := db.Where("user_id = ? AND auth_type = ? AND provider_name = ? AND external_id = ?",
		user.ID, models.AuthSourceLDAP, "ldap", "uid=carol,dc=example,dc=com").First(&record).Error; err != nil {
		t.Errorf("未写入 LDAP 绑定: %v", err)
	}

	// 目录密码错误
	h.SetLDAPService(&fakeLDAPVerifier{enabled: true, err: services.ErrLDAPInvalidCredentials})
	local := createLocalTestUser(t, db, "dave", "secret123", "user")
	c2, w2 := newAuthTestContext(http.MethodPost, "/api/v1/web/user/auth-bindings/ldap", "10.4.0.2", gin.H{"password": "wrong"})
	c2.Set("userUUID", local.UUID)
	h.BindLDAPIdentity(c2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("目录密码错误应返回 401，实际 %d", w2.Code)
	}

	// 非本地账号
	external := models.User{UUID: "uuid-external", Username: "erin", Role: "user", AuthSource: models.AuthSourceOAuth2}
	if err := db.Create(&external).Error; err != nil {
		t.Fatalf("创建外部账号失败: %v", err)
	}
	c3, w3 := newAuthTestContext(http.MethodPost, "/api/v1/web/user/auth-bindings/ldap", "10.4.0.3", gin.H{"password": "dirpass"})
	c3.Set("userUUID", external.UUID)
	h.BindLDAPIdentity(c3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("非本地账号应返回 400，实际 %d", w3.Code)
	}

	// LDAP 未启用
	appconfig.GlobalConfig.Auth.LDAP.Enabled = false
	c4, w4 := newAuthTestContext(http.MethodPost, "/api/v1/web/user/auth-bindings/ldap", "10.4.0.4", gin.H{"password": "dirpass"})
	c4.Set("userUUID", local.UUID)
	h.BindLDAPIdentity(c4)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("LDAP 未启用应返回 400，实际 %d", w4.Code)
	}
}
