package webdav

import (
	"context"
	"errors"
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

// PublicFileSystem implements a webdav.FileSystem that serves multiple root
// directories. Paths are formatted as: /<root_name>/<sub_path>.
// Public directories are create-only: new files/dirs can be created, but
// existing files cannot be overwritten, and delete/rename are forbidden.
// The root name is the basename of the directory path in config.RootNames.
type PublicFileSystem struct {
	rootNames  map[string]string
	validators map[string]*utils.PathValidator
	backend    Backend // 服务层能力（上传开关/扩展名/磁盘/索引）；nil 时仅跳过校验（测试用）
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

// SetBackend 注入服务层后端能力（上传开关/扩展名/磁盘/索引）。
func (fs *PublicFileSystem) SetBackend(backend Backend) {
	fs.backend = backend
}

// canCreatePublic 校验公开写入口：全局上传开关 + 扩展名白名单 + 磁盘空间。
// backend 为 nil 时保持旧行为（不校验），用于单元测试。
func (fs *PublicFileSystem) canCreatePublic(location, filename string) error {
	if fs.backend == nil {
		return nil
	}
	if !fs.backend.UploadEnabled() {
		return os.ErrPermission
	}
	if filename != "" {
		if err := fs.backend.ValidateExtension(filename); err != nil {
			return os.ErrPermission
		}
	}
	if err := fs.backend.CheckDiskSpace(location, 1); err != nil {
		return os.ErrPermission
	}
	return nil
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
	if err := fs.canCreatePublic(filepath.Dir(fullPath), ""); err != nil {
		return err
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

	// New file creation is allowed. O_EXCL guarantees we only create brand-new
	// files (existing files are never overwritten), eliminating the Stat-then-open
	// TOCTOU race against a concurrent PUT to the same path.
	isCreate := flag&os.O_CREATE != 0
	if isCreate {
		if err := fs.canCreatePublic(filepath.Dir(fullPath), subPath); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(fullPath, flag|os.O_EXCL, perm)
		if err != nil {
			// 目标已存在（或并发方抢建）：公开目录只允许新建，映射为权限错误
			if errors.Is(err, os.ErrExist) {
				return nil, os.ErrPermission
			}
			return nil, err
		}
		// 上传完成后经 Close 触发索引/哈希/上传者 IP 同步（H2）
		return &syncedPublicFile{
			File:       f,
			backend:    fs.backend,
			rootName:   rootName,
			relPath:    subPath,
			clientIP:   getClientIP(ctx),
			uploadOnly: isWriteFlags(flag),
		}, nil
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

// rootReachable 判定共享根目录在磁盘上真实可达。
// 用 os.Lstat 判定（与扫描器护栏一致）：junction/符号链接根或不存在/不可访问目录的
// Lstat().IsDir() 为 false，可拦截幽灵根不参与列表。
func rootReachable(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
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
	for name, rootPath := range d.names {
		// 仅列出磁盘上真实可达的根目录，避免幽灵目录（配置了但在磁盘上不存在/不可访问）。
		if rootReachable(rootPath) {
			names = append(names, name)
		}
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

// syncedPublicFile 包装新建的公开文件句柄：在请求写完后（Close）把文件同步进索引，
// 触发哈希计算并写入上传者 IP（H2 链路）。backend 为 nil 时仅透传（单元测试用）。
type syncedPublicFile struct {
	*os.File
	backend    Backend
	rootName   string
	relPath    string
	clientIP   string
	uploadOnly bool
}

func (f *syncedPublicFile) Close() error {
	err := f.File.Close()
	if f.backend != nil && f.uploadOnly {
		// 可能尚未写入完成即被异常关闭；文件确实被成功创建（O_EXCL）才同步。
		if _, serr := os.Stat(f.File.Name()); serr == nil {
			f.backend.SyncPublicFile(f.rootName, f.relPath, f.clientIP)
		}
	}
	return err
}

// Ensure the root listing walks through root directory file objects correctly
// by providing Stat info for the files returned by Readdir. The webdav handler
// calls Stat on each returned entry.
