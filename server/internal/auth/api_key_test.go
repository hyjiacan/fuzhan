package auth

import (
    "testing"
    "time"

    "github.com/glebarez/sqlite"
    "gorm.io/gorm"
    "fuzhan/internal/models"
)

// setupTestDB 创建内存 SQLite 数据库用于测试
func setupTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil {
        t.Fatalf("打开内存数据库失败: %v", err)
    }
    if err := db.AutoMigrate(&models.ApiKey{}, &models.User{}); err != nil {
        t.Fatalf("自动迁移失败: %v", err)
    }
    // 创建测试用户
    db.Create(&models.User{
        Username: "admin",
        PasswordHash: "hash",
        Role: "admin",
    })
    return db
}

func TestCreateApiKey(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, err := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "Test Key",
        UserID: 1,
        Scopes: "open_api:reader",
    })
    if err != nil {
        t.Fatalf("创建 API Key 失败: %v", err)
    }
    if resp.RawKey == "" {
        t.Fatal("期望返回明文密钥")
    }
    if resp.ApiKey.Name != "Test Key" {
        t.Errorf("名称不匹配: %s", resp.ApiKey.Name)
    }
    if resp.ApiKey.Scopes != "open_api:reader" {
        t.Errorf("Scope 不匹配: %s", resp.ApiKey.Scopes)
    }
    if resp.ApiKey.Status != "active" {
        t.Errorf("状态应为 active")
    }
}

func TestCreateApiKey_DefaultScope(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, err := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "Key With Default Scope",
        UserID: 1,
    })
    if err != nil {
        t.Fatalf("创建 API Key 失败: %v", err)
    }
    if resp.ApiKey.Scopes != "open_api:reader" {
        t.Errorf("期望默认 scope 为 open_api:reader，实际 %s", resp.ApiKey.Scopes)
    }
}

func TestCreateApiKey_WithExpiry(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, err := svc.CreateApiKey(CreateApiKeyRequest{
        Name:      "Expiring Key",
        UserID:    1,
        Scopes:    "open_api:reader",
        ExpiresIn: 3600, // 1小时
    })
    if err != nil {
        t.Fatalf("创建 API Key 失败: %v", err)
    }
    if resp.ApiKey.ExpiresAt == nil {
        t.Fatal("期望有过期时间")
    }
    if resp.ApiKey.ExpiresAt.Before(time.Now().Add(3599 * time.Second)) {
        t.Error("过期时间不正确")
    }
}

func TestValidateApiKey_Success(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, _ := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "Valid Key",
        UserID: 1,
        Scopes: "open_api:reader",
    })

    userID, scopes, err := svc.ValidateApiKey(resp.RawKey)
    if err != nil {
        t.Fatalf("验证 API Key 失败: %v", err)
    }
    if userID != 1 {
        t.Errorf("用户 ID 不匹配: %d", userID)
    }
    if scopes != "open_api:reader" {
        t.Errorf("Scope 不匹配: %s", scopes)
    }
}

func TestValidateApiKey_InvalidKey(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    _, _, err := svc.ValidateApiKey("invalid_key_format")
    if err == nil {
        t.Fatal("期望无效密钥验证失败")
    }
}

func TestValidateApiKey_WrongFormat(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    _, _, err := svc.ValidateApiKey("ab")
    if err == nil {
        t.Fatal("期望短密钥格式验证失败")
    }
}

func TestUpdateApiKeyStatus(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, _ := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "Test Key",
        UserID: 1,
        Scopes: "open_api:reader",
    })

    // 禁用
    if err := svc.UpdateApiKeyStatus(resp.ApiKey.ID, "disabled"); err != nil {
        t.Fatalf("禁用 API Key 失败: %v", err)
    }

    // 验证禁用后不能使用
    _, _, err := svc.ValidateApiKey(resp.RawKey)
    if err == nil {
        t.Error("禁用后验证应失败")
    }

    // 重新启用
    if err := svc.UpdateApiKeyStatus(resp.ApiKey.ID, "active"); err != nil {
        t.Fatalf("启用 API Key 失败: %v", err)
    }
}

func TestUpdateApiKeyStatus_InvalidStatus(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    err := svc.UpdateApiKeyStatus(1, "invalid_status")
    if err == nil {
        t.Fatal("期望无效状态错误")
    }
}

func TestDeleteApiKey(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, _ := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "To Delete",
        UserID: 1,
        Scopes: "open_api:reader",
    })

    if err := svc.DeleteApiKey(resp.ApiKey.ID); err != nil {
        t.Fatalf("删除 API Key 失败: %v", err)
    }

    // 验证已删除
    _, err := svc.GetApiKey(resp.ApiKey.ID)
    if err == nil {
        t.Error("删除后查询应返回错误")
    }
}

