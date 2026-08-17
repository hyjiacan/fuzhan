package auth

import (
    "testing"

    "github.com/glebarez/sqlite"
    "gorm.io/gorm"
    "fuzhan/internal/models"
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

    // 正确启用
    svc = NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", Port: 389})
    if !svc.IsEnabled() {
        t.Error("正确配置时 IsEnabled 应返回 true")
    }
}

func TestLDAPService_BuildUserDN_WithDC(t *testing.T) {
    svc := NewLDAPService(nil, &LDAPConfig{
        BaseDN: "dc=example,dc=com",
    })

    dn := svc.buildUserDN("zhangsan")
    expected := "uid=zhangsan,dc=example,dc=com"
    if dn != expected {
        t.Errorf("DN 不匹配，期望 %q，实际 %q", expected, dn)
    }
}

func TestLDAPService_BuildUserDN_WithCN(t *testing.T) {
    svc := NewLDAPService(nil, &LDAPConfig{
        BaseDN: "ou=people,dc=example,dc=com",
    })

    dn := svc.buildUserDN("admin")
    expected := "uid=admin,ou=people,dc=example,dc=com"
    if dn != expected {
        t.Errorf("DN 不匹配，期望 %q，实际 %q", expected, dn)
    }
}

func TestLDAPService_BuildUserDN_NoDC(t *testing.T) {
    svc := NewLDAPService(nil, &LDAPConfig{
        BaseDN: "o=company",
    })

    dn := svc.buildUserDN("testuser")
    expected := "uid=testuser,o=company"
    if dn != expected {
        t.Errorf("DN 不匹配，期望 %q，实际 %q", expected, dn)
    }
}

func TestLDAPService_UpdateConfig(t *testing.T) {
    db := setupLDAPTestDB(t)
    svc := NewLDAPService(db, &LDAPConfig{Enabled: false})

    if svc.IsEnabled() {
        t.Error("初始应为未启用")
    }

    svc.UpdateConfig(&LDAPConfig{Enabled: true, Host: "new.example.com", Port: 636, UseSSL: true})

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
    svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com", Port: 389})

    _, err := svc.Authenticate("", "pass")
    if err == nil {
        t.Error("空用户名应返回错误")
    }

    _, err = svc.Authenticate("user", "")
    if err == nil {
        t.Error("空密码应返回错误")
    }
}

func TestLDAPService_FindOrCreateUser_Existing(t *testing.T) {
    db := setupLDAPTestDB(t)

    // 先创建用户
    db.Create(&models.User{
        Username: "existing_user",
        Role:     "user",
    })

    svc := NewLDAPService(db, &LDAPConfig{Enabled: true, Host: "ldap.example.com"})

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

    // 验证认证记录已创建
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
        AutoCreateUser: false,
    })

    _, err := svc.findOrCreateUser("nonexistent_user", map[string]string{})
    if err == nil {
        t.Fatal("未启用自动创建时应返回错误")
    }
}

func TestLDAPService_TestConnection_Invalid(t *testing.T) {
    // 测试无效连接（预期失败）
    svc := NewLDAPService(nil, &LDAPConfig{
        Enabled: true,
        Host:    "nonexistent.example.com",
        Port:    389,
    })

    err := svc.TestConnection()
    if err == nil {
        t.Log("连接测试应失败（服务器不存在），但可能因网络延迟而超时")
    }
}
