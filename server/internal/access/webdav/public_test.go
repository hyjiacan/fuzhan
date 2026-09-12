package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicFileSystem_RootListing(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	root2 := filepath.Join(tmpDir, "share2")
	os.MkdirAll(root1, 0755)
	os.MkdirAll(root2, 0755)

	rootNames := map[string]string{
		"share1": root1,
		"share2": root2,
	}
	fs := NewPublicFileSystem(rootNames)
	ctx := context.Background()

	// Stat on "/" should return directory info
	info, err := fs.Stat(ctx, "/")
	if err != nil {
		t.Fatalf("Stat / failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("Stat / should be a directory")
	}

	// OpenFile on "/" should list root directories
	f, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile / failed: %v", err)
	}
	defer f.Close()

	entries, err := f.Readdir(0)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 root entries, got %d", len(entries))
	}

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	sort.Strings(names)
	if names[0] != "share1" || names[1] != "share2" {
		t.Errorf("Expected [share1 share2], got %v", names)
	}
}

func TestPublicFileSystem_RootListingFiltersMissingRoot(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "existing")
	os.MkdirAll(existing, 0755)
	missing := filepath.Join(tmpDir, "missing") // 磁盘上不存在，属幽灵根，不应被列出
	jar := filepath.Join(tmpDir, "jar.notdir")  // 存在但不是目录，也不应列出
	os.WriteFile(jar, []byte("x"), 0644)

	fs := NewPublicFileSystem(map[string]string{
		"existing": existing,
		"missing":  missing,
		"jar":      jar,
	})
	ctx := context.Background()

	f, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile / failed: %v", err)
	}
	defer f.Close()

	entries, err := f.Readdir(0)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "existing" {
		got := make([]string, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Fatalf("幽灵根应被过滤, 期望仅 [existing], 实际 %v", got)
	}
}

func TestPublicFileSystem_ResolveFile(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	os.MkdirAll(root1, 0755)

	// Create a file in the root directory
	fileContent := "hello public webdav"
	os.WriteFile(filepath.Join(root1, "test.txt"), []byte(fileContent), 0644)

	rootNames := map[string]string{"share1": root1}
	fs := NewPublicFileSystem(rootNames)
	ctx := context.Background()

	// Open file via WebDAV path
	f, err := fs.OpenFile(ctx, "/share1/test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer f.Close()

	buf := make([]byte, len(fileContent))
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != fileContent {
		t.Errorf("Read content mismatch: got %q, want %q", string(buf[:n]), fileContent)
	}

	// Stat the file
	info, err := fs.Stat(ctx, "/share1/test.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.IsDir() {
		t.Error("test.txt should not be a directory")
	}
}

func TestPublicFileSystem_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	os.MkdirAll(root1, 0755)

	rootNames := map[string]string{"share1": root1}
	fs := NewPublicFileSystem(rootNames)
	ctx := context.Background()

	// Mkdir should succeed (public allows creation)
	if err := fs.Mkdir(ctx, "/share1/newdir", 0755); err != nil {
		t.Errorf("Mkdir should succeed, got %v", err)
	}

	// RemoveAll should fail
	if err := fs.RemoveAll(ctx, "/share1/existing"); err != os.ErrPermission {
		t.Errorf("RemoveAll should return ErrPermission, got %v", err)
	}

	// Rename should fail
	if err := fs.Rename(ctx, "/share1/a", "/share1/b"); err != os.ErrPermission {
		t.Errorf("Rename should return ErrPermission, got %v", err)
	}

	// OpenFile with O_CREATE on new file should succeed (create allowed)
	f, err := fs.OpenFile(ctx, "/share1/created.txt", os.O_CREATE, 0644)
	if err != nil {
		t.Errorf("OpenFile with O_CREATE on new file should succeed, got %v", err)
	}
	if f != nil {
		f.Close()
	}

	// OpenFile with write flag on existing file should fail (modify forbidden)
	f2, err := fs.OpenFile(ctx, "/share1/created.txt", os.O_RDWR|os.O_CREATE, 0644)
	if err != os.ErrPermission {
		t.Errorf("OpenFile with write flag on existing file should return ErrPermission, got %v", err)
	}
	if f2 != nil {
		f2.Close()
	}

	// OpenFile with O_WRONLY (no O_CREATE) should fail
	f3, err := fs.OpenFile(ctx, "/share1/nonexistent.txt", os.O_WRONLY, 0)
	if err != os.ErrPermission {
		t.Errorf("OpenFile with O_WRONLY should return ErrPermission, got %v", err)
	}
	if f3 != nil {
		f3.Close()
	}

	// Read-only should still work for existing files
	f4, err := fs.OpenFile(ctx, "/share1/created.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Errorf("OpenFile with O_RDONLY should succeed, got %v", err)
	}
	if f4 != nil {
		f4.Close()
	}
}