func TestDeleteApiKey_NotFound(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    err := svc.DeleteApiKey(99999)
    if err == nil {
        t.Fatal("期望不存在的 Key 删除失败")
    }
}

func TestListApiKeys(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    svc.CreateApiKey(CreateApiKeyRequest{Name: "Key1", UserID: 1, Scopes: "open_api:reader"})
    svc.CreateApiKey(CreateApiKeyRequest{Name: "Key2", UserID: 1, Scopes: "open_api:writer"})

    keys, total, err := svc.ListApiKeys(1, 10)
    if err != nil {
        t.Fatalf("查询 API Key 失败: %v", err)
    }
    if total != 2 {
        t.Errorf("期望 2 条记录，实际 %d", total)
    }
    if len(keys) != 2 {
        t.Errorf("期望返回 2 条，实际 %d", len(keys))
    }
}

func TestGetApiKey(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    resp, _ := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "Get Me",
        UserID: 1,
        Scopes: "open_api:reader",
    })

    key, err := svc.GetApiKey(resp.ApiKey.ID)
    if err != nil {
        t.Fatalf("查询失败: %v", err)
    }
    if key.Name != "Get Me" {
        t.Errorf("名称不匹配: %s", key.Name)
    }
    // 验证密钥不返回明文
    if key.ApiKey != "" && key.ApiKey == resp.RawKey {
        t.Error("不应返回明文密钥")
    }
}

func TestGetApiKey_NotFound(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    _, err := svc.GetApiKey(99999)
    if err == nil {
        t.Fatal("期望不存在的 Key 查询失败")
    }
}

func TestHasScope_Reader(t *testing.T) {
    key := &models.ApiKey{Scopes: "open_api:reader"}
    if !key.HasScope("open_api:reader") {
        t.Error("reader 应包含 reader scope")
    }
    if key.HasScope("open_api:writer") {
        t.Error("reader 不应包含 writer scope")
    }
    if key.HasScope("open_api:admin") {
        t.Error("reader 不应包含 admin scope")
    }
}

func TestHasScope_Writer(t *testing.T) {
    key := &models.ApiKey{Scopes: "open_api:writer"}
    if !key.HasScope("open_api:reader") {
        t.Error("writer 应包含 reader scope（层级关系）")
    }
    if !key.HasScope("open_api:writer") {
        t.Error("writer 应包含 writer scope")
    }
    if key.HasScope("open_api:admin") {
        t.Error("writer 不应包含 admin scope")
    }
}

func TestHasScope_Admin(t *testing.T) {
    key := &models.ApiKey{Scopes: "open_api:admin"}
    if !key.HasScope("open_api:reader") {
        t.Error("admin 应包含 reader scope")
    }
    if !key.HasScope("open_api:writer") {
        t.Error("admin 应包含 writer scope")
    }
    if !key.HasScope("open_api:admin") {
        t.Error("admin 应包含 admin scope")
    }
}

func TestHasScope_Multiple(t *testing.T) {
    key := &models.ApiKey{Scopes: "open_api:reader,open_api:writer"}
    if !key.HasScope("open_api:reader") {
        t.Error("应包含 reader")
    }
    if !key.HasScope("open_api:writer") {
        t.Error("应包含 writer")
    }
    if key.HasScope("open_api:admin") {
        t.Error("不应包含 admin")
    }
}

func TestExpiredApiKey(t *testing.T) {
    db := setupTestDB(t)
    svc := NewApiKeyService(db)

    // 创建已过期的 key
    expiredAt := time.Now().Add(-1 * time.Hour)
    key := models.ApiKey{
        KeyID:     "test_expired",
        ApiKey:    "hashed",
        Name:      "Expired",
        UserID:    1,
        Scopes:    "open_api:reader",
        Status:    "active",
        ExpiresAt: &expiredAt,
    }
    db.Create(&key)

    // 创建正常 key
    resp, _ := svc.CreateApiKey(CreateApiKeyRequest{
        Name:   "Valid",
        UserID: 1,
        Scopes: "open_api:reader",
    })

    // 正常 key 应可用
    userID, scopes, err := svc.ValidateApiKey(resp.RawKey)
    if err != nil {
        t.Errorf("正常 key 验证失败: %v", err)
    } else {
        if userID != 1 || scopes != "open_api:reader" {
            t.Error("正常 key 返回信息不正确")
        }
    }
}
