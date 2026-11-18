package services

import (
	"testing"
	"time"

	"fuzhan/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupIdentityConflictTestDB(t *testing.T) (*gorm.DB, *AdminService, models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuthRecord{}, &models.IdentityConflict{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	admin := models.User{
		UUID:         "uuid-admin",
		Username:     "admin",
		PasswordHash: "hash",
		Role:         "admin",
		AuthSource:   models.AuthSourceLocal,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	return db, NewAdminService(db), admin
}

func createConflict(t *testing.T, db *gorm.DB, username, authType, provider, externalID string) models.IdentityConflict {
	t.Helper()
	conflict := models.IdentityConflict{
		Username:       username,
		AuthType:       authType,
		ProviderName:   provider,
		ExternalID:     externalID,
		RejectedSource: models.AuthSourceLocal,
		AttemptCount:   1,
		LastSeenAt:     time.Now().UTC(),
	}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatalf("创建身份冲突失败: %v", err)
	}
	return conflict
}

func TestBatchBindIdentityConflictsSuccess(t *testing.T) {
	db, svc, admin := setupIdentityConflictTestDB(t)
	setUserListTestConfig(t, true, false)

	user := models.User{
		UUID:         "uuid-local",
		Username:     "zhangsan",
		PasswordHash: "local-hash",
		Role:         "user",
		AuthSource:   models.AuthSourceLocal,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建本地用户失败: %v", err)
	}
	conflict := createConflict(t, db, "zhangsan", models.AuthSourceLDAP, "ldap", "uid=zhangsan,dc=example")

	results, err := svc.BatchBindIdentityConflicts(admin.UUID, []uint{conflict.ID})
	if err != nil {
		t.Fatalf("批量绑定失败: %v", err)
	}
	if len(results) != 1 || results[0].Status != "bound" {
		t.Fatalf("期望绑定成功，实际: %+v", results)
	}

	var updated models.User
	if err := db.Where("id = ?", user.ID).First(&updated).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if updated.AuthSource != models.AuthSourceLDAP {
		t.Fatalf("期望 auth_source=ldap，实际 %s", updated.AuthSource)
	}
	if updated.PasswordHash != "" {
		t.Fatalf("绑定后本地密码应被清空，实际 %q", updated.PasswordHash)
	}

	var record models.AuthRecord
	if err := db.Where("user_id = ?", user.ID).First(&record).Error; err != nil {
		t.Fatalf("查询绑定记录失败: %v", err)
	}
	if record.ProviderName != "ldap" || record.ExternalID != conflict.ExternalID {
		t.Fatalf("绑定记录不符合预期: %+v", record)
	}

	var updatedConflict models.IdentityConflict
	if err := db.Where("id = ?", conflict.ID).First(&updatedConflict).Error; err != nil {
		t.Fatalf("查询冲突记录失败: %v", err)
	}
	if updatedConflict.Resolution != models.IdentityConflictResolutionBound || updatedConflict.ResolvedAt == nil {
		t.Fatalf("冲突应标记为 bound，实际: %+v", updatedConflict)
	}
}

func TestBatchBindIdentityConflictsSkips(t *testing.T) {
	t.Run("本地无同名账号", func(t *testing.T) {
		db, svc, admin := setupIdentityConflictTestDB(t)
		setUserListTestConfig(t, true, false)
		conflict := createConflict(t, db, "nobody", models.AuthSourceLDAP, "ldap", "uid=nobody,dc=example")

		results, err := svc.BatchBindIdentityConflicts(admin.UUID, []uint{conflict.ID})
		if err != nil {
			t.Fatalf("批量绑定返回错误: %v", err)
		}
		if results[0].Status != "skipped" || results[0].Message != "本地无同名账号" {
			t.Fatalf("期望跳过（本地无同名账号），实际: %+v", results[0])
		}
	})

	t.Run("用户已绑定其他来源", func(t *testing.T) {
		db, svc, admin := setupIdentityConflictTestDB(t)
		setUserListTestConfig(t, true, false)
		user := models.User{UUID: "uuid-bound", Username: "bound", Role: "user", AuthSource: models.AuthSourceLDAP}
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("创建用户失败: %v", err)
		}
		if err := db.Create(&models.AuthRecord{UserID: user.ID, AuthType: models.AuthSourceLDAP, ProviderName: "ldap", ExternalID: "uid=old"}).Error; err != nil {
			t.Fatalf("创建绑定失败: %v", err)
		}
		conflict := createConflict(t, db, "bound", models.AuthSourceLDAP, "ldap", "uid=new")

		results, err := svc.BatchBindIdentityConflicts(admin.UUID, []uint{conflict.ID})
		if err != nil {
			t.Fatalf("批量绑定返回错误: %v", err)
		}
		if results[0].Status != "skipped" || results[0].Message != "该用户已绑定其他认证来源，请先转为本地账号" {
			t.Fatalf("期望跳过（已绑定），实际: %+v", results[0])
		}
	})

	t.Run("认证源未启用", func(t *testing.T) {
		db, svc, admin := setupIdentityConflictTestDB(t)
		setUserListTestConfig(t, false, false)
		user := models.User{UUID: "uuid-off", Username: "off", PasswordHash: "hash", Role: "user", AuthSource: models.AuthSourceLocal}
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("创建用户失败: %v", err)
		}
		conflict := createConflict(t, db, "off", models.AuthSourceLDAP, "ldap", "uid=off")

		results, err := svc.BatchBindIdentityConflicts(admin.UUID, []uint{conflict.ID})
		if err != nil {
			t.Fatalf("批量绑定返回错误: %v", err)
		}
		if results[0].Status != "skipped" || results[0].Message != "认证源未启用" {
			t.Fatalf("期望跳过（认证源未启用），实际: %+v", results[0])
		}
	})
}

