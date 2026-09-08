package webdav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"fuzhan/internal/models"
)

// RecordingRepository defines the interface for recording operations.
type RecordingRepository interface {
	Create(record *models.OperationRecord) error
}

// RecordingFileSystem wraps a webdav.FileSystem and records file operations
// (uploads, downloads, directory creation) to the database via a repository.
//
// The decorator reads user UUID, client IP, and HTTP method from the request
// context — these values are set by OptionalBasicAuthMiddleware.
type RecordingFileSystem struct {
	inner      webdav.FileSystem
	recordRepo RecordingRepository
}

// NewRecordingFileSystem creates a RecordingFileSystem that records operations
// through the given repository. Returns os.ErrPermission from all methods if
// recordRepo is nil.
func NewRecordingFileSystem(inner webdav.FileSystem, recordRepo RecordingRepository) *RecordingFileSystem {
	return &RecordingFileSystem{
		inner:      inner,
		recordRepo: recordRepo,
	}
}

func (fs *RecordingFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if fs.recordRepo == nil {
		return os.ErrPermission
	}
	if err := fs.inner.Mkdir(ctx, name, perm); err != nil {
		return err
	}
	rootName, subPath := splitPath(name)
	fs.recordAsync(&models.OperationRecord{
		Action:   "mkdir",
		FileName: filepath.Base(name),
		// WebDAV 虚拟路径为 "/<rootName>/<subPath>"，FilePath 取子路径以便与 FileRecord 保持一致
		FilePath: ensureLeadingSlash(subPath),
		// FullPath 拼上 rootName 前缀，便于统一查询
		FullPath: rootName + ensureLeadingSlash(subPath),
		RootName: rootName,
		ClientIP: getClientIP(ctx),
		UserID:   getUserUUID(ctx),
	})
	return nil
}

func (fs *RecordingFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if fs.recordRepo == nil {
		return nil, os.ErrPermission
	}
	f, err := fs.inner.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}

	action := detectAction(ctx, flag)
	if action == "" {
		return f, nil
	}

	rootName, subPath := splitPath(name)
	record := &models.OperationRecord{
		Action:     action,
		FileName:   filepath.Base(name),
		FilePath:   ensureLeadingSlash(subPath),
		FullPath:   "/" + rootName + ensureLeadingSlash(subPath),
		RootName:   rootName,
		ClientIP:   getClientIP(ctx),
		UserID:     getUserUUID(ctx),
		UploadType: detectUploadType(name),
	}

	// For downloads, try to get file size from the opened file handle.
	if action == "download" {
		if info, err := f.Stat(); err == nil {
			record.FileSize = info.Size()
		}
	}

	fs.recordAsync(record)
	return f, nil
}

func (fs *RecordingFileSystem) RemoveAll(ctx context.Context, name string) error {
	if fs.recordRepo == nil {
		return os.ErrPermission
	}
	return fs.inner.RemoveAll(ctx, name)
}

func (fs *RecordingFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	if fs.recordRepo == nil {
		return os.ErrPermission
	}
	return fs.inner.Rename(ctx, oldName, newName)
}

func (fs *RecordingFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if fs.recordRepo == nil {
		return nil, os.ErrPermission
	}
	return fs.inner.Stat(ctx, name)
}

// detectAction determines the operation action based on HTTP method and file
// open flags. Returns "upload", "download", or empty string (no recording).
func detectAction(ctx context.Context, flag int) string {
	method, _ := ctx.Value(ContextKeyMethod).(string)
	switch {
	case isWriteFlags(flag):
		return "upload"
	case method == "GET" || method == "HEAD":
		return "download"
	default:
		return ""
	}
}

// isWriteFlags returns true if the file open flags include any write mode.
func isWriteFlags(flag int) bool {
	return flag&(os.O_RDWR|os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
}

// detectUploadType determines the target type from the virtual path.
// Paths starting with "/private" are private storage.
func detectUploadType(name string) models.TargetType {
	if strings.HasPrefix(name, "/private") && (len(name) == 8 || name[8] == '/') {
		return models.TargetTypePrivate
	}
	return models.TargetTypeRegular
}

func getUserUUID(ctx context.Context) string {
	uuid, _ := ctx.Value(ContextKeyUserUUID).(string)
	return uuid
}

func getClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(ContextKeyClientIP).(string)
	return ip
}

// ensureLeadingSlash returns s prefixed with "/" if it is non-empty and
// does not already start with one. Recording uses this so that FilePath
// always carries the leading slash convention used by FileRecord entries.
func ensureLeadingSlash(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	return "/" + s
}

// recordAsync records an operation in a background goroutine.
// Failures are silently ignored since recording is non-critical.
func (fs *RecordingFileSystem) recordAsync(record *models.OperationRecord) {
	go func() {
		// 带超时保护，防止 goroutine 因 DB 卡住而永久泄漏
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()

		done := make(chan struct{})
		go func() {
			_ = fs.recordRepo.Create(record)
			close(done)
		}()

		select {
		case <-done:
		case <-timer.C:
			// 超时，放弃记录操作以避免 goroutine 无限等待
		}
	}()
}

// Compile-time interface checks.
var _ webdav.FileSystem = (*RecordingFileSystem)(nil)
