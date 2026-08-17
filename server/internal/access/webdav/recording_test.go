package webdav

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "golang.org/x/net/webdav"

    "fuzhan/internal/models"
)

// mockRecordRepo implements RecordingRepository for testing.
type mockRecordRepo struct {
    records []*models.OperationRecord
    err     error
}

func (r *mockRecordRepo) Create(record *models.OperationRecord) error {
    if r.err != nil {
        return r.err
    }
    r.records = append(r.records, record)
    return nil
}

// inMemoryFileSystem implements webdav.FileSystem using os operations on a temp dir.
type inMemoryFileSystem struct {
    root string
}

func newInMemoryFS(t *testing.T) (*inMemoryFileSystem, string) {
    t.Helper()
    dir, err := os.MkdirTemp("", "recording-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    return &inMemoryFileSystem{root: dir}, dir
}

func (fs *inMemoryFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
    return os.MkdirAll(filepath.Join(fs.root, name), perm)
}

func (fs *inMemoryFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
    fullPath := filepath.Join(fs.root, name)
    if flag&os.O_CREATE != 0 {
        os.MkdirAll(filepath.Dir(fullPath), 0755)
    }
    return os.OpenFile(fullPath, flag, perm)
}

func (fs *inMemoryFileSystem) RemoveAll(ctx context.Context, name string) error {
    return os.RemoveAll(filepath.Join(fs.root, name))
}

func (fs *inMemoryFileSystem) Rename(ctx context.Context, oldName, newName string) error {
    return os.Rename(filepath.Join(fs.root, oldName), filepath.Join(fs.root, newName))
}

func (fs *inMemoryFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
    return os.Stat(filepath.Join(fs.root, name))
}

// failingFS wraps an inMemoryFileSystem and fails on Mkdir when failMkdir is set.
type failingFS struct {
    inner     *inMemoryFileSystem
    failMkdir bool
}

func (f *failingFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
    if f.failMkdir {
        return os.ErrPermission
    }
    return f.inner.Mkdir(ctx, name, perm)
}

func (f *failingFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
    return f.inner.OpenFile(ctx, name, flag, perm)
}

func (f *failingFS) RemoveAll(ctx context.Context, name string) error {
    return f.inner.RemoveAll(ctx, name)
}

func (f *failingFS) Rename(ctx context.Context, oldName, newName string) error {
    return f.inner.Rename(ctx, oldName, newName)
}

func (f *failingFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
    return f.inner.Stat(ctx, name)
}

// recordingContext creates a context with the given method, client IP, and user UUID.
func recordingContext(method, clientIP, userUUID string) context.Context {
    ctx := context.Background()
    ctx = context.WithValue(ctx, ContextKeyMethod, method)
    ctx = context.WithValue(ctx, ContextKeyClientIP, clientIP)
    ctx = context.WithValue(ctx, ContextKeyUserUUID, userUUID)
    return ctx
}

// waitForRecords polls until repo has at least n records, with a 1s timeout.
func waitForRecords(repo *mockRecordRepo, n int) bool {
    for i := 0; i < 100; i++ {
        if len(repo.records) >= n {
            return true
        }
        time.Sleep(10 * time.Millisecond)
    }
    return false
}

func TestRecordingFileSystem_NilRepo(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    rec := NewRecordingFileSystem(inner, nil)

    ctx := recordingContext("PUT", "127.0.0.1", "user-uuid")

    if _, err := rec.OpenFile(ctx, "/test.txt", os.O_CREATE|os.O_WRONLY, 0644); err != os.ErrPermission {
        t.Errorf("expected ErrPermission for nil repo on OpenFile, got %v", err)
    }
    if err := rec.Mkdir(ctx, "/dir", 0755); err != os.ErrPermission {
        t.Errorf("expected ErrPermission for nil repo on Mkdir, got %v", err)
    }
    if err := rec.RemoveAll(ctx, "/test.txt"); err != os.ErrPermission {
        t.Errorf("expected ErrPermission for nil repo on RemoveAll, got %v", err)
    }
    if err := rec.Rename(ctx, "/a", "/b"); err != os.ErrPermission {
        t.Errorf("expected ErrPermission for nil repo on Rename, got %v", err)
    }
    if _, err := rec.Stat(ctx, "/"); err != os.ErrPermission {
        t.Errorf("expected ErrPermission for nil repo on Stat, got %v", err)
    }
}

func TestRecordingFileSystem_Mkdir(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    ctx := recordingContext("MKCOL", "10.0.0.1", "test-uuid")

    if err := rec.Mkdir(ctx, "/newdir", 0755); err != nil {
        t.Fatalf("Mkdir failed: %v", err)
    }

    if _, err := os.Stat(filepath.Join(inner.root, "newdir")); os.IsNotExist(err) {
        t.Error("directory was not created on disk")
    }

    if !waitForRecords(repo, 1) {
        t.Fatal("timed out waiting for mkdir record")
    }
    r := repo.records[0]
    if r.Action != "mkdir" {
        t.Errorf("expected action mkdir, got %s", r.Action)
    }
    if r.FileName != "newdir" {
        t.Errorf("expected filename newdir, got %s", r.FileName)
    }
    if r.UserID != "test-uuid" {
        t.Errorf("expected userID test-uuid, got %s", r.UserID)
    }
    if r.ClientIP != "10.0.0.1" {
        t.Errorf("expected clientIP 10.0.0.1, got %s", r.ClientIP)
    }
}

func TestRecordingFileSystem_Mkdir_InnerError(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    failingInner := &failingFS{inner: inner, failMkdir: true}
    rec := NewRecordingFileSystem(failingInner, repo)

    ctx := recordingContext("MKCOL", "10.0.0.1", "test-uuid")

    err := rec.Mkdir(ctx, "/anydir", 0755)
    if err == nil {
        t.Fatal("expected error from failing inner FS, got nil")
    }

    if len(repo.records) != 0 {
        t.Errorf("expected 0 records on inner error, got %d", len(repo.records))
    }
}

func TestRecordingFileSystem_Upload(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    ctx := recordingContext("PUT", "192.168.1.1", "uploader-uuid")

    f, err := rec.OpenFile(ctx, "/upload.txt", os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatalf("OpenFile for upload failed: %v", err)
    }
    if _, err := f.Write([]byte("hello world")); err != nil {
        t.Fatalf("Write failed: %v", err)
    }
    f.Close()

    if !waitForRecords(repo, 1) {
        t.Fatal("timed out waiting for upload record")
    }
    r := repo.records[0]
    if r.Action != "upload" {
        t.Errorf("expected action upload, got %s", r.Action)
    }
    if r.FileName != "upload.txt" {
        t.Errorf("expected filename upload.txt, got %s", r.FileName)
    }
    if r.UserID != "uploader-uuid" {
        t.Errorf("expected userID uploader-uuid, got %s", r.UserID)
    }
    if r.ClientIP != "192.168.1.1" {
        t.Errorf("expected clientIP 192.168.1.1, got %s", r.ClientIP)
    }
    if r.UploadType != models.TargetTypeRegular {
        t.Errorf("expected TargetTypeRegular, got %s", r.UploadType)
    }
}

func TestRecordingFileSystem_UploadPrivate(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    ctx := recordingContext("PUT", "10.0.0.2", "private-user")

    f, err := rec.OpenFile(ctx, "/private/myfile.txt", os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatalf("OpenFile for private upload failed: %v", err)
    }
    if _, err := f.Write([]byte("private data")); err != nil {
        t.Fatalf("Write failed: %v", err)
    }
    f.Close()

    if !waitForRecords(repo, 1) {
        t.Fatal("timed out waiting for private upload record")
    }
    r := repo.records[0]
    if r.Action != "upload" {
        t.Errorf("expected action upload, got %s", r.Action)
    }
    if r.UploadType != models.TargetTypePrivate {
        t.Errorf("expected TargetTypePrivate, got %s", r.UploadType)
    }
}

func TestRecordingFileSystem_Download(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    // Create a file first
    uploadCtx := recordingContext("PUT", "1.1.1.1", "uploader")
    f, err := rec.OpenFile(uploadCtx, "/download.txt", os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatalf("OpenFile for upload setup failed: %v", err)
    }
    content := strings.Repeat("A", 500)
    if _, err := f.Write([]byte(content)); err != nil {
        t.Fatalf("Write for upload setup failed: %v", err)
    }
    f.Close()
    waitForRecords(repo, 1)
    repo.records = nil // clear the upload record

    // Now download
    downloadCtx := recordingContext("GET", "2.2.2.2", "downloader")
    f2, err := rec.OpenFile(downloadCtx, "/download.txt", os.O_RDONLY, 0)
    if err != nil {
        t.Fatalf("OpenFile for download failed: %v", err)
    }
    defer f2.Close()

    if !waitForRecords(repo, 1) {
        t.Fatal("timed out waiting for download record")
    }
    r := repo.records[0]
    if r.Action != "download" {
        t.Errorf("expected action download, got %s", r.Action)
    }
    if r.FileName != "download.txt" {
        t.Errorf("expected filename download.txt, got %s", r.FileName)
    }
    if r.FileSize != 500 {
        t.Errorf("expected file size 500, got %d", r.FileSize)
    }
    if r.UserID != "downloader" {
        t.Errorf("expected userID downloader, got %s", r.UserID)
    }
    if r.ClientIP != "2.2.2.2" {
        t.Errorf("expected clientIP 2.2.2.2, got %s", r.ClientIP)
    }
}

func TestRecordingFileSystem_PropfindNoRecord(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    os.MkdirAll(inner.root, 0755)
    os.WriteFile(filepath.Join(inner.root, "exists.txt"), []byte("data"), 0644)

    ctx := recordingContext("PROPFIND", "3.3.3.3", "browser")
    f, err := rec.OpenFile(ctx, "/exists.txt", os.O_RDONLY, 0)
    if err != nil {
        t.Fatalf("OpenFile for PROPFIND failed: %v", err)
    }
    f.Close()

    if len(repo.records) != 0 {
        t.Errorf("expected 0 records for PROPFIND, got %d", len(repo.records))
    }
}

func TestRecordingFileSystem_Anonymous(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    ctx := recordingContext("PUT", "10.0.0.99", "")
    f, err := rec.OpenFile(ctx, "/anon.txt", os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatalf("OpenFile for anonymous upload failed: %v", err)
    }
    if _, err := f.Write([]byte("anon data")); err != nil {
        t.Fatalf("Write failed: %v", err)
    }
    f.Close()

    if !waitForRecords(repo, 1) {
        t.Fatal("timed out waiting for anonymous upload record")
    }
    r := repo.records[0]
    if r.UserID != "" {
        t.Errorf("expected empty userID for anonymous, got %s", r.UserID)
    }
}

func TestRecordingFileSystem_OpenFileInnerError(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    ctx := recordingContext("GET", "1.2.3.4", "user")
    _, err := rec.OpenFile(ctx, "/nonexistent.txt", os.O_RDONLY, 0)
    if !os.IsNotExist(err) {
        t.Errorf("expected ErrNotExist for nonexistent file, got %v", err)
    }

    if len(repo.records) != 0 {
        t.Errorf("expected 0 records on inner error, got %d", len(repo.records))
    }
}

func TestRecordingFileSystem_RemoveAll(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    os.MkdirAll(filepath.Join(inner.root, "dirtoremove"), 0755)

    ctx := recordingContext("DELETE", "", "")
    if err := rec.RemoveAll(ctx, "/dirtoremove"); err != nil {
        t.Fatalf("RemoveAll failed: %v", err)
    }

    if _, err := os.Stat(filepath.Join(inner.root, "dirtoremove")); !os.IsNotExist(err) {
        t.Error("directory should have been removed")
    }

    if len(repo.records) != 0 {
        t.Errorf("expected 0 records for RemoveAll, got %d", len(repo.records))
    }
}

func TestRecordingFileSystem_Rename(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    os.WriteFile(filepath.Join(inner.root, "old.txt"), []byte("rename me"), 0644)

    ctx := recordingContext("MOVE", "", "")
    if err := rec.Rename(ctx, "/old.txt", "/new.txt"); err != nil {
        t.Fatalf("Rename failed: %v", err)
    }

    if _, err := os.Stat(filepath.Join(inner.root, "new.txt")); os.IsNotExist(err) {
        t.Error("renamed file should exist")
    }

    if len(repo.records) != 0 {
        t.Errorf("expected 0 records for Rename, got %d", len(repo.records))
    }
}

func TestRecordingFileSystem_Stat(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{}
    rec := NewRecordingFileSystem(inner, repo)

    os.MkdirAll(inner.root, 0755)

    ctx := recordingContext("PROPFIND", "", "")
    info, err := rec.Stat(ctx, "/")
    if err != nil {
        t.Fatalf("Stat failed: %v", err)
    }
    if !info.IsDir() {
        t.Error("expected root to be a directory")
    }
}

func TestDetectUploadType(t *testing.T) {
    tests := []struct {
        path     string
        expected models.TargetType
    }{
        {"/public/test.txt", models.TargetTypeRegular},
        {"/public/", models.TargetTypeRegular},
        {"/private/test.txt", models.TargetTypePrivate},
        {"/private", models.TargetTypePrivate},
        {"/private/", models.TargetTypePrivate},
        {"/private/sub/dir/file.txt", models.TargetTypePrivate},
        {"/public", models.TargetTypeRegular},
        {"/something/else", models.TargetTypeRegular},
        {"test.txt", models.TargetTypeRegular},
    }
    for _, tt := range tests {
        got := detectUploadType(tt.path)
        if got != tt.expected {
            t.Errorf("detectUploadType(%q) = %s, want %s", tt.path, got, tt.expected)
        }
    }
}

func TestDetectAction(t *testing.T) {
    tests := []struct {
        method string
        flag   int
        want   string
    }{
        {"PUT", os.O_CREATE | os.O_WRONLY, "upload"},
        {"PUT", os.O_RDWR, "upload"},
        {"PUT", os.O_WRONLY, "upload"},
        {"PUT", os.O_RDONLY, ""},
        {"GET", os.O_RDONLY, "download"},
        {"HEAD", os.O_RDONLY, "download"},
        {"PROPFIND", os.O_RDONLY, ""},
        {"MKCOL", os.O_RDONLY, ""},
        {"MOVE", os.O_RDONLY, ""},
        {"DELETE", os.O_RDONLY, ""},
        {"OPTIONS", os.O_RDONLY, ""},
    }
    for _, tt := range tests {
        ctx := recordingContext(tt.method, "", "")
        got := detectAction(ctx, tt.flag)
        if got != tt.want {
            t.Errorf("detectAction(%s, %x) = %q, want %q", tt.method, tt.flag, got, tt.want)
        }
    }
}

func TestRecordingFileSystem_RepoError(t *testing.T) {
    inner, cleanup := newInMemoryFS(t)
    defer os.RemoveAll(cleanup)

    repo := &mockRecordRepo{err: errors.New("db unavailable")}
    rec := NewRecordingFileSystem(inner, repo)

    ctx := recordingContext("PUT", "1.1.1.1", "user")
    f, err := rec.OpenFile(ctx, "/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatalf("OpenFile should succeed even if repo errors: %v", err)
    }
    f.Close()
}