func TestBatchIgnoreIdentityConflictsOnlyPending(t *testing.T) {
	db, svc, admin := setupIdentityConflictTestDB(t)
	setUserListTestConfig(t, true, false)

	pending1 := createConflict(t, db, "a", models.AuthSourceLDAP, "ldap", "uid=a")
	pending2 := createConflict(t, db, "b", models.AuthSourceLDAP, "ldap", "uid=b")
	resolvedAt := time.Now().UTC().Add(-time.Hour)
	done := createConflict(t, db, "c", models.AuthSourceLDAP, "ldap", "uid=c")
	done.ResolvedAt = &resolvedAt
	done.Resolution = models.IdentityConflictResolutionBound
	if err := db.Save(&done).Error; err != nil {
		t.Fatalf("更新冲突状态失败: %v", err)
	}

	count, err := svc.BatchIgnoreIdentityConflicts(admin.UUID, []uint{pending1.ID, pending2.ID, done.ID})
	if err != nil {
		t.Fatalf("批量忽略失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("期望忽略 2 条，实际 %d", count)
	}

	var ignored models.IdentityConflict
	if err := db.Where("id = ?", pending1.ID).First(&ignored).Error; err != nil {
		t.Fatalf("查询冲突失败: %v", err)
	}
	if ignored.Resolution != models.IdentityConflictResolutionIgnored || ignored.ResolvedAt == nil {
		t.Fatalf("待处理冲突应标记为 ignored，实际: %+v", ignored)
	}

	var untouched models.IdentityConflict
	if err := db.Where("id = ?", done.ID).First(&untouched).Error; err != nil {
		t.Fatalf("查询冲突失败: %v", err)
	}
	if untouched.Resolution != models.IdentityConflictResolutionBound {
		t.Fatalf("已处理冲突不应被覆盖，实际: %+v", untouched)
	}
}

func TestUpsertIdentityConflictReopensIgnored(t *testing.T) {
	db, _, _ := setupIdentityConflictTestDB(t)

	resolvedAt := time.Now().UTC().Add(-time.Hour)
	conflict := createConflict(t, db, "reopen", models.AuthSourceOAuth2, "oauth2:test", "sub-1")
	conflict.ResolvedAt = &resolvedAt
	conflict.Resolution = models.IdentityConflictResolutionIgnored
	conflict.AttemptCount = 3
	if err := db.Save(&conflict).Error; err != nil {
		t.Fatalf("更新冲突失败: %v", err)
	}

	if err := models.UpsertIdentityConflict(db, &models.IdentityConflict{
		Username:       "reopen",
		AuthType:       models.AuthSourceOAuth2,
		ProviderName:   "oauth2:test",
		ExternalID:     "sub-1",
		RejectedSource: models.AuthSourceLocal,
		LastSeenAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("写入冲突失败: %v", err)
	}

	var reopened models.IdentityConflict
	if err := db.Where("id = ?", conflict.ID).First(&reopened).Error; err != nil {
		t.Fatalf("查询冲突失败: %v", err)
	}
	if reopened.ResolvedAt != nil || reopened.Resolution != "" {
		t.Fatalf("已忽略冲突再次触发后应重新打开，实际: %+v", reopened)
	}
	if reopened.AttemptCount != 4 {
		t.Fatalf("尝试次数应累加为 4，实际 %d", reopened.AttemptCount)
	}
}
