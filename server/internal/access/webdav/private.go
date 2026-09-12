package webdav

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/webdav"
)

// PrivateFileSystem implements webdav.FileSystem scoped to the authenticated
// user's private storage directory. The user UUID is extracted from the
// request context, which is set by BasicAuthMiddleware.
type PrivateFileSystem struct {
	basePath string
	backend  Backend // 服务层能力（磁盘/配额/索引哈希）；nil 时保持旧行为（测试用）
}

// NewPrivateFileSystem creates a PrivateFileSystem rooted at the private
// files base path (config.Storage.Private.Path). User directories are at
// {basePath}/users/{userUUID}/.
func NewPrivateFileSystem(basePath string) *PrivateFileSystem {
	absPath, _ := filepath.Abs(basePath)
	return &PrivateFileSystem{basePath: absPath}
}

// SetBackend 注入服务层后端能力（磁盘/配额/索引哈希）。
func (fs *PrivateFileSystem) SetBackend(backend Backend) {
	fs.backend = backend
}

// getUserRoot returns the user's private storage root directory from context.
// The context value is set by BasicAuthMiddleware.
func (fs *PrivateFileSystem) getUserRoot(ctx context.Context) string {
	if userUUID, ok := ctx.Value(ContextKeyUserUUID).(string); ok {
		return filepath.Join(fs.basePath, "users", userUUID)
	}
	return ""
}

// validatePath resolves symlinks on both the user root and target path,
// then verifies the resolved target is within the user's private directory.
// It returns the validated absolute path for subsequent file operations.
func (fs *PrivateFileSystem) validatePath(ctx context.Context, name string) (string, error) {
	userRoot := fs.getUserRoot(ctx)
	if userRoot == "" {
		return "", os.ErrPermission
	}
	resolvedRoot, err := filepath.EvalSymlinks(userRoot)
	if err != nil {
		resolvedRoot = userRoot
	}
	target := filepath.Join(resolvedRoot, name)
	// Try to resolve symlinks on the existing target path
	// to catch symlink-outside-root attacks (e.g., a user-created symlink
	// pointing to /etc/passwd). If the path doesn't exist yet (e.g., new file),
	// the path constructed from resolvedRoot is inherently in-bounds.
	if resolvedTarget, err := filepath.EvalSymlinks(target); err == nil {
		target = resolvedTarget
	}
	rel, err := filepath.Rel(resolvedRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", os.ErrPermission
	}
	return target, nil
}

// ensureUserRoot creates the user's private root directory if it doesn't exist.
func (fs *PrivateFileSystem) ensureUserRoot(ctx context.Context) error {
	userRoot := fs.getUserRoot(ctx)
	if userRoot == "" {
		return os.ErrPermission
	}
	return os.MkdirAll(userRoot, 0755)
}

func (fs *PrivateFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	fullPath, err := fs.validatePath(ctx, name)
	if err != nil {
		return os.ErrPermission
	}
	return os.MkdirAll(fullPath, perm)
}

func (fs *PrivateFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	fullPath, err := fs.validatePath(ctx, name)
	if err != nil {
		return nil, os.ErrPermission
	}
	if err := fs.ensureUserRoot(ctx); err != nil {
		return nil, os.ErrPermission
	}

	isCreate := flag&os.O_CREATE != 0 && flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_TRUNC) != 0
	if isCreate && fs.backend != nil {
		// 私有写入口：磁盘空间预检 + 配额预检（真实大小最终在写完后校验，见 AddPrivateFile）
		if err := fs.backend.CheckDiskSpace(filepath.Dir(fullPath), 1); err != nil {
			return nil, os.ErrPermission
		}
		userUUID := getUserUUID(ctx)
		if userUUID != "" {
			if err := fs.backend.CheckPrivateQuota(userUUID, 1); err != nil {
				return nil, os.ErrPermission
			}
		}
	}

	f, err := os.OpenFile(fullPath, flag, perm)
	if err != nil {
		return nil, err
	}
	if isCreate && fs.backend != nil {
		// 写完成后同步私有文件记录（含分享码）并触发哈希（H2），真实大小在此刻校验
		return &syncedPrivateFile{
			File:     f,
			backend:  fs.backend,
			userUUID: getUserUUID(ctx),
			relPath:  name,
			fileName: filepath.Base(name),
		}, nil
	}
	return f, nil
}

func (fs *PrivateFileSystem) RemoveAll(ctx context.Context, name string) error {
	fullPath, err := fs.validatePath(ctx, name)
	if err != nil {
		return os.ErrPermission
	}
	return os.RemoveAll(fullPath)
}

func (fs *PrivateFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldPath, err := fs.validatePath(ctx, oldName)
	if err != nil {
		return os.ErrPermission
	}
	newPath, err := fs.validatePath(ctx, newName)
	if err != nil {
		return os.ErrPermission
	}
	return os.Rename(oldPath, newPath)
}

func (fs *PrivateFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	fullPath, err := fs.validatePath(ctx, name)
	if err != nil {
		return nil, os.ErrPermission
	}
	// Ensure user root exists so Stat works on fresh directories
	if name == "/" || name == "" {
		if err := fs.ensureUserRoot(ctx); err != nil {
			return nil, os.ErrPermission
		}
	}
	return os.Stat(fullPath)
}

// syncedPrivateFile 包装新建的私有文件句柄：写完后（Close）写入私有文件记录
// （含分享码）并触发哈希（H2），并对真实大小做最终配额校验。backend 为 nil 时透传。
type syncedPrivateFile struct {
	*os.File
	backend  Backend
	userUUID string
	relPath  string
	fileName string
}

func (f *syncedPrivateFile) Close() error {
	var size int64
	if fi, err := f.File.Stat(); err == nil {
		size = fi.Size()
	}
	err := f.File.Close()
	if f.backend != nil && size > 0 && f.userUUID != "" {
		// AddPrivateFile 内部会做最终配额校验，失败时回滚落盘文件
		_ = f.backend.AddPrivateFile(f.userUUID, f.relPath, f.fileName, size)
	}
	return err
}
