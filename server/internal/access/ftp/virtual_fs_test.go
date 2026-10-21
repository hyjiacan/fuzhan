package ftp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/security"
)

// secEventSink 内存安全事件存储，捕获被测代码经 security.Record 提交的事件
type secEventSink struct {
	repositories.SecurityEventStore
	mu     sync.Mutex
	events []*models.SecurityEvent
}

func (s *secEventSink) CreateBatch(evs []*models.SecurityEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evs...)
	return nil
}

// startSecCapture 初始化全局安全事件写入器，返回「停止并取回事件」函数
func startSecCapture(t *testing.T) func() []*models.SecurityEvent {
	t.Helper()
	sink := &secEventSink{}
	security.InitWriter(sink, 1, 256)
	return func() []*models.SecurityEvent {
		security.StopWriter()
		sink.mu.Lock()
		defer sink.mu.Unlock()
		out := make([]*models.SecurityEvent, len(sink.events))
		copy(out, sink.events)
		return out
	}
}

func findEvent(events []*models.SecurityEvent, eventType string) *models.SecurityEvent {
	for _, e := range events {
		if e.EventType == eventType {
			return e
		}
	}
	return nil
}

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

func TestPathTraversalRecordsSecurityEvents(t *testing.T) {
	rootDir := t.TempDir()
	capture := startSecCapture(t)
	fs := newTestFs(t, rootDir, "", "1.2.3.4", &stubBackend{uploadEnabled: true})

	// 反斜杠穿越 / 上级目录穿越 / 未知命名空间，各应记一条高危 access.forbidden
	if _, err := fs.Open(`public/files/..\..\secret.txt`); err == nil {
		t.Fatal("反斜杠穿越应被拒绝")
	}
	if _, err := fs.Open("private/../../secret.txt"); err == nil {
		t.Fatal("上级目录穿越应被拒绝")
	}
	if _, err := fs.Stat("unknown/x.txt"); err == nil {
		t.Fatal("未知命名空间应被拒绝")
	}
	events := capture()
	if len(events) != 3 {
		t.Fatalf("3 个穿越/越权拒绝点应各入库 1 条，got %d 条：%+v", len(events), events)
	}
	for _, ev := range events {
		if ev.EventType != models.EventAccessForbidden || ev.Severity != models.SeverityHigh ||
			ev.Guard != models.GuardFTP || ev.ClientIP != "1.2.3.4" || ev.Identity != "anonymous" {
			t.Fatalf("事件维度异常：%+v", ev)
		}
	}
	if !strings.Contains(events[0].Reason, "反斜杠") {
		t.Errorf("第 1 条应注明反斜杠穿越，got %q", events[0].Reason)
	}
	if !strings.Contains(events[1].Reason, "上级目录") {
		t.Errorf("第 2 条应注明上级目录穿越，got %q", events[1].Reason)
	}
	if !strings.Contains(events[2].Reason, "未知命名空间") {
		t.Errorf("第 3 条应注明未知命名空间，got %q", events[2].Reason)
	}
	if events[0].Operation != "file.open" || events[2].Operation != "file.stat" {
		t.Errorf("Operation 应携带调用方操作名，got %q / %q", events[0].Operation, events[2].Operation)
	}
}

func TestAnonymousManagePublicRecordsEvent(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	capture := startSecCapture(t)
	fs := newTestFs(t, rootDir, "", "9.9.9.9", &stubBackend{uploadEnabled: true})

	if err := fs.Remove("public/files/a.txt"); err != os.ErrPermission {
		t.Fatalf("匿名删除公开文件应被拒绝, got %v", err)
	}
	events := capture()
	ev := findEvent(events, models.EventAccessForbidden)
	if ev == nil {
		t.Fatalf("越权管理应入库 access.forbidden，got %+v", events)
	}
	if ev.Severity != models.SeverityMedium || ev.Guard != models.GuardFTP || ev.Operation != "file.remove" {
		t.Fatalf("事件维度异常：%+v", ev)
	}
	if !strings.Contains(ev.Reason, "管理公开文件") {
		t.Errorf("Reason 应说明越权管理公开文件，got %q", ev.Reason)
	}
}

func TestRenameCrossNamespaceRecordsEvent(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	capture := startSecCapture(t)
	fs := newTestFs(t, rootDir, "uuid-1", "1.2.3.4", &stubBackend{uploadEnabled: true})

	if err := fs.Rename("public/files/a.txt", "private/b.txt"); err != os.ErrPermission {
		t.Fatalf("跨命名空间重命名应被拒绝, got %v", err)
	}
	events := capture()
	ev := findEvent(events, models.EventAccessForbidden)
	if ev == nil {
		t.Fatalf("跨命名空间重命名应入库，got %+v", events)
	}
	if ev.Severity != models.SeverityHigh || !strings.Contains(ev.Reason, "跨命名空间") {
		t.Fatalf("应为高危跨命名空间事件：%+v", ev)
	}
	if ev.Identity != "uuid-1" {
		t.Errorf("认证用户事件 Identity 应记录 UUID，got %q", ev.Identity)
	}
}

func TestAnonymousDownloadLimitedRecordsEvent(t *testing.T) {
	rootDir := t.TempDir()
	writeRootFile(t, rootDir, "a.txt", "hello")
	capture := startSecCapture(t)
	fs := NewMultiRootFs(map[string]string{"files": rootDir}, "", "", "8.8.8.8", nil, &stubBackend{})
	appconfig.GlobalConfig.Download.RateLimit = appconfig.DownloadRateLimitConfig{WindowMinutes: 1, MaxRequests: 1}
	defer func() { appconfig.GlobalConfig = appconfig.Config{} }()

	if f, err := fs.Open("public/files/a.txt"); err == nil {
		f.Close()
	} else {
		t.Fatalf("首次匿名下载应放行, got %v", err)
	}
	if _, err := fs.Open("public/files/a.txt"); err != os.ErrPermission {
		t.Fatalf("超阈值匿名下载应被拒绝, got %v", err)
	}
	events := capture()
	ev := findEvent(events, models.EventRateLimited)
	if ev == nil {
		t.Fatalf("匿名下载限流应入库 rate.limited，got %+v", events)
	}
	if ev.Severity != models.SeverityLow || ev.Guard != models.GuardFTP || ev.Operation != "file.download" {
		t.Fatalf("事件维度异常：%+v", ev)
	}
}
