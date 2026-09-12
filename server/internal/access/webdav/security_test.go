package webdav

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"fuzhan/internal/accessguard"
	"fuzhan/internal/appconfig"

	"github.com/gin-gonic/gin"
)

func pubFS(t *testing.T) *PublicFileSystem {
	t.Helper()
	root1 := filepath.Join(t.TempDir(), "share1")
	os.MkdirAll(root1, 0755)
	return NewPublicFileSystem(map[string]string{"share1": root1})
}

func TestPublicFileSystem_UploadDisabled(t *testing.T) {
	fs := pubFS(t)
	fs.SetBackend(&stubBackend{uploadEnabled: false})
	ctx := context.Background()
	if _, err := fs.OpenFile(ctx, "/share1/f.txt", os.O_CREATE|os.O_WRONLY, 0644); err != os.ErrPermission {
		t.Errorf("上传开关关闭时新建应返回 ErrPermission，got %v", err)
	}
	// 只读打开不受上传开关影响
	if _, err := fs.OpenFile(ctx, "/share1/f.txt", os.O_RDONLY, 0); err == nil {
		t.Error("文件不存在时只读打开应失败")
	}
}

func TestPublicFileSystem_RejectsDisallowedExtension(t *testing.T) {
	fs := pubFS(t)
	fs.SetBackend(&stubBackend{uploadEnabled: true, disallowExt: ".exe"})
	ctx := context.Background()
	if _, err := fs.OpenFile(ctx, "/share1/evil.exe", os.O_CREATE|os.O_WRONLY, 0644); err != os.ErrPermission {
		t.Errorf("扩展名不在白名单时应返回 ErrPermission，got %v", err)
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "evil.exe")); err == nil {
		t.Error("被拒绝的文件不应落盘")
	}
	// 白名单内扩展名正常放行
	okF, err := fs.OpenFile(ctx, "/share1/ok.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Errorf("白名单内扩展名应放行，got %v", err)
	}
	okF.Close()
}

func TestPublicFileSystem_DiskFullBlocksCreate(t *testing.T) {
	fs := pubFS(t)
	fs.SetBackend(&stubBackend{uploadEnabled: true, diskBlock: true})
	ctx := context.Background()
	if _, err := fs.OpenFile(ctx, "/share1/f.txt", os.O_CREATE|os.O_WRONLY, 0644); err != os.ErrPermission {
		t.Errorf("磁盘空间不足时新建应返回 ErrPermission，got %v", err)
	}
}

func TestPublicFileSystem_SyncIndexedOnClose(t *testing.T) {
	fs := pubFS(t)
	stub := &stubBackend{uploadEnabled: true}
	fs.SetBackend(stub)

	ctx := context.WithValue(context.Background(), ContextKeyClientIP, "203.0.113.9")
	f, err := fs.OpenFile(ctx, "/share1/upload.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if _, err := f.Write([]byte("payload")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if stub.syncRoot != "" || stub.syncRel != "" {
		t.Error("未关闭前不应触发索引同步")
	}
	f.Close()

	if stub.syncRoot != "share1" || stub.syncRel != "upload.txt" {
		t.Errorf("Close 后应同步索引，got root=%q rel=%q", stub.syncRoot, stub.syncRel)
	}
	if stub.syncIP != "203.0.113.9" {
		t.Errorf("应记录上传者 IP，got %q", stub.syncIP)
	}
}

func TestPrivateFileSystem_QuotaBlocksCreate(t *testing.T) {
	fs := NewPrivateFileSystem(t.TempDir())
	fs.SetBackend(&stubBackend{quotaBlock: true})
	ctx := context.WithValue(context.Background(), ContextKeyUserUUID, "u-quota")
	if _, err := fs.OpenFile(ctx, "/f.txt", os.O_CREATE|os.O_WRONLY, 0644); err != os.ErrPermission {
		t.Errorf("配额超限时新建应返回 ErrPermission，got %v", err)
	}
}

func TestPrivateFileSystem_DiskBlocksCreate(t *testing.T) {
	fs := NewPrivateFileSystem(t.TempDir())
	fs.SetBackend(&stubBackend{diskBlock: true})
	ctx := context.WithValue(context.Background(), ContextKeyUserUUID, "u-disk")
	if _, err := fs.OpenFile(ctx, "/f.txt", os.O_CREATE|os.O_WRONLY, 0644); err != os.ErrPermission {
		t.Errorf("磁盘空间不足时新建应返回 ErrPermission，got %v", err)
	}
}

func TestPrivateFileSystem_SyncPrivateOnClose(t *testing.T) {
	fs := NewPrivateFileSystem(t.TempDir())
	stub := &stubBackend{}
	fs.SetBackend(stub)
	ctx := context.WithValue(context.Background(), ContextKeyUserUUID, "u-owner")

	f, err := fs.OpenFile(ctx, "/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	f.Close()

	if len(stub.privateAdded) != 1 {
		t.Fatalf("Close 后应写入 1 条私有记录，got %d", len(stub.privateAdded))
	}
	pa := stub.privateAdded[0]
	if pa.userUUID != "u-owner" || pa.relPath != "/file.txt" || pa.fileName != "file.txt" || pa.size != 5 {
		t.Errorf("私有记录字段异常：%+v", pa)
	}
}

func TestOptionalBasicAuthFailLock(t *testing.T) {
	authFunc := UserAuthenticator(func(username, password string) (string, bool, error) {
		return "", false, errUnauthorized
	})

	// 低频阈值：2 次失败即锁定，用于快速验证
	appconfig.LockConfig()
	old := appconfig.GlobalConfig.Download.RateLimit
	appconfig.GlobalConfig.Download.RateLimit = appconfig.DownloadRateLimitConfig{
		WindowMinutes: 1, MaxRequests: 1000, LockAfter: 2, LockMinutes: 10,
	}
	appconfig.UnlockConfig()
	defer func() {
		appconfig.LockConfig()
		appconfig.GlobalConfig.Download.RateLimit = old
		appconfig.UnlockConfig()
	}()

	// 用独立 IP 避免影响其它用例
	badAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("wrong:creds"))
	mw := OptionalBasicAuthMiddleware(authFunc)

	do := func() int {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/webdav/", nil)
		c.Request.RemoteAddr = "198.51.100.7:5555"
		c.Request.Header.Set("Authorization", badAuth)
		mw(c)
		return w.Code
	}

	if code := do(); code != http.StatusUnauthorized {
		t.Fatalf("第 1 次失败应为 401，got %d", code)
	}
	if code := do(); code != http.StatusUnauthorized {
		t.Fatalf("第 2 次失败应为 401，got %d", code)
	}
	// 达到阈值后该 IP 被锁定
	if !accessguard.IsLocked(accessguard.SCOPE_WEBDAV, "198.51.100.7") {
		t.Fatal("达到 lock_after 阈值后应锁定该 IP")
	}
	if code := do(); code != http.StatusTooManyRequests {
		t.Fatalf("锁定期间应返回 429，got %d", code)
	}
}
