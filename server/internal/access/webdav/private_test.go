package webdav

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPrivateFileSystem_Operations(t *testing.T) {
	tmpDir := t.TempDir()
	fs := NewPrivateFileSystem(tmpDir)
	userUUID := "test-user-uuid"
	ctx := context.WithValue(context.Background(), ContextKeyUserUUID, userUUID)

	// Mkdir
	err := fs.Mkdir(ctx, "testdir", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	expectedPath := filepath.Join(tmpDir, "users", userUUID, "testdir")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Error("Mkdir did not create directory")
	}

	// OpenFile for writing
	f, err := fs.OpenFile(ctx, "testdir/test.txt", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	_, err = f.Write([]byte("hello private webdav"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	f.Close()

	// OpenFile for reading
	f, err = fs.OpenFile(ctx, "testdir/test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile read failed: %v", err)
	}
	buf := make([]byte, 30)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != "hello private webdav" {
		t.Errorf("Read content mismatch: got %q", string(buf[:n]))
	}
	f.Close()

	// Stat
	info, err := fs.Stat(ctx, "testdir/test.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.IsDir() {
		t.Error("test.txt should not be a directory")
	}

	// Rename
	err = fs.Rename(ctx, "testdir/test.txt", "testdir/renamed.txt")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(expectedPath, "renamed.txt")); os.IsNotExist(err) {
		t.Error("Rename did not work")
	}

	// RemoveAll
	err = fs.RemoveAll(ctx, "testdir")
	if err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Error("RemoveAll did not delete directory")
	}
}

func TestPrivateFileSystem_MissingContext(t *testing.T) {
	tmpDir := t.TempDir()
	fs := NewPrivateFileSystem(tmpDir)
	ctx := context.Background()

	if err := fs.Mkdir(ctx, "test", 0755); err == nil {
		t.Error("Mkdir should fail without user UUID in context")
	}
	if _, err := fs.OpenFile(ctx, "test.txt", os.O_RDONLY, 0); err == nil {
		t.Error("OpenFile should fail without user UUID in context")
	}
	if _, err := fs.Stat(ctx, "test.txt"); err == nil {
		t.Error("Stat should fail without user UUID in context")
	}
	if err := fs.RemoveAll(ctx, "test"); err == nil {
		t.Error("RemoveAll should fail without user UUID in context")
	}
	if err := fs.Rename(ctx, "a", "b"); err == nil {
		t.Error("Rename should fail without user UUID in context")
	}
}

func TestPrivateFileSystem_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	fs := NewPrivateFileSystem(tmpDir)
	userUUID := "test-user-uuid"
	ctx := context.WithValue(context.Background(), ContextKeyUserUUID, userUUID)

	outsideFile := filepath.Join(tmpDir, "secret.txt")
	os.WriteFile(outsideFile, []byte("secret"), 0644)

	_, err := fs.Stat(ctx, "../secret.txt")
	if err == nil {
		t.Error("Expected error for path traversal")
	}

	_, err = fs.OpenFile(ctx, "../secret.txt", os.O_RDONLY, 0)
	if err == nil {
		t.Error("Expected error for path traversal")
	}
}

func TestPrivateFileSystem_UserIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	fs := NewPrivateFileSystem(tmpDir)

	ctxA := context.WithValue(context.Background(), ContextKeyUserUUID, "user-a")
	// Create user A's directory first
	os.MkdirAll(filepath.Join(tmpDir, "users", "user-a"), 0755)
	f, err := fs.OpenFile(ctxA, "a-file.txt", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("User A OpenFile failed: %v", err)
	}
	f.Write([]byte("user a data"))
	f.Close()

	// Create user B's directory
	os.MkdirAll(filepath.Join(tmpDir, "users", "user-b"), 0755)

	ctxB := context.WithValue(context.Background(), ContextKeyUserUUID, "user-b")
	_, err = fs.Stat(ctxB, "a-file.txt")
	if err == nil {
		t.Error("User B should not see User A's file")
	}

	info, err := fs.Stat(ctxA, "a-file.txt")
	if err != nil {
		t.Fatalf("User A Stat failed: %v", err)
	}
	if info.IsDir() {
		t.Error("a-file.txt should not be a directory")
	}
}

