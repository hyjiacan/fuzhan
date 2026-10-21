package ftp

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/security"
	"fuzhan/internal/utils"
	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// secSink 内存安全事件存储，捕获 AuthUser 提交的安全事件
type secSink struct {
	repositories.SecurityEventStore
	mu     sync.Mutex
	events []*models.SecurityEvent
}

func (s *secSink) CreateBatch(evs []*models.SecurityEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evs...)
	return nil
}

// fakeClientConn 最小化 ftpserver.ClientContext 桩：AuthUser 仅用到 RemoteAddr
type fakeClientConn struct {
	ftpserver.ClientContext
	addr net.Addr
}

func (f *fakeClientConn) RemoteAddr() net.Addr { return f.addr }

func TestStripPort(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:5678":  "1.2.3.4",
		"1.2.3.4":       "1.2.3.4",
		"[::1]:21":      "::1",
		"203.0.113.9:0": "203.0.113.9",
	}
	for in, want := range cases {
		if got := stripPort(in); got != want {
			t.Errorf("stripPort(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestAuthUserSecurityEvents(t *testing.T) {
	oldFail, oldLimit := loginFailDelay, loginRateLimitDelay
	loginFailDelay, loginRateLimitDelay = time.Duration(0), time.Duration(0)
	defer func() { loginFailDelay, loginRateLimitDelay = oldFail, oldLimit }()

	sink := &secSink{}
	security.InitWriter(sink, 1, 256)

	d := &ftpDriver{
		failCounts: make(map[string]*loginFailEntry),
		authUser:   func(username, password string) (string, error) { return "", errors.New("密码错误") },
	}
	cc := &fakeClientConn{addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 54321}}

	for i := 0; i < 6; i++ {
		if _, err := d.AuthUser(cc, "bob", "bad"); err == nil {
			t.Fatalf("第 %d 次错误密码认证应失败", i+1)
		}
	}
	security.StopWriter()

	var failed, locked int
	for _, ev := range sink.events {
		if ev.Guard != models.GuardFTP || ev.ClientIP != "203.0.113.5" {
			t.Fatalf("事件 guard/ClientIP 异常（IP 应已去端口）：%+v", ev)
		}
		switch ev.EventType {
		case models.EventAuthFailedLogin:
			failed++
			if ev.Severity != models.SeverityMedium || ev.Operation != "auth.login" || ev.Identity != "bob" {
				t.Fatalf("failed_login 维度异常：%+v", ev)
			}
		case models.EventAuthLocked:
			locked++
			if ev.Severity != models.SeverityHigh || ev.Operation != "auth.login" {
				t.Fatalf("locked 维度异常：%+v", ev)
			}
		default:
			t.Fatalf("出现预期外事件类型：%+v", ev)
		}
	}
	if failed != 5 {
		t.Errorf("6 次错误登录应记录 5 条 failed_login（第 6 次被频率拦截），got %d", failed)
	}
	if locked != 1 {
		t.Errorf("跨越阈值仅应记录 1 条 locked，got %d", locked)
	}
}

func ftpTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.FileRecordPrivate{}); err != nil {
		t.Fatal(err)
	}
	appconfig.SetDB(db)
	return db
}

