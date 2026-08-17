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
}

// NewPrivateFileSystem creates a PrivateFileSystem rooted at the private
// files base path (config.Storage.Private.Path). User directories are at
// {basePath}/users/{userUUID}/.
func NewPrivateFileSystem(basePath string) *PrivateFileSystem {
    absPath, _ := filepath.Abs(basePath)
    return &PrivateFileSystem{basePath: absPath}
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
    return os.OpenFile(fullPath, flag, perm)
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