func TestParseBasicAuth(t *testing.T) {
	tests := []struct {
		header   string
		username string
		password string
		wantErr  bool
	}{
		{"Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass")), "user", "pass", false},
		{"Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret123")), "admin", "secret123", false},
		{"", "", "", true},
		{"Invalid header", "", "", true},
		{"Basic invalid-base64!!!", "", "", true},
		{"Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), "", "", true},
	}

	for _, tt := range tests {
		username, password, err := parseBasicAuth(tt.header)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseBasicAuth(%q) expected error, got (%q, %q)", tt.header, username, password)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseBasicAuth(%q) unexpected error: %v", tt.header, err)
			continue
		}
		if username != tt.username || password != tt.password {
			t.Errorf("parseBasicAuth(%q) = (%q, %q), want (%q, %q)", tt.header, username, password, tt.username, tt.password)
		}
	}
}

func TestAuthCache(t *testing.T) {
	cache := newAuthCache(5*time.Minute, 100)

	cache.set("key1", &authCacheEntry{userUUID: "uuid-1"})
	entry, ok := cache.get("key1")
	if !ok {
		t.Error("Expected to find key1 in cache")
	}
	if entry.userUUID != "uuid-1" {
		t.Errorf("Expected uuid-1, got %s", entry.userUUID)
	}

	cache.set("expired", &authCacheEntry{
		userUUID:  "expired-uuid",
		expiresAt: time.Now().Add(-1 * time.Minute),
	})
	_, ok = cache.get("expired")
	if ok {
		t.Error("Expired entry should not be found")
	}

	_, ok = cache.get("nonexistent")
	if ok {
		t.Error("Nonexistent key should not be found")
	}
}

func TestAuthCacheEviction(t *testing.T) {
	cache := newAuthCache(5*time.Minute, 2)

	cache.set("a", &authCacheEntry{userUUID: "uuid-a"})
	cache.set("b", &authCacheEntry{userUUID: "uuid-b"})
	cache.set("c", &authCacheEntry{userUUID: "uuid-c"})

	_, okA := cache.get("a")
	_, okB := cache.get("b")
	_, okC := cache.get("c")

	if !okC {
		t.Error("Newest entry should survive")
	}
	if okA && okB {
		t.Error("At least one old entry should have been evicted")
	}
}

func TestBasicAuthMiddleware_Success(t *testing.T) {
	authFunc := UserAuthenticator(func(username, password string) (string, bool, error) {
		if username == "testuser" && password == "testpass" {
			return "test-uuid", false, nil
		}
		return "", false, errUnauthorized
	})

	middleware := BasicAuthMiddleware(authFunc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/webdav/private/", nil)
	c.Request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("testuser:testpass")))

	middleware(c)

	if uuid, ok := c.Request.Context().Value(ContextKeyUserUUID).(string); !ok || uuid != "test-uuid" {
		t.Errorf("Expected test-uuid in context, got %v", uuid)
	}
}

func TestBasicAuthMiddleware_InvalidCredentials(t *testing.T) {
	authFunc := UserAuthenticator(func(username, password string) (string, bool, error) {
		return "", false, errUnauthorized
	})

	middleware := BasicAuthMiddleware(authFunc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/webdav/private/", nil)
	c.Request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wrong:creds")))

	middleware(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
	}
	authHeader := w.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(authHeader, "Basic") {
		t.Errorf("Expected WWW-Authenticate header to start with Basic, got %q", authHeader)
	}
}

func TestBasicAuthMiddleware_DisabledUser(t *testing.T) {
	authFunc := UserAuthenticator(func(username, password string) (string, bool, error) {
		if username == "disabled" && password == "pass" {
			return "disabled-uuid", true, nil
		}
		return "", false, errUnauthorized
	})

	middleware := BasicAuthMiddleware(authFunc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/webdav/private/", nil)
	c.Request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("disabled:pass")))

	middleware(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestBasicAuthMiddleware_NoHeader(t *testing.T) {
	authFunc := UserAuthenticator(func(username, password string) (string, bool, error) {
		return "", false, errUnauthorized
	})

	middleware := BasicAuthMiddleware(authFunc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/webdav/private/", nil)

	middleware(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
	}
}
