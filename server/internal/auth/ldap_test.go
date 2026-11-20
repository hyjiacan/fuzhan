package auth

import (
	"errors"
	"testing"

	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"github.com/glebarez/sqlite"
	"github.com/go-ldap/ldap/v3"
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
		name   string
		filter string
		user   string
		want   string
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
		name, filter, explicit, want string
	}{
		{"空", "", "", "uid"},
		{"uid", "(uid=%s)", "", "uid"},
		{"AD", "(sAMAccountName=%s)", "", "sAMAccountName"},
		{"复合过滤器回退", "(&(objectClass=person)(uid=%s))", "", "uid"},
		{"显式配置优先", "(&(objectClass=person)(uid=%s))", "sAMAccountName", "sAMAccountName"},
		{"显式配置优先于简单过滤器", "(uid=%s)", " mail ", "mail"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewLDAPService(db, &LDAPConfig{UserFilter: tc.filter, UsernameAttribute: tc.explicit})
			got := svc.usernameAttribute(svc.GetConfig())
			if got != tc.want {
				t.Errorf("usernameAttribute(%q, %q) = %q, want %q", tc.filter, tc.explicit, got, tc.want)
			}
		})
	}
}

func TestLDAPService_UsernameAttributeCandidates(t *testing.T) {
	db := setupLDAPTestDB(t)
	svc := NewLDAPService(db, &LDAPConfig{UserFilter: "(uid=%s)"})
	got := svc.usernameAttributeCandidates(svc.GetConfig())
	want := []string{"uid", "sAMAccountName", "mail"}
	if len(got) != len(want) {
		t.Fatalf("usernameAttributeCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("usernameAttributeCandidates() = %v, want %v", got, want)
		}
	}
}

func TestLDAPService_PickUniqueEntry(t *testing.T) {
	entries := []*ldap.Entry{
		ldap.NewEntry("uid=zhangsan,ou=Users,dc=example,dc=com", map[string][]string{
			"uid": {"zhangsan"},
			"cn":  {"Other"},
		}),
		ldap.NewEntry("cn=ZhangSan,ou=Users,dc=example,dc=com", map[string][]string{
			"uid": {"Zhangsan"},
			"cn":  {"ZhangSan"},
		}),
	}
	if got := pickUniqueEntry(entries, "uid", "zhangsan"); got == nil || got.DN != entries[0].DN {
		t.Errorf("精确匹配应选中 %s", entries[0].DN)
	}
	// 无精确匹配时退化为忽略大小写，此时两条都命中 → 无法消歧
	if got := pickUniqueEntry(entries, "uid", "ZHANGSAN"); got != nil {
		t.Errorf("忽略大小写命中多条时不应返回结果，却返回了 %s", got.DN)
	}
	// 用户名属性缺失于条目时不匹配
	if got := pickUniqueEntry(entries, "sAMAccountName", "zhangsan"); got != nil {
		t.Errorf("属性不存在时不应返回结果，却返回了 %s", got.DN)
	}
}

func TestJoinEntryDNs(t *testing.T) {
	entries := []*ldap.Entry{
		ldap.NewEntry("dn1", nil),
		ldap.NewEntry("dn2", nil),
	}
	if got := joinEntryDNs(entries, 5); got != "dn1, dn2" {
		t.Errorf("joinEntryDNs() = %q", got)
	}
	if got := joinEntryDNs(entries, 1); got != "dn1, ..." {
		t.Errorf("joinEntryDNs() 截断结果 = %q", got)
	}
}

func TestLDAPService_FindOrCreateUser_Existing(t *testing.T) {
	db := setupLDAPTestDB(t)

	// 历史 LDAP 用户：auth_source=ldap、空密码、无绑定记录
	db.Create(&models.User{
		Username:   "existing_user",
		AuthSource: models.AuthSourceLDAP,
		Role:       "user",
	})

	svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com"})

	user, err := svc.findOrCreateUser("existing_user", map[string]string{
		"dn":   "uid=existing_user,dc=example,dc=com",
		"name": "Existing User",
	}, models.CreatedViaLDAPJIT)
	if err != nil {
		t.Fatalf("查找已有用户失败: %v", err)
	}
	if user.Username != "existing_user" {
		t.Errorf("用户名不匹配: %s", user.Username)
	}
	// 历史用户首次登录应补齐绑定
	var record models.AuthRecord
	if err := db.Where("user_id = ? AND auth_type = ? AND provider_name = ? AND external_id = ?",
		user.ID, models.AuthSourceLDAP, ldapProviderName, "uid=existing_user,dc=example,dc=com").First(&record).Error; err != nil {
		t.Errorf("历史 LDAP 用户未补齐绑定: %v", err)
	}
}

