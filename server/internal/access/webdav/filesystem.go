package webdav

import (
    "context"
    "io/fs"
    "os"
    "path/filepath"
    "time"

    "golang.org/x/net/webdav"

    "fuzhan/internal/utils"
)

// FileSystem implements webdav.FileSystem by wrapping an OS root directory
// with sub-path scope validation.
type FileSystem struct {
    rootDir   string
    validator *utils.PathValidator
}

// NewFileSystem creates a WebDAV filesystem for the given root directory.
func NewFileSystem(rootDir string) *FileSystem {
    absRoot, _ := filepath.Abs(rootDir)
    return &FileSystem{
        rootDir:   absRoot,
        validator: utils.NewPathValidator("webdav", absRoot),
    }
}

// setValidator sets the path validator for scope checking.
func (fs *FileSystem) setValidator(v *utils.PathValidator) {
    fs.validator = v
}

func (fs *FileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
    fullPath := filepath.Join(fs.rootDir, name)
    if err := fs.validator.Validate(fullPath); err != nil {
        return os.ErrPermission
    }
    return os.MkdirAll(fullPath, perm)
}

func (fs *FileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
    fullPath := filepath.Join(fs.rootDir, name)
    if err := fs.validator.Validate(fullPath); err != nil {
        return nil, os.ErrPermission
    }
    f, err := os.OpenFile(fullPath, flag, perm)
    if err != nil {
        return nil, err
    }
    return f, nil
}

func (fs *FileSystem) RemoveAll(ctx context.Context, name string) error {
    fullPath := filepath.Join(fs.rootDir, name)
    if err := fs.validator.Validate(fullPath); err != nil {
        return os.ErrPermission
    }
    return os.RemoveAll(fullPath)
}

func (fs *FileSystem) Rename(ctx context.Context, oldName, newName string) error {
    oldPath := filepath.Join(fs.rootDir, oldName)
    newPath := filepath.Join(fs.rootDir, newName)
    if err := fs.validator.Validate(oldPath); err != nil {
        return os.ErrPermission
    }
    if err := fs.validator.Validate(newPath); err != nil {
        return os.ErrPermission
    }
    return os.Rename(oldPath, newPath)
}

func (fs *FileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
    fullPath := filepath.Join(fs.rootDir, name)
    if err := fs.validator.Validate(fullPath); err != nil {
        return nil, os.ErrPermission
    }
    info, err := os.Stat(fullPath)
    if err != nil {
        return nil, err
    }
    return info, nil
}

// dirInfo implements os.FileInfo for synthetic directory entries.
type dirInfo struct {
    name  string
    isDir bool
}

func (d *dirInfo) Name() string      { return d.name }
func (d *dirInfo) Size() int64       { return 0 }
func (d *dirInfo) Mode() os.FileMode { return os.ModeDir | 0755 }
func (d *dirInfo) ModTime() time.Time { return time.Time{} }
func (d *dirInfo) IsDir() bool       { return d.isDir }
func (d *dirInfo) Sys() interface{}  { return nil }

// Ensure *dirInfo implements fs.FileInfo
var _ fs.FileInfo = (*dirInfo)(nil)