func TestPublicFileSystem_UnknownRoot(t *testing.T) {
	tmpDir := t.TempDir()
	rootNames := map[string]string{"share1": tmpDir}
	fs := NewPublicFileSystem(rootNames)
	ctx := context.Background()

	// Stat on unknown root
	_, err := fs.Stat(ctx, "/nonexistent/file.txt")
	if err == nil {
		t.Error("Expected error for unknown root")
	}

	// OpenFile on unknown root
	_, err = fs.OpenFile(ctx, "/nonexistent/file.txt", os.O_RDONLY, 0)
	if err == nil {
		t.Error("Expected error for unknown root")
	}
}

func TestPublicFileSystem_EmptyRootNames(t *testing.T) {
	rootNames := map[string]string{}
	fs := NewPublicFileSystem(rootNames)
	ctx := context.Background()

	// Stat on "/" with no roots is still valid (empty listing)
	info, err := fs.Stat(ctx, "/")
	if err != nil {
		t.Fatalf("Stat / failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("Stat / should be a directory")
	}

	// OpenFile on "/" with no roots
	f, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile / failed: %v", err)
	}
	defer f.Close()

	entries, err := f.Readdir(0)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestPublicFileSystem_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	os.MkdirAll(root1, 0755)

	// Create a file outside the root
	outsideFile := filepath.Join(tmpDir, "secret.txt")
	os.WriteFile(outsideFile, []byte("secret"), 0644)

	rootNames := map[string]string{"share1": root1}
	fs := NewPublicFileSystem(rootNames)
	ctx := context.Background()

	// Try path traversal to access file outside root
	_, err := fs.Stat(ctx, "/share1/../secret.txt")
	if err == nil {
		t.Error("Expected error for path traversal")
	}

	_, err = fs.OpenFile(ctx, "/share1/../secret.txt", os.O_RDONLY, 0)
	if err == nil {
		t.Error("Expected error for path traversal")
	}
}

func TestReadOnlyMiddleware(t *testing.T) {
	middleware := readOnlyMiddleware()

	tests := []struct {
		name      string
		method    string
		wantBlock bool
	}{
		{"GET", "GET", false},
		{"HEAD", "HEAD", false},
		{"PROPFIND", "PROPFIND", false},
		{"OPTIONS", "OPTIONS", false},
		{"PUT", "PUT", true},
		{"MKCOL", "MKCOL", true},
		{"MOVE", "MOVE", true},
		{"DELETE", "DELETE", true},
		{"COPY", "COPY", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, "/webdav/public/", nil)

			middleware(c)

			if tt.wantBlock && w.Code != http.StatusForbidden {
				t.Errorf("Expected 403 Forbidden for %s, got %d", tt.method, w.Code)
			}
			if !tt.wantBlock && w.Code != 0 && w.Code != http.StatusOK {
				t.Errorf("Expected pass for %s, got %d", tt.method, w.Code)
			}
		})
	}
}

func TestSetupPublicRouter(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	os.MkdirAll(root1, 0755)

	rootNames := map[string]string{"share1": root1}
	fs := NewPublicFileSystem(rootNames)
	h := NewHandler("/api/v1/webdav/public", fs)

	// Test PROPFIND on public endpoint
	req := httptest.NewRequest("PROPFIND", "/api/v1/webdav/public/", nil)
	req.Header.Set("Depth", "0")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusMultiStatus {
		t.Logf("PROPFIND response code: %d", w.Code)
	}
}

func TestUnifiedFileSystem_AnonymousAccess(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	os.MkdirAll(root1, 0755)

	rootNames := map[string]string{"share1": root1}
	publicFS := NewPublicFileSystem(rootNames)
	unifiedFS := NewUnifiedFileSystem(publicFS, nil)
	ctx := context.Background()

	// Anonymous Stat on root should succeed
	info, err := unifiedFS.Stat(ctx, "/")
	if err != nil {
		t.Fatalf("Stat / failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("Stat / should be a directory")
	}

	// Anonymous OpenFile on root should list public + private entries
	f, err := unifiedFS.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile / failed: %v", err)
	}
	defer f.Close()
	entries, err := f.Readdir(0)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}
	// Without private storage, only "public" should be visible
	if len(entries) != 1 || entries[0].Name() != "public" {
		t.Errorf("Expected [public], got %v", entryNames(entries))
	}

	// Anonymous OpenFile on /public should succeed
	pf, err := unifiedFS.OpenFile(ctx, "/public", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile /public failed: %v", err)
	}
	defer pf.Close()
	publicEntries, err := pf.Readdir(0)
	if err != nil {
		t.Fatalf("Readdir /public failed: %v", err)
	}
	if len(publicEntries) != 1 || publicEntries[0].Name() != "share1" {
		t.Errorf("Expected [share1], got %v", entryNames(publicEntries))
	}

	// Anonymous Stat on /private should fail (no private storage)
	_, err = unifiedFS.Stat(ctx, "/private")
	if err == nil {
		t.Error("Anonymous Stat on /private should fail without private storage")
	}

	// Anonymous OpenFile on /private should fail
	_, err = unifiedFS.OpenFile(ctx, "/private", os.O_RDONLY, 0)
	if err == nil {
		t.Error("Anonymous OpenFile on /private should fail")
	}
}

// entryNames extracts directory entry names for test assertions.
func entryNames(entries []os.FileInfo) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
