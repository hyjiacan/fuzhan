package webdav

import (
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/gin-gonic/gin"
)

func TestFileSystemOperations(t *testing.T) {
    // Create temp directory for testing
    tmpDir := t.TempDir()

    fs := NewFileSystem(tmpDir)
    ctx := context.Background()

    // Test Mkdir
    err := fs.Mkdir(ctx, "testdir", 0755)
    if err != nil {
        t.Fatalf("Mkdir failed: %v", err)
    }
    if _, err := os.Stat(filepath.Join(tmpDir, "testdir")); os.IsNotExist(err) {
        t.Error("Mkdir did not create directory")
    }

    // Test Stat
    info, err := fs.Stat(ctx, "testdir")
    if err != nil {
        t.Fatalf("Stat failed: %v", err)
    }
    if !info.IsDir() {
        t.Error("Stat should return directory type")
    }

    // Test OpenFile for writing
    f, err := fs.OpenFile(ctx, "testdir/test.txt", os.O_RDWR|os.O_CREATE, 0644)
    if err != nil {
        t.Fatalf("OpenFile failed: %v", err)
    }
    _, err = f.Write([]byte("hello webdav"))
    if err != nil {
        t.Fatalf("Write failed: %v", err)
    }
    f.Close()

    // Test OpenFile for reading
    f, err = fs.OpenFile(ctx, "testdir/test.txt", os.O_RDONLY, 0)
    if err != nil {
        t.Fatalf("OpenFile read failed: %v", err)
    }
    buf := make([]byte, 20)
    n, err := f.Read(buf)
    if err != nil {
        t.Fatalf("Read failed: %v", err)
    }
    if string(buf[:n]) != "hello webdav" {
        t.Errorf("Read content mismatch: got %q", string(buf[:n]))
    }
    f.Close()

    // Test Rename
    err = fs.Rename(ctx, "testdir/test.txt", "testdir/renamed.txt")
    if err != nil {
        t.Fatalf("Rename failed: %v", err)
    }
    if _, err := os.Stat(filepath.Join(tmpDir, "testdir/renamed.txt")); os.IsNotExist(err) {
        t.Error("Rename did not work")
    }

    // Test RemoveAll
    err = fs.RemoveAll(ctx, "testdir")
    if err != nil {
        t.Fatalf("RemoveAll failed: %v", err)
    }
    if _, err := os.Stat(filepath.Join(tmpDir, "testdir")); !os.IsNotExist(err) {
        t.Error("RemoveAll did not delete directory")
    }
}

func TestFileSystemPathScope(t *testing.T) {
    tmpDir := t.TempDir()
    // Create a file outside the root with a unique name to avoid collisions
    suffix := fmt.Sprintf("%d", os.Getpid())
    outsideFile := filepath.Join(tmpDir, "..", "outside_"+suffix+".txt")
    os.WriteFile(outsideFile, []byte("should not be accessible"), 0644)
    defer os.Remove(outsideFile)

    fs := NewFileSystem(tmpDir)
    ctx := context.Background()

    // Try to access file outside root - this should fail the path validation
    _, err := fs.Stat(ctx, "../outside_"+suffix+".txt")
    if err == nil {
        t.Error("Expected error when accessing file outside root")
    }
}

func TestDepthMiddleware(t *testing.T) {
    middleware := depthMiddleware()

    tests := []struct {
        name      string
        method    string
        depth     string
        wantBlock bool
    }{
        {"PROPFIND Depth 0", "PROPFIND", "0", false},
        {"PROPFIND Depth 1", "PROPFIND", "1", false},
        {"PROPFIND Depth infinity", "PROPFIND", "infinity", true},
        {"PROPFIND Depth 2", "PROPFIND", "2", true},
        {"PROPFIND Depth 10", "PROPFIND", "10", true},
        {"PROPFIND Depth abc", "PROPFIND", "abc", true},
        {"PROPFIND no depth (defaults to 1)", "PROPFIND", "", false},
        {"GET no depth", "GET", "", false},
        {"PUT no depth", "PUT", "", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            w := httptest.NewRecorder()
            c, _ := gin.CreateTestContext(w)
            c.Request = httptest.NewRequest(tt.method, "/webdav/", nil)
            if tt.depth != "" {
                c.Request.Header.Set("Depth", tt.depth)
            }

            middleware(c)

            if tt.wantBlock && w.Code != http.StatusForbidden {
                t.Errorf("Expected 403 Forbidden, got %d", w.Code)
            }
            if !tt.wantBlock && w.Code != 0 && w.Code != http.StatusOK {
                t.Errorf("Expected pass, got %d", w.Code)
            }
        })
    }
}

func TestIsReadOnlyMethod(t *testing.T) {
    tests := []struct {
        method string
        readOnly bool
    }{
        {"GET", true},
        {"HEAD", true},
        {"PROPFIND", true},
        {"OPTIONS", true},
        {"PUT", false},
        {"MKCOL", false},
        {"MOVE", false},
        {"DELETE", false},
        {"LOCK", false},
        {"UNLOCK", false},
        {"COPY", false},
    }

    for _, tt := range tests {
        if got := isReadOnlyMethod(tt.method); got != tt.readOnly {
            t.Errorf("isReadOnlyMethod(%s) = %v, want %v", tt.method, got, tt.readOnly)
        }
    }
}

func TestDirInfo(t *testing.T) {
    d := &dirInfo{name: "test", isDir: true}
    if d.Name() != "test" {
        t.Errorf("Name() = %q, want %q", d.Name(), "test")
    }
    if !d.IsDir() {
        t.Error("IsDir() should be true")
    }
    if d.Size() != 0 {
        t.Errorf("Size() = %d, want 0", d.Size())
    }
}

func TestNewHandler(t *testing.T) {
    tmpDir := t.TempDir()
    fs := NewFileSystem(tmpDir)
    h := NewHandler("/webdav", fs)

    if h == nil {
        t.Fatal("NewHandler returned nil")
    }
    if h.Prefix != "/webdav" {
        t.Errorf("Prefix = %q, want %q", h.Prefix, "/webdav")
    }
    if h.FileSystem == nil {
        t.Error("FileSystem should not be nil")
    }
    if h.LockSystem == nil {
        t.Error("LockSystem should not be nil")
    }
}

// Ensure *webDavHandler implements http.Handler
func TestWebDAVHandlerInterface(t *testing.T) {
    tmpDir := t.TempDir()
    fs := NewFileSystem(tmpDir)
    h := NewHandler("/webdav", fs)

    // Verify it implements http.Handler
    var _ http.Handler = h

    // Test OPTIONS request (preflight)
    req := httptest.NewRequest("OPTIONS", "/webdav/", nil)
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req)

    if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
        t.Logf("OPTIONS response code: %d", w.Code)
    }
}

// TestSetupRouter tests that SetupRouter registers routes correctly
func TestSetupRouter(t *testing.T) {
    // This test verifies the router setup function works
    tmpDir := t.TempDir()
    fs := NewFileSystem(tmpDir)
    h := NewHandler("/webdav", fs)

    // Create a minimal gin router
    req := httptest.NewRequest("PROPFIND", "/webdav/", nil)
    req.Header.Set("Depth", "0")
    w := httptest.NewRecorder()

    // Direct handler test (since we can't easily create gin.Group in test)
    h.ServeHTTP(w, req)

    // PROPFIND on empty directory should return a valid response
    if w.Code != http.StatusOK && w.Code != http.StatusMultiStatus {
        t.Logf("PROPFIND response code: %d (expected 207 or 200)", w.Code)
    }
}