// TestLDAPService_FindOrCreateUser_LocalConflict 同名本地账号必须拒绝且不修改任何字段。
func TestLDAPService_FindOrCreateUser_LocalConflict(t *testing.T) {
	db := setupLDAPTestDB(t)

	local := models.User{
		Username:     "conflict_user",
		PasswordHash: "hash",
		AuthSource:   models.AuthSourceLocal,
		Role:         "user",
	}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("创建本地用户失败: %v", err)
	}

	svc := NewLDAPService(db, &LDAPConfig{
		Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com", AutoCreateUser: true,
	})
	_, err := svc.findOrCreateUser("conflict_user", map[string]string{
		"dn":   "uid=conflict_user,dc=example,dc=com",
		"name": "冲突用户",
	}, models.CreatedViaLDAPJIT)
	if !errors.Is(err, services.ErrAccountUnavailable) {
		t.Fatalf("同名本地账号应拒绝登录，实际 err=%v", err)
	}

	var got models.User
	if err := db.Where("id = ?", local.ID).First(&got).Error; err != nil {
		t.Fatalf("重新读取用户失败: %v", err)
	}
	if got.AuthSource != models.AuthSourceLocal || got.PasswordHash != "hash" || got.DisplayName != "" {
		t.Errorf("冲突拒绝不得修改用户字段: %+v", got)
	}
	var count int64
	db.Model(&models.AuthRecord{}).Where("user_id = ?", local.ID).Count(&count)
	if count != 0 {
		t.Errorf("冲突拒绝不得写入绑定记录，实际 %d 条", count)
	}
}

// TestLDAPService_FindOrCreateUser_ProviderMismatch provider 与当前配置不一致时拒绝登录。
func TestLDAPService_FindOrCreateUser_ProviderMismatch(t *testing.T) {
	db := setupLDAPTestDB(t)

	user := models.User{Username: "mismatch_user", AuthSource: models.AuthSourceLDAP, Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建 LDAP 用户失败: %v", err)
	}
	if err := db.Create(&models.AuthRecord{
		UserID: user.ID, AuthType: models.AuthSourceLDAP,
		ProviderName: "old-ldap-host", ExternalID: "uid=mismatch_user,dc=example,dc=com",
	}).Error; err != nil {
		t.Fatalf("创建旧绑定失败: %v", err)
	}

	svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com"})
	_, err := svc.findOrCreateUser("mismatch_user", map[string]string{
		"dn": "uid=mismatch_user,dc=example,dc=com",
	}, models.CreatedViaLDAPJIT)
	if !errors.Is(err, services.ErrAccountUnavailable) {
		t.Fatalf("provider 不匹配应拒绝登录，实际 err=%v", err)
	}
}

// TestLDAPService_FindOrCreateUser_RenameOnDNMatch DN 命中后目录用户名变化应同步本地用户名。
func TestLDAPService_FindOrCreateUser_RenameOnDNMatch(t *testing.T) {
	db := setupLDAPTestDB(t)

	user := models.User{Username: "old_name", AuthSource: models.AuthSourceLDAP, Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建 LDAP 用户失败: %v", err)
	}
	if err := db.Create(&models.AuthRecord{
		UserID: user.ID, AuthType: models.AuthSourceLDAP,
		ProviderName: ldapProviderName, ExternalID: "uid=old_name,dc=example,dc=com",
	}).Error; err != nil {
		t.Fatalf("创建绑定失败: %v", err)
	}

	svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com"})
	got, err := svc.findOrCreateUser("new_name", map[string]string{
		"dn": "uid=old_name,dc=example,dc=com",
	}, models.CreatedViaLDAPJIT)
	if err != nil {
		t.Fatalf("DN 命中时应允许登录: %v", err)
	}
	if got.Username != "new_name" {
		t.Errorf("目录用户名变化后本地用户名应更新，实际 %q", got.Username)
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

	user, err := svc.findOrCreateUser("new_ldap_user", map[string]string{
		"dn":   "uid=new_ldap_user,dc=example,dc=com",
		"name": "New User",
	}, models.CreatedViaLDAPJIT)
	if err != nil {
		t.Fatalf("自动创建用户失败: %v", err)
	}
	if user.Username != "new_ldap_user" {
		t.Errorf("用户名不匹配: %s", user.Username)
	}
	if user.PasswordHash != "" {
		t.Error("LDAP 用户不应有本地密码")
	}
	if user.AuthSource != models.AuthSourceLDAP || user.CreatedVia != models.CreatedViaLDAPJIT {
		t.Errorf("JIT 用户来源/渠道不正确: %+v", user)
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

	_, err := svc.findOrCreateUser("nonexistent_user", map[string]string{
		"dn": "uid=nonexistent_user,dc=example,dc=com",
	}, models.CreatedViaLDAPJIT)
	if !errors.Is(err, services.ErrAccountUnavailable) {
		t.Fatal("未启用自动创建时应返回账号不可用错误")
	}
}

func TestLDAPService_TestConnection_NoHost(t *testing.T) {
	svc := NewLDAPService(nil, &LDAPConfig{Enabled: true})
	if err := svc.TestConnection(); err == nil {
		t.Error("未配置 Host 时 TestConnection 应返回错误")
	}
}

func TestLDAPService_FindOrCreateUser_Disabled(t *testing.T) {
	db := setupLDAPTestDB(t)
	if err := db.Create(&models.User{
		Username:     "disabled_ldap_user",
		PasswordHash: "",
		Disabled:     true,
	}).Error; err != nil {
		t.Fatalf("创建禁用用户失败: %v", err)
	}

	svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com"})
	if _, err := svc.findOrCreateUser("disabled_ldap_user", map[string]string{
		"dn": "uid=disabled_ldap_user,dc=example,dc=com",
	}, models.CreatedViaLDAPJIT); err == nil {
		t.Fatal("已禁用的本地用户不应通过 LDAP 认证")
	}
}
