package auth

import (
	"testing"

	"fuzhan/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupLDAPTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuthRecord{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

func TestLDAPService_IsEnabled(t *testing.T) {
	db := setupLDAPTestDB(t)

	// 未启用
	svc := NewLDAPService(db, &LDAPConfig{Enabled: false})
	if svc.IsEnabled() {
		t.Error("未启用时 IsEnabled 应返回 false")
	}

	// 启用但无 Host
	svc = NewLDAPService(db, &LDAPConfig{Enabled: true, Host: ""})
	if svc.IsEnabled() {
		t.Error("无 Host 时 IsEnabled 应返回 false")
	}

	// 启用但有 Host 无 BaseDN
	svc = NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", Port: 389})
	if svc.IsEnabled() {
		t.Error("无 BaseDN 时 IsEnabled 应返回 false")
	}

	// 正确启用
	svc = NewLDAPService(db, &LDAPConfig{
		Enabled: true,
		Host:    "ldap.example.com",
		Port:    389,
		BaseDN:  "dc=example,dc=com",
	})
	if !svc.IsEnabled() {
		t.Error("正确配置时 IsEnabled 应返回 true")
	}
}

func TestLDAPService_UpdateConfig(t *testing.T) {
	db := setupLDAPTestDB(t)
	svc := NewLDAPService(db, &LDAPConfig{Enabled: false})

	if svc.IsEnabled() {
		t.Error("初始应为未启用")
	}

	svc.UpdateConfig(&LDAPConfig{Enabled: true, Host: "new.example.com", Port: 636, UseSSL: true, BaseDN: "dc=example,dc=com"})

	if !svc.IsEnabled() {
		t.Error("更新后应启用")
	}

	cfg := svc.GetConfig()
	if cfg.Host != "new.example.com" || cfg.Port != 636 || !cfg.UseSSL {
		t.Error("配置更新不正确")
	}
}

func TestLDAPService_Authenticate_NotEnabled(t *testing.T) {
	db := setupLDAPTestDB(t)
	svc := NewLDAPService(db, &LDAPConfig{Enabled: false})

	_, err := svc.Authenticate("user", "pass")
	if err == nil {
		t.Fatal("未启用时应返回错误")
	}
}

func TestLDAPService_Authenticate_EmptyCredentials(t *testing.T) {
	db := setupLDAPTestDB(t)
	svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", Port: 389, BaseDN: "dc=example,dc=com"})

	_, err := svc.Authenticate("", "pass")
	if err == nil {
		t.Error("空用户名应返回错误")
	}

	_, err = svc.Authenticate("user", "")
	if err == nil {
		t.Error("空密码应返回错误")
	}
}

func TestLDAPService_BuildUserFilter(t *testing.T) {
	db := setupLDAPTestDB(t)
	cases := []struct {
		name    string
		filter  string
		user    string
		want    string
	}{
		{"默认(uid)", "", "zhangsan", "(uid=zhangsan)"},
		{"OpenLDAP模板", "(uid=%s)", "zhangsan", "(uid=zhangsan)"},
		{"AD模板", "(sAMAccountName=%s)", "zhangsan", "(sAMAccountName=zhangsan)"},
		{"静态过滤器", "(objectClass=person)", "zhangsan", "(objectClass=person)"},
		// RFC 4515：\ 转义为 \5c（go-ldap 的 EscapeFilter）
		{"特殊字符转义", "(uid=%s)", "a(b)*c\\d", "(uid=a\\28b\\29\\2ac\\5cd)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewLDAPService(db, &LDAPConfig{UserFilter: tc.filter})
			got := svc.buildUserFilter(svc.GetConfig(), tc.user)
			if got != tc.want {
				t.Errorf("buildUserFilter(%q, %q) = %q, want %q", tc.filter, tc.user, got, tc.want)
			}
		})
	}
}

func TestLDAPService_UsernameAttribute(t *testing.T) {
	db := setupLDAPTestDB(t)
	cases := []struct {
		name, filter, want string
	}{
		{"空", "", "uid"},
		{"uid", "(uid=%s)", "uid"},
		{"AD", "(sAMAccountName=%s)", "sAMAccountName"},
		{"复合过滤器回退", "(&(objectClass=person)(uid=%s))", "uid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewLDAPService(db, &LDAPConfig{UserFilter: tc.filter})
			got := svc.usernameAttribute(svc.GetConfig())
			if got != tc.want {
				t.Errorf("usernameAttribute(%q) = %q, want %q", tc.filter, got, tc.want)
			}
		})
	}
}

func TestLDAPService_SyncFilter(t *testing.T) {
	db := setupLDAPTestDB(t)
	cases := []struct {
		name, filter, want string
	}{
		{"默认遍历", "", "(uid=*)"},
		{"uid遍历", "(uid=%s)", "(uid=*)"},
		{"AD遍历", "(sAMAccountName=%s)", "(sAMAccountName=*)"},
		{"静态过滤器", "(objectClass=person)", "(objectClass=person)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewLDAPService(db, &LDAPConfig{UserFilter: tc.filter})
			got := svc.syncFilter(svc.GetConfig())
			if got != tc.want {
				t.Errorf("syncFilter(%q) = %q, want %q", tc.filter, got, tc.want)
			}
		})
	}
}

func TestLDAPService_FindOrCreateUser_Existing(t *testing.T) {
	db := setupLDAPTestDB(t)

	db.Create(&models.User{
		Username: "existing_user",
		Role:     "user",
	})

	svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com"})

	user, err := svc.findOrCreateUser("existing_user", map[string]string{"dn": "uid=existing_user,dc=example,dc=com"})
	if err != nil {
		t.Fatalf("查找已有用户失败: %v", err)
	}
	if user.Username != "existing_user" {
		t.Errorf("用户名不匹配: %s", user.Username)
	}
}

func TestLDAPService_FindOrCreateUser_AutoCreate(t *testing.T) {
	db := setupLDAPTestDB(t)

	svc := NewLDAPService(db, &LDAPConfig{
		Enabled:        true,
		Host:           "ldap.example.com",
		BaseDN:         "dc=example,dc=com",
		AutoCreateUser: true,
	})

	user, err := svc.findOrCreateUser("new_ldap_user", map[string]string{"dn": "uid=new_ldap_user,dc=example,dc=com"})
	if err != nil {
		t.Fatalf("自动创建用户失败: %v", err)
	}
	if user.Username != "new_ldap_user" {
		t.Errorf("用户名不匹配: %s", user.Username)
	}
	if user.PasswordHash != "" {
		t.Error("LDAP 用户不应有本地密码")
	}

	var record models.AuthRecord
	if err := db.Where("user_id = ? AND auth_type = ?", user.ID, "ldap").First(&record).Error; err != nil {
		t.Errorf("认证记录未创建: %v", err)
	}
}

func TestLDAPService_FindOrCreateUser_NoAutoCreate(t *testing.T) {
	db := setupLDAPTestDB(t)

	svc := NewLDAPService(db, &LDAPConfig{
		Enabled:        true,
		Host:           "ldap.example.com",
		BaseDN:         "dc=example,dc=com",
		AutoCreateUser: false,
	})

	_, err := svc.findOrCreateUser("nonexistent_user", map[string]string{})
	if err == nil {
		t.Fatal("未启用自动创建时应返回错误")
	}
}

func TestLDAPService_TestConnection_NoHost(t *testing.T) {
	svc := NewLDAPService(nil, &LDAPConfig{Enabled: true})
	if err := svc.TestConnection(); err == nil {
		t.Error("未配置 Host 时 TestConnection 应返回错误")
	}
}