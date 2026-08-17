package webdav

import (
    "context"
    "io"
    "io/fs"
    "os"
    "sort"
    "strings"
    "time"

    "golang.org/x/net/webdav"
)

// UnifiedFileSystem implements webdav.FileSystem that routes requests between
// public and private storage based on the path prefix and authentication status.
//
// Directory structure:
//
//  /
//    public/        ← shared root dirs (visible to all)
//    private/       ← user private storage (authenticated only)
type UnifiedFileSystem struct {
    publicFS  webdav.FileSystem
    privateFS webdav.FileSystem
}

// NewUnifiedFileSystem creates a UnifiedFileSystem with the given public and
// private file systems. privateFS may be nil if private storage is not enabled.
func NewUnifiedFileSystem(publicFS, privateFS webdav.FileSystem) *UnifiedFileSystem {
    return &UnifiedFileSystem{
        publicFS:  publicFS,
        privateFS: privateFS,
    }
}

func (fs *UnifiedFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
    prefix, subPath := splitUnifiedPath(name)
    switch prefix {
    case "public":
        return fs.publicFS.Mkdir(ctx, subPath, perm)
    case "private":
        if fs.privateFS == nil {
            return os.ErrPermission
        }
        if !isAuthenticated(ctx) {
            return os.ErrPermission
        }
        return fs.privateFS.Mkdir(ctx, subPath, perm)
    default:
        return os.ErrPermission
    }
}

func (fs *UnifiedFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
    prefix, subPath := splitUnifiedPath(name)
    switch prefix {
    case "public":
        return fs.publicFS.OpenFile(ctx, subPath, flag, perm)
    case "private":
        if fs.privateFS == nil {
            return nil, os.ErrPermission
        }
        if !isAuthenticated(ctx) {
            return nil, os.ErrPermission
        }
        return fs.privateFS.OpenFile(ctx, subPath, flag, perm)
    case "":
        // Virtual root — list public/ and (if authenticated) private/
        return &unifiedRootDir{
            hasPrivate: fs.privateFS != nil && isAuthenticated(ctx),
        }, nil
    default:
        return nil, os.ErrNotExist
    }
}

func (fs *UnifiedFileSystem) RemoveAll(ctx context.Context, name string) error {
    prefix, subPath := splitUnifiedPath(name)
    switch prefix {
    case "public":
        return fs.publicFS.RemoveAll(ctx, subPath)
    case "private":
        if fs.privateFS == nil {
            return os.ErrPermission
        }
        if !isAuthenticated(ctx) {
            return os.ErrPermission
        }
        return fs.privateFS.RemoveAll(ctx, subPath)
    default:
        return os.ErrPermission
    }
}

func (fs *UnifiedFileSystem) Rename(ctx context.Context, oldName, newName string) error {
    oldPrefix, oldSubPath := splitUnifiedPath(oldName)
    newPrefix, newSubPath := splitUnifiedPath(newName)

    // Cross-namespace rename is not allowed
    if oldPrefix != newPrefix {
        return os.ErrPermission
    }

    switch oldPrefix {
    case "public":
        return fs.publicFS.Rename(ctx, oldSubPath, newSubPath)
    case "private":
        if fs.privateFS == nil {
            return os.ErrPermission
        }
        if !isAuthenticated(ctx) {
            return os.ErrPermission
        }
        return fs.privateFS.Rename(ctx, oldSubPath, newSubPath)
    default:
        return os.ErrPermission
    }
}

func (fs *UnifiedFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
    prefix, subPath := splitUnifiedPath(name)
    switch prefix {
    case "public":
        return fs.publicFS.Stat(ctx, subPath)
    case "private":
        if fs.privateFS == nil {
            return nil, os.ErrPermission
        }
        if !isAuthenticated(ctx) {
            return nil, os.ErrPermission
        }
        return fs.privateFS.Stat(ctx, subPath)
    case "":
        // Virtual root
        return &unifiedRootDirInfo{
            hasPrivate: fs.privateFS != nil && isAuthenticated(ctx),
        }, nil
    default:
        return nil, os.ErrNotExist
    }
}

// splitUnifiedPath splits a unified path into the top-level prefix (public/private)
// and the remaining sub-path.
func splitUnifiedPath(name string) (prefix, subPath string) {
    name = strings.TrimPrefix(name, "/")
    if name == "" {
        return "", ""
    }
    parts := strings.SplitN(name, "/", 2)
    prefix = parts[0]
    if len(parts) > 1 {
        subPath = "/" + parts[1]
    } else {
        subPath = "/"
    }
    return
}

// isAuthenticated checks if the context has a non-empty user UUID.
func isAuthenticated(ctx context.Context) bool {
    uuid, _ := ctx.Value(ContextKeyUserUUID).(string)
    return uuid != ""
}

// unifiedRootDir implements webdav.File for the top-level directory listing.
type unifiedRootDir struct {
    hasPrivate bool
    pos        int
}

func (d *unifiedRootDir) Close() error { return nil }

func (d *unifiedRootDir) Read(_ []byte) (int, error) {
    return 0, io.EOF
}

func (d *unifiedRootDir) Write(_ []byte) (int, error) {
    return 0, os.ErrPermission
}

func (d *unifiedRootDir) Seek(offset int64, whence int) (int64, error) {
    if offset == 0 && whence == 0 {
        d.pos = 0
    }
    return 0, nil
}

func (d *unifiedRootDir) Stat() (os.FileInfo, error) {
    return &unifiedRootDirInfo{hasPrivate: d.hasPrivate}, nil
}

func (d *unifiedRootDir) Readdir(count int) ([]os.FileInfo, error) {
    var entries []string
    entries = append(entries, "public")
    if d.hasPrivate {
        entries = append(entries, "private")
    }
    sort.Strings(entries)

    if d.pos >= len(entries) {
        if count > 0 {
            return nil, io.EOF
        }
        return nil, nil
    }

    entries = entries[d.pos:]
    if count > 0 && count < len(entries) {
        entries = entries[:count]
    }
    d.pos += len(entries)

    infos := make([]os.FileInfo, 0, len(entries))
    for _, name := range entries {
        infos = append(infos, &unifiedRootDirInfo{name: name})
    }
    return infos, nil
}

// unifiedRootDirInfo implements os.FileInfo for virtual root entries.
type unifiedRootDirInfo struct {
	name      string
	hasPrivate bool
}

func (d *unifiedRootDirInfo) Name() string { return d.name }

func (d *unifiedRootDirInfo) Size() int64 { return 0 }

func (d *unifiedRootDirInfo) Mode() os.FileMode { return os.ModeDir | 0755 }

func (d *unifiedRootDirInfo) ModTime() time.Time { return time.Time{} }

func (d *unifiedRootDirInfo) IsDir() bool { return true }

func (d *unifiedRootDirInfo) Sys() interface{} { return nil }

// Ensure interfaces are satisfied.
var _ fs.FileInfo = (*unifiedRootDirInfo)(nil)
var _ webdav.File = (*unifiedRootDir)(nil)
