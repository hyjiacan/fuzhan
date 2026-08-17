package webdav

import (
    "context"
    "io"
    "io/fs"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "golang.org/x/net/webdav"

    "fuzhan/internal/utils"
)

// PublicFileSystem implements a read-only webdav.FileSystem that serves
// multiple root directories. Paths are formatted as: /<root_name>/<sub_path>.
// The root name is the basename of the directory path in config.RootNames.
type PublicFileSystem struct {
    rootNames  map[string]string
    validators map[string]*utils.PathValidator
}

// NewPublicFileSystem creates a PublicFileSystem from a root names map.
// The map keys are directory basenames, values are absolute directory paths.
func NewPublicFileSystem(rootNames map[string]string) *PublicFileSystem {
    validators := make(map[string]*utils.PathValidator, len(rootNames))
    for name, path := range rootNames {
        validators[name] = utils.NewPathValidator(name, path)
    }
    return &PublicFileSystem{
        rootNames:  rootNames,
        validators: validators,
    }
}

func (fs *PublicFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
    rootName, subPath := splitPath(name)
    rootPath, ok := fs.rootNames[rootName]
    if !ok {
        return os.ErrNotExist
    }
    fullPath := filepath.Join(rootPath, subPath)
    if err := fs.validators[rootName].Validate(fullPath); err != nil {
        return os.ErrPermission
    }
    return os.MkdirAll(fullPath, perm)
}

func (fs *PublicFileSystem) RemoveAll(_ context.Context, _ string) error {
    return os.ErrPermission
}

func (fs *PublicFileSystem) Rename(_ context.Context, _, _ string) error {
    return os.ErrPermission
}

func (fs *PublicFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
    // Root listing (always read-only)
    if name == "" || name == "/" {
        return &publicRootDir{names: fs.rootNames}, nil
    }

    rootName, subPath := splitPath(name)
    rootPath, ok := fs.rootNames[rootName]
    if !ok {
        return nil, os.ErrNotExist
    }

    fullPath := filepath.Join(rootPath, subPath)
    if err := fs.validators[rootName].Validate(fullPath); err != nil {
        return nil, os.ErrPermission
    }

    // New file creation is allowed
    isCreate := flag&os.O_CREATE != 0
    if isCreate {
        // Check if file already exists — modifying existing files is forbidden
        if _, err := os.Stat(fullPath); err == nil {
            return nil, os.ErrPermission
        }
        return os.OpenFile(fullPath, flag, perm)
    }

    // Read-only for existing files
    if flag&(os.O_WRONLY|os.O_RDWR|os.O_TRUNC|os.O_APPEND) != 0 {
        return nil, os.ErrPermission
    }

    return os.OpenFile(fullPath, os.O_RDONLY, 0)
}

func (fs *PublicFileSystem) Stat(_ context.Context, name string) (os.FileInfo, error) {
    // Root listing
    if name == "" || name == "/" {
        return &publicRootDirInfo{
            name:  "/",
            names: fs.rootNames,
        }, nil
    }

    rootName, subPath := splitPath(name)
    rootPath, ok := fs.rootNames[rootName]
    if !ok {
        return nil, os.ErrNotExist
    }

    fullPath := filepath.Join(rootPath, subPath)
    if err := fs.validators[rootName].Validate(fullPath); err != nil {
        return nil, os.ErrPermission
    }

    return os.Stat(fullPath)
}

// splitPath splits a WebDAV resource path into root name and sub-path.
// For "/root_name/sub/file.txt" returns ("root_name", "sub/file.txt").
// For "/root_name" returns ("root_name", "").
func splitPath(name string) (rootName, subPath string) {
    name = strings.TrimPrefix(name, "/")
    parts := strings.SplitN(name, "/", 2)
    if len(parts) == 0 {
        return "", ""
    }
    rootName = parts[0]
    if len(parts) > 1 {
        subPath = parts[1]
    }
    return
}

// publicRootDir implements http.File for the top-level directory listing
// of all configured root directories.
type publicRootDir struct {
    names map[string]string
    pos   int
}

func (d *publicRootDir) Close() error { return nil }

func (d *publicRootDir) Read(_ []byte) (int, error) {
    return 0, io.EOF
}

func (d *publicRootDir) Write(_ []byte) (int, error) {
    return 0, os.ErrPermission
}

func (d *publicRootDir) Seek(offset int64, whence int) (int64, error) {
    if offset == 0 && whence == 0 {
        d.pos = 0
    }
    return 0, nil
}

func (d *publicRootDir) Stat() (os.FileInfo, error) {
    return &publicRootDirInfo{name: "/", names: d.names}, nil
}

func (d *publicRootDir) Readdir(count int) ([]os.FileInfo, error) {
    names := make([]string, 0, len(d.names))
    for name := range d.names {
        names = append(names, name)
    }
    sort.Strings(names)

    // Skip already returned entries
    if d.pos >= len(names) {
        if count > 0 {
            return nil, io.EOF
        }
        return nil, nil
    }

    names = names[d.pos:]
    if count > 0 && count < len(names) {
        names = names[:count]
    }
    d.pos += len(names)

    infos := make([]os.FileInfo, 0, len(names))
    for _, name := range names {
        infos = append(infos, &publicRootDirInfo{name: name, names: d.names})
    }
    return infos, nil
}

// publicRootDirInfo implements os.FileInfo for a root directory entry.
type publicRootDirInfo struct {
    name  string
    names map[string]string
}

func (d *publicRootDirInfo) Name() string { return d.name }

func (d *publicRootDirInfo) Size() int64 { return 0 }

func (d *publicRootDirInfo) Mode() os.FileMode { return os.ModeDir | 0755 }

func (d *publicRootDirInfo) ModTime() time.Time { return time.Time{} }

func (d *publicRootDirInfo) IsDir() bool { return true }

func (d *publicRootDirInfo) Sys() interface{} { return nil }

// Ensure *publicRootDirInfo implements fs.FileInfo
var _ fs.FileInfo = (*publicRootDirInfo)(nil)

// Ensure the root listing walks through root directory file objects correctly
// by providing Stat info for the files returned by Readdir. The webdav handler
// calls Stat on each returned entry.

