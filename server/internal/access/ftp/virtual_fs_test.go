package ftp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fuzhan/internal/appconfig"
)

// stubBackend 测试用 FtpBackend 桩：权限/索引/配额行为由测试用例控制。
type stubBackend struct {
	adminUUID     string
	uploaderIPs   map[string]string // key: rootName + relPath → uploader IP
	uploadEnabled bool
	diskFull      bool
}

func (s *stubBackend) IsAdmin(userUUID string) bool {
	return userUUID != "" && userUUID == s.adminUUID
}

func (s *stubBackend) PublicUploaderIP(rootName, relPath string) string {
	if s.uploaderIPs == nil {
		return ""
	}
	return s.uploaderIPs[rootName+relPath]
}

func (s *stubBackend) UploadEnabled() bool { return s.uploadEnabled }

func (s *stubBackend) CheckDiskSpace(_ string, _ int64) error {
	if s.diskFull {
		return errors.New("磁盘空间不足")
	}
	return nil
}

func (s *stubBackend) CheckPrivateQuota(_ string, _ int64) error { return nil }

func (s *stubBackend) SyncPublicFile(_, _, _ string)                {}
func (s *stubBackend) RemovePublicFile(_, _ string)                 {}
func (s *stubBackend) MovePublicFile(_, _, _ string)                {}
func (s *stubBackend) AddPrivateFile(_, _, _ string, _ int64) error { return nil }
func (s *stubBackend) SoftDeletePrivateFile(_, _ string) error      { return nil }
func (s *stubBackend) RenamePrivateFile(_, _, _ string) error       { return nil }

// newTestFs 构造测试用 MultiRootFs：rootDir 作为名为 "files" 的共享根目录。
func newTestFs(t *testing.T, rootDir, userUUID, clientIP string, backend FtpBackend) *MultiRootFs {
	t.Helper()
	appconfig.GlobalConfig = appconfig.Config{}
	return NewMultiRootFs(
		map[string]string{"files": rootDir},
		"", userUUID, clientIP, nil, backend,
	)
}

