package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupIdentityMigrationTestDB 创建认证归属迁移测试用的 SQLite 内存库
func setupIdentityMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&User{}, &AuthRecord{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

func mustCreateUser(t *testing.T, db *gorm.DB, username, passwordHash string) *User {
	t.Helper()
	u := &User{UUID: "uuid-" + username, Username: username, PasswordHash: passwordHash, Role: "user"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("创建用户 %s 失败: %v", username, err)
	}
	return u
}

func mustCreateAuthRecord(t *testing.T, db *gorm.DB, userID uint, authType, provider, externalID string) *AuthRecord {
	t.Helper()
	r := &AuthRecord{UserID: userID, AuthType: authType, ProviderName: provider, ExternalID: externalID}
	if err := db.Create(r).Error; err != nil {
		t.Fatalf("创建认证绑定失败: %v", err)
	}
	return r
}

// 三类历史用户的回填结果 + 幂等执行两次
func TestMigrateUserIdentity_BackfillAndIdempotent(t *testing.T) {
	db := setupIdentityMigrationTestDB(t)

	ldapUser := mustCreateUser(t, db, "ldapuser", "")
	localUser := mustCreateUser(t, db, "localuser", "bcrypt-hash")
	legacyLdapUser := mustCreateUser(t, db, "legacyldap", "")
	mustCreateAuthRecord(t, db, ldapUser.ID, AuthSourceLDAP, "ldap:company", "uid=ldapuser,dc=x")

	MigrateUserIdentity(db)
	MigrateUserIdentity(db) // 幂等：第二次执行不得改变结果

	cases := []struct {
		user     *User
		expected string
	}{
		{ldapUser, AuthSourceLDAP},
		{localUser, AuthSourceLocal},
		{legacyLdapUser, AuthSourceLDAP}, // 空密码且无绑定：防御性回填
	}
	for _, tc := range cases {
		var got User
		if err := db.First(&got, tc.user.ID).Error; err != nil {
			t.Fatalf("读取用户失败: %v", err)
		}
		if got.AuthSource != tc.expected {
			t.Errorf("用户 %s 的 auth_source 期望 %s，实际 %s", got.Username, tc.expected, got.AuthSource)
		}
		if got.CreatedVia != CreatedViaLegacy {
			t.Errorf("用户 %s 的 created_via 期望 %s，实际 %s", got.Username, CreatedViaLegacy, got.CreatedVia)
		}
	}

	// LDAP 绑定的 provider_name 被归一化为 "ldap"
	var rec AuthRecord
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("读取绑定失败: %v", err)
	}
	if rec.ProviderName != AuthSourceLDAP {
		t.Errorf("provider_name 期望 %s，实际 %s", AuthSourceLDAP, rec.ProviderName)
	}

	// 无重复时创建唯一索引
	if !db.Migrator().HasIndex(&AuthRecord{}, authRecordsUniqueIndex) {
		t.Errorf("期望创建唯一索引 %s", authRecordsUniqueIndex)
	}
}

// 同一用户重复绑定：保留 id 最小的一条并建索引
func TestMigrateUserIdentity_DedupeSameUser(t *testing.T) {
	db := setupIdentityMigrationTestDB(t)
	u := mustCreateUser(t, db, "dupuser", "hash")
	first := mustCreateAuthRecord(t, db, u.ID, AuthSourceLDAP, AuthSourceLDAP, "uid=dup,dc=x")
	mustCreateAuthRecord(t, db, u.ID, AuthSourceLDAP, AuthSourceLDAP, "uid=dup,dc=x")

	MigrateUserIdentity(db)

	var records []AuthRecord
	if err := db.Where("user_id = ?", u.ID).Find(&records).Error; err != nil {
		t.Fatalf("读取绑定失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("去重后期望 1 条绑定，实际 %d", len(records))
	}
	if records[0].ID != first.ID {
		t.Errorf("期望保留 id 最小的记录 %d，实际 %d", first.ID, records[0].ID)
	}
	if !db.Migrator().HasIndex(&AuthRecord{}, authRecordsUniqueIndex) {
		t.Errorf("去重后应创建唯一索引 %s", authRecordsUniqueIndex)
	}
}

// 跨用户冲突：不删除记录，且跳过唯一索引创建
func TestMigrateUserIdentity_CrossUserConflictSkipsIndex(t *testing.T) {
	db := setupIdentityMigrationTestDB(t)
	u1 := mustCreateUser(t, db, "user1", "hash1")
	u2 := mustCreateUser(t, db, "user2", "hash2")
	mustCreateAuthRecord(t, db, u1.ID, AuthSourceLDAP, AuthSourceLDAP, "uid=shared,dc=x")
	mustCreateAuthRecord(t, db, u2.ID, AuthSourceLDAP, AuthSourceLDAP, "uid=shared,dc=x")

	MigrateUserIdentity(db)

	var count int64
	if err := db.Model(&AuthRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("统计绑定失败: %v", err)
	}
	if count != 2 {
		t.Errorf("跨用户冲突不应删除记录，期望 2 条，实际 %d", count)
	}
	if db.Migrator().HasIndex(&AuthRecord{}, authRecordsUniqueIndex) {
		t.Errorf("存在跨用户冲突时不应创建唯一索引 %s", authRecordsUniqueIndex)
	}
}