func TestFtpBackend_IsAdmin(t *testing.T) {
	db := ftpTestDB(t)
	b := NewFtpBackend(db, nil, "")

	now := utils.Now()
	users := []models.User{
		{UUID: "u-admin", Username: "admin1", Role: "admin", CreatedAt: now, UpdatedAt: now},
		{UUID: "u-user", Username: "user1", Role: "user", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}

	if !b.IsAdmin("u-admin") {
		t.Error("admin 用户应判定为管理员")
	}
	if b.IsAdmin("u-user") {
		t.Error("普通用户不应判定为管理员")
	}
	if b.IsAdmin("") || b.IsAdmin("u-nonexist") {
		t.Error("匿名/未知用户不应判定为管理员")
	}
}

func TestFtpBackend_AddPrivateFile_QuotaExceeded(t *testing.T) {
	db := ftpTestDB(t)
	privateDir := t.TempDir()
	b := NewFtpBackend(db, nil, privateDir)

	appconfig.GlobalConfig = appconfig.Config{
		Storage: appconfig.StorageConfig{
			Private: appconfig.PrivateStorageConfig{
				Enabled:      true,
				Path:         privateDir,
				PerUserQuota: 100,
			},
		},
	}

	// 已有记录占用 80 字节
	now := utils.Now()
	existing := models.FileRecordPrivate{
		FileRecordBase: models.FileRecordBase{
			FileName:     "old.bin",
			FilePath:     "/old.bin",
			RootName:     "private",
			FullPath:     "/private/old.bin",
			FileSize:     80,
			IsDir:        false,
			HashStatus:   "done",
			ModTime:      now,
			LastSyncedAt: now,
			Status:       models.FileStatusActive,
			OwnerID:      "u-user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	// 落盘 30 字节的新文件，配额 100-80=20，应拒绝并回滚删除
	relPath := "/big.bin"
	p := filepath.Join(privateDir, "users", "u-user", strings.TrimPrefix(relPath, "/"))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, 30), 0644); err != nil {
		t.Fatal(err)
	}

	err := b.AddPrivateFile("u-user", relPath, "big.bin", 30)
	if err == nil {
		t.Fatal("超出配额应返回错误")
	}
	if _, serr := os.Stat(p); !os.IsNotExist(serr) {
		t.Fatal("配额超限后已落盘文件应被删除")
	}
	var count int64
	if err := db.Model(&models.FileRecordPrivate{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("不应写入新的私有记录, count=%d", count)
	}
}

func TestFtpBackend_AddPrivateFile_Ok(t *testing.T) {
	db := ftpTestDB(t)
	privateDir := t.TempDir()
	b := NewFtpBackend(db, nil, privateDir)

	appconfig.GlobalConfig = appconfig.Config{
		Storage: appconfig.StorageConfig{
			Private: appconfig.PrivateStorageConfig{
				Enabled: true,
				Path:    privateDir,
				// PerUserQuota 默认 0 = 不限制
			},
		},
	}

	relPath := "/sub/a.txt"
	p := filepath.Join(privateDir, "users", "u-user", "sub", "a.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := b.AddPrivateFile("u-user", relPath, "a.txt", 4); err != nil {
		t.Fatalf("AddPrivateFile 应成功, got %v", err)
	}

	var rec models.FileRecordPrivate
	if err := db.Where("owner_id = ? AND file_path = ?", "u-user", relPath).First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	if rec.ShareCode == "" || len(rec.ShareCode) != 32 {
		t.Fatalf("分享码应为32位hex, got %q", rec.ShareCode)
	}
	if rec.FileSize != 4 || rec.HashStatus != "pending" {
		t.Fatalf("记录字段不正确: size=%d hash=%s", rec.FileSize, rec.HashStatus)
	}
	if _, serr := os.Stat(p); serr != nil {
		t.Fatal("成功上传后文件应保留")
	}
}

func TestFtpBackend_SoftDeleteAndRenamePrivate(t *testing.T) {
	db := ftpTestDB(t)
	b := NewFtpBackend(db, nil, t.TempDir())

	now := utils.Now()
	rec := models.FileRecordPrivate{
		FileRecordBase: models.FileRecordBase{
			FileName:     "a.txt",
			FilePath:     "/a.txt",
			RootName:     "private",
			FullPath:     "/private/a.txt",
			FileSize:     1,
			HashStatus:   "done",
			ModTime:      now,
			LastSyncedAt: now,
			Status:       models.FileStatusActive,
			OwnerID:      "u-user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}

	if err := b.RenamePrivateFile("u-user", "/a.txt", "/b.txt"); err != nil {
		t.Fatalf("重命名记录失败: %v", err)
	}
	var renamed models.FileRecordPrivate
	if err := db.Where("owner_id = ? AND file_path = ?", "u-user", "/b.txt").First(&renamed).Error; err != nil {
		t.Fatalf("重命名后应能按新路径查到: %v", err)
	}
	if renamed.FullPath != "/private/b.txt" {
		t.Fatalf("FullPath 应同步更新, got %s", renamed.FullPath)
	}

	if err := b.SoftDeletePrivateFile("u-user", "/b.txt"); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	var count int64
	if err := db.Model(&models.FileRecordPrivate{}).
		Where("owner_id = ? AND status = ?", "u-user", models.FileStatusActive).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("软删除后不应有活跃记录, count=%d", count)
	}
}