// writeRootFile 在共享根目录下真实创建文件，模拟已有文件。
func writeRootFile(t *testing.T, rootDir, name, content string) {
	t.Helper()
	p := filepath.Join(rootDir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAnonymousCannotModifyPublic(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	fs := newTestFs(t, rootDir, "", "", &stubBackend{uploadEnabled: true})

	if err := fs.Remove("public/files/a.txt"); err != os.ErrPermission {
		t.Fatalf("匿名删除应被拒绝, got %v", err)
	}
	if err := fs.Rename("public/files/a.txt", "public/files/b.txt"); err != os.ErrPermission {
		t.Fatalf("匿名重命名应被拒绝, got %v", err)
	}
	f, err := fs.Create("public/files/new.txt")
	if err != nil {
		t.Fatalf("匿名新建文件应允许, got %v", err)
	}
	_ = f.Close()
}

func TestAnonymousDownloadRateLimited(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	// 匿名用户（userUUID 空、真实 clientIP），下载走免认证面
	fs := NewMultiRootFs(map[string]string{"files": rootDir}, "", "", "9.9.9.9", nil, &stubBackend{})
	appconfig.GlobalConfig.Download.RateLimit = appconfig.DownloadRateLimitConfig{WindowMinutes: 1, MaxRequests: 1}
	defer func() { appconfig.GlobalConfig = appconfig.Config{} }()

	if f, err := fs.Open("public/files/a.txt"); err != nil {
		t.Fatalf("首次匿名下载应放行, got %v", err)
	} else {
		f.Close()
	}
	if _, err := fs.Open("public/files/a.txt"); err != os.ErrPermission {
		t.Fatalf("超阈值匿名下载应被拒绝, got %v", err)
	}
}

func TestAuthenticatedDownloadNotRateLimited(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	// 认证用户下载走登录态，不受匿名下载限流约束
	fs := NewMultiRootFs(map[string]string{"files": rootDir}, "", "uuid-owner", "1.2.3.4", nil, &stubBackend{})
	appconfig.GlobalConfig.Download.RateLimit = appconfig.DownloadRateLimitConfig{WindowMinutes: 1, MaxRequests: 1}
	defer func() { appconfig.GlobalConfig = appconfig.Config{} }()

	for i := 0; i < 3; i++ {
		if f, err := fs.Open("public/files/a.txt"); err != nil {
			t.Fatalf("认证用户下载不应被限流(第 %d 次), got %v", i+1, err)
		} else {
			f.Close()
		}
	}
}

func TestUploaderIPCanManagePublic(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	backend := &stubBackend{uploadEnabled: true, uploaderIPs: map[string]string{"files/a.txt": "1.2.3.4"}}
	fs := newTestFs(t, rootDir, "uuid-1", "1.2.3.4", backend)

	if err := fs.Remove("public/files/a.txt"); err != nil {
		t.Fatalf("上传者IP一致应允许删除, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("文件应已被删除")
	}
}

func TestDifferentIPCannotManagePublic(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	backend := &stubBackend{uploadEnabled: true, uploaderIPs: map[string]string{"files/a.txt": "1.2.3.4"}}
	fs := newTestFs(t, rootDir, "uuid-1", "9.9.9.9", backend)

	if err := fs.Remove("public/files/a.txt"); err != os.ErrPermission {
		t.Fatalf("IP不一致应拒绝删除, got %v", err)
	}
}

func TestAdminCanManagePublic(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	backend := &stubBackend{adminUUID: "uuid-admin", uploadEnabled: true}
	fs := newTestFs(t, rootDir, "uuid-admin", "9.9.9.9", backend)

	if err := fs.Remove("public/files/a.txt"); err != nil {
		t.Fatalf("管理员应允许删除, got %v", err)
	}
}

func TestUploadDisabledBlocksPublicWrite(t *testing.T) {
	rootDir := t.TempDir()
	fs := newTestFs(t, rootDir, "", "", &stubBackend{uploadEnabled: false})

	if _, err := fs.Create("public/files/new.txt"); err != os.ErrPermission {
		t.Fatalf("上传总开关关闭时应拒绝新建, got %v", err)
	}
}

func TestPrivateHiddenForAnonymous(t *testing.T) {
	rootDir := t.TempDir()
	fs := newTestFs(t, rootDir, "", "", &stubBackend{uploadEnabled: true})

	if _, err := fs.Stat("private"); err != os.ErrPermission {
		t.Fatalf("匿名用户不应看到 private, got %v", err)
	}
	if _, err := fs.Open("private"); err != os.ErrPermission {
		t.Fatalf("匿名用户不应打开 private, got %v", err)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	rootDir := t.TempDir()
	fs := newTestFs(t, rootDir, "", "", &stubBackend{uploadEnabled: true})

	for _, p := range []string{
		"public/files/../secret.txt",
		"public/files/..%2fsecret.txt",
		`public/files/..\..\secret.txt`,
		"private/../../secret.txt",
	} {
		if _, err := fs.Open(p); err == nil {
			t.Fatalf("路径穿越应被拒绝: %s", p)
		}
	}
}

func TestSizeLimitedFileTracksSeek(t *testing.T) {
	real, err := os.CreateTemp(t.TempDir(), "limit")
	if err != nil {
		t.Fatal(err)
	}
	defer real.Close()

	f := &sizeLimitedFile{File: real, maxSize: 10}

	if _, err := f.Write([]byte("12345")); err != nil {
		t.Fatalf("写入5字节应成功, got %v", err)
	}
	if _, err := f.Write([]byte("123456")); err == nil {
		t.Fatal("超过上限的写入应失败")
	}

	// REST 续传：Seek 到大偏移后写入少量数据也不应绕过上限
	if _, err := f.Seek(9, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); err != nil {
		t.Fatalf("offset 9 写入1字节(10)应成功, got %v", err)
	}
	if _, err := f.Write([]byte("y")); err == nil {
		t.Fatal("offset 10 写入1字节(11)应失败")
	}

	// WriteAt 指定偏移超过上限
	if _, err := f.WriteAt([]byte("z"), 10); err == nil {
		t.Fatal("WriteAt 偏移超过上限应失败")
	}
}
