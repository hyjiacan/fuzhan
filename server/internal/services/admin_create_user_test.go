package services

import (
	"testing"

	"fuzhan/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupAdminUserTestDB 创建用于测试管理员添加用户的 SQLite 内存数据库
func setupAdminUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

func TestAdminService_CreateUser(t *testing.T) {
	db := setupAdminUserTestDB(t)
	svc := NewAdminService(db)

	// 创建调用方管理员
	admin := models.User{Username: "admin01", PasswordHash: "hash", Role: "admin"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	user, err := svc.CreateUser(admin.UUID, "zhangsan", "张三", "secret123", "user")
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if user.Username != "zhangsan" {
		t.Errorf("用户名不匹配: %s", user.Username)
	}
	if user.DisplayName != "张三" {
		t.Errorf("姓名不匹配: %s", user.DisplayName)
	}
	if user.Role != "user" {
		t.Errorf("角色不匹配: %s", user.Role)
	}
	if user.UUID == "" {
		t.Error("UUID 不应为空")
	}
	if user.PasswordHash == "secret123" || user.PasswordHash == "" {
		t.Error("密码应被加密存储")
	}

	// 校验默认角色为 user
	user2, err := svc.CreateUser(admin.UUID, "lisi", "", "secret123", "")
	if err != nil {
		t.Fatalf("创建用户失败(默认角色): %v", err)
	}
	if user2.Role != "user" {
		t.Errorf("默认角色应为 user，实际 %s", user2.Role)
	}
}

func TestAdminService_CreateUser_Validation(t *testing.T) {
	db := setupAdminUserTestDB(t)
	svc := NewAdminService(db)

	admin := models.User{Username: "admin02", PasswordHash: "hash", Role: "admin"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	cases := []struct {
		name     string
		username string
		password string
		role     string
		wantErr  bool
	}{
		{name: "用户名过短", username: "ab", password: "secret123", wantErr: true},
		{name: "密码过短", username: "validuser", password: "123", wantErr: true},
		{name: "角色非法", username: "validuser", password: "secret123", role: "root", wantErr: true},
		{name: "用户名空白", username: "  ", password: "secret123", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateUser(admin.UUID, tc.username, "", tc.password, tc.role)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CreateUser() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAdminService_CreateUser_Duplicate(t *testing.T) {
	db := setupAdminUserTestDB(t)
	svc := NewAdminService(db)

	admin := models.User{Username: "admin03", PasswordHash: "hash", Role: "admin"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	if _, err := svc.CreateUser(admin.UUID, "dupuser", "", "secret123", "user"); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if _, err := svc.CreateUser(admin.UUID, "dupuser", "", "secret123", "user"); err == nil {
		t.Fatal("重复用户名应返回错误")
	}
}

func TestAdminService_CreateUser_NonAdmin(t *testing.T) {
	db := setupAdminUserTestDB(t)
	svc := NewAdminService(db)

	regUser := models.User{Username: "regular01", PasswordHash: "hash", Role: "user"}
	if err := db.Create(&regUser).Error; err != nil {
		t.Fatalf("创建普通用户失败: %v", err)
	}

	if _, err := svc.CreateUser(regUser.UUID, "victim", "", "secret123", "user"); err == nil {
		t.Fatal("非管理员调用应返回错误")
	}
}

func TestAdminService_CreateUser_ReservedName(t *testing.T) {
	db := setupAdminUserTestDB(t)
	svc := NewAdminService(db)

	admin := models.User{Username: "admin04", PasswordHash: "hash", Role: "admin"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	// 匿名用户名（GetReservedUsernames 默认保留 "anonymous"）不允许使用
	if _, err := svc.CreateUser(admin.UUID, "anonymous", "", "secret123", "user"); err == nil {
		t.Fatal("保留用户名应返回错误")
	}
}
