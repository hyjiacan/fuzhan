package ftp

import (
    "fmt"
    "io"
    "os"
    "path"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "github.com/spf13/afero"

    "fuzhan/internal/appconfig"
)

// OperationRecordFunc 操作记录回调函数
// rootName 是 filePath 所属的逻辑根名（来自配置的 rootNames key，或 "private"），
// 用于让调用方构造 FullPath（rootName + filePath）。
type OperationRecordFunc func(action, filePath, fileName, rootName, fileTypeTag, clientIP, userID string, fileSize int64)

// MultiRootFs implements afero.Fs for FTP with a virtual directory structure:
//
//  /
//    public/        ← shared root dirs (anonymous + authenticated)
//    private/       ← user private storage (authenticated only, absent for anon)
//
// It does NOT implement afero.Symlinker, eliminating symlink-based attacks.
type MultiRootFs struct {
    rootNames         map[string]string // basename → absolute path for public roots
    privateDir        string            // private storage root path (empty if disabled)
    userUUID          string            // authenticated user UUID (empty for anonymous)
    clientIP          string            // client IP for recording
    allowedExtensions []string          // from appconfig.Storage.AllowedExtensions
    maxFileSize       int64             // from appconfig.Upload.MaxFileSize (0 = unlimited)
    recordFn          OperationRecordFunc
}

// NewMultiRootFs creates a MultiRootFs. For anonymous users, privateDir and
// userUUID should be empty; the private/ directory will not appear.
//
// clientIP is used for operation recording. recordFn may be nil to skip recording.
func NewMultiRootFs(rootNames map[string]string, privateDir, userUUID, clientIP string, recordFn OperationRecordFunc) *MultiRootFs {
    cfg := appconfig.GlobalConfig
    return &MultiRootFs{
        rootNames:         rootNames,
        privateDir:        privateDir,
        userUUID:          userUUID,
        clientIP:          clientIP,
        allowedExtensions: cfg.Storage.AllowedExtensions,
        maxFileSize:       cfg.Upload.MaxFileSize,
        recordFn:          recordFn,
    }
}

// ---- virtual path helpers ----

// cleanFtpPath normalizes an FTP virtual path with OS-independent cleaning.
// Returns the cleaned path with leading "/" stripped.
func cleanFtpPath(name string) string {
    cleaned := path.Clean(name)
    return strings.TrimPrefix(cleaned, "/")
}

// splitFtpPath splits a cleaned virtual path into the top-level prefix
// ("public" or "private") and the remainder.
func splitFtpPath(cleaned string) (prefix, rest string) {
    parts := strings.SplitN(cleaned, "/", 2)
    if len(parts) == 0 || parts[0] == "" {
        return "", ""
    }
    prefix = parts[0]
    if len(parts) > 1 {
        rest = parts[1]
    }
    return
}

// ftpRootName extracts the logical root name from a virtual FTP path.
// "public/<rootName>/sub/file" -> "<rootName>"
// "private/<userUUID>/sub/file" -> "private"
// "public/<rootName>"            -> "<rootName>"
// Returns "" if the path does not belong to a known root.
func ftpRootName(name string) string {
    cleaned := cleanFtpPath(name)
    prefix, rest := splitFtpPath(cleaned)
    switch prefix {
    case "public":
        sub := strings.SplitN(rest, "/", 2)
        if len(sub) == 0 || sub[0] == "" {
            return ""
        }
        return sub[0]
    case "private":
        return "private"
    }
    return ""
}

// resolvePublicPath resolves a "public/..." virtual path to a real filesystem path.
// rest is the path after "public/", e.g., "photos/subdir/file.txt".
func (fs *MultiRootFs) resolvePublicPath(rest string) (string, error) {
    subParts := strings.SplitN(rest, "/", 2)
    rootName := subParts[0]
    rootPath, ok := fs.rootNames[rootName]
    if !ok {
        return "", os.ErrPermission
    }
    if len(subParts) > 1 {
        return filepath.Join(rootPath, subParts[1]), nil
    }
    return rootPath, nil
}

// resolvePrivatePath resolves a "private/..." virtual path to a real filesystem path.
// rest is the path after "private/", e.g., "myfile.txt" or "subdir/myfile.txt".
func (fs *MultiRootFs) resolvePrivatePath(rest string) (string, error) {
    if fs.privateDir == "" || fs.userUUID == "" {
        return "", os.ErrPermission
    }
    userDir := filepath.Join(fs.privateDir, "users", fs.userUUID)
    if rest != "" {
        return filepath.Join(userDir, rest), nil
    }
    return userDir, nil
}

// resolvePath converts a virtual path to a real filesystem path.
func (fs *MultiRootFs) resolvePath(virtualPath string) (string, error) {
    cleaned := cleanFtpPath(virtualPath)
    if strings.HasPrefix(cleaned, "..") || path.IsAbs(cleaned) {
        return "", os.ErrPermission
    }
    if cleaned == "" || cleaned == "." {
        return "", nil // virtual root — handled specially
    }

    prefix, rest := splitFtpPath(cleaned)
    switch prefix {
    case "public":
        if rest == "" {
            return "", os.ErrPermission // /public alone is a virtual directory
        }
        return fs.resolvePublicPath(rest)
    case "private":
        return fs.resolvePrivatePath(rest)
    default:
        return "", os.ErrPermission
    }
}

// ---- namespace helpers ----

// getNamespace returns the namespace ("public", "private", or "") for a virtual path.
func (fs *MultiRootFs) getNamespace(virtualPath string) string {
    cleaned := cleanFtpPath(virtualPath)
    prefix, _ := splitFtpPath(cleaned)
    return prefix
}

// isPublicPath reports whether the virtual path belongs to the public namespace.
func (fs *MultiRootFs) isPublicPath(virtualPath string) bool {
    return fs.getNamespace(virtualPath) == "public"
}

// validateExtension checks filename against allowedExtensions. Returns nil if allowed.
func (fs *MultiRootFs) validateExtension(filename string) error {
    if len(fs.allowedExtensions) == 0 {
        return nil
    }
    ext := strings.ToLower(filepath.Ext(filename))
    if ext == "" || len(ext) <= 1 {
        return fmt.Errorf("禁止上传无扩展名的文件")
    }
    ext = ext[1:] // strip leading dot
    for _, allowed := range fs.allowedExtensions {
        if allowed == ext {
            return nil
        }
    }
    return fmt.Errorf("不允许的文件类型: .%s", ext)
}

// ---- afero.Fs implementation ----

func (fs *MultiRootFs) Create(name string) (afero.File, error) {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return nil, err
    }
    // Validate extension for public namespace
    if fs.isPublicPath(name) {
        if err := fs.validateExtension(filepath.Base(name)); err != nil {
            return nil, err
        }
    }
    f, err := afero.NewOsFs().Create(realPath)
    if err != nil {
        return nil, err
    }
    // Record operation
    if fs.recordFn != nil {
        fs.recordFn("upload", name, filepath.Base(name), ftpRootName(name), "", fs.clientIP, fs.userUUID, 0)
    }
    // Wrap for size limiting in public namespace
    if fs.isPublicPath(name) && fs.maxFileSize > 0 {
        return &sizeLimitedFile{File: f, maxSize: fs.maxFileSize, path: realPath}, nil
    }
    return f, nil
}

func (fs *MultiRootFs) Mkdir(name string, perm os.FileMode) error {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return err
    }
    return afero.NewOsFs().Mkdir(realPath, perm)
}

func (fs *MultiRootFs) MkdirAll(name string, perm os.FileMode) error {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return err
    }
    return afero.NewOsFs().MkdirAll(realPath, perm)
}

func (fs *MultiRootFs) Open(name string) (afero.File, error) {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return nil, err
    }
    return afero.NewOsFs().Open(realPath)
}

func (fs *MultiRootFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return nil, err
    }
    // Validate extension for write operations in public namespace
    isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0
    if isWrite && fs.isPublicPath(name) {
        if err := fs.validateExtension(filepath.Base(name)); err != nil {
            return nil, err
        }
    }
    f, err := afero.NewOsFs().OpenFile(realPath, flag, perm)
    if err != nil {
        return nil, err
    }
    // Wrap for size limiting in public namespace on write operations
    if isWrite && fs.isPublicPath(name) && fs.maxFileSize > 0 {
        wrapped := &sizeLimitedFile{File: f, maxSize: fs.maxFileSize, path: realPath}
        return wrapped, nil
    }
    return f, nil
}

func (fs *MultiRootFs) Remove(name string) error {
    // Block anonymous users from deleting public files
    if fs.isPublicPath(name) && fs.userUUID == "" {
        return os.ErrPermission
    }
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return err
    }
    err = afero.NewOsFs().Remove(realPath)
    if err == nil && fs.recordFn != nil {
        fs.recordFn("delete", name, filepath.Base(name), ftpRootName(name), "", fs.clientIP, fs.userUUID, 0)
    }
    return err
}

func (fs *MultiRootFs) RemoveAll(name string) error {
    // Block anonymous users from deleting public directories
    if fs.isPublicPath(name) && fs.userUUID == "" {
        return os.ErrPermission
    }
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return err
    }
    err = afero.NewOsFs().RemoveAll(realPath)
    if err == nil && fs.recordFn != nil {
        fs.recordFn("delete", name, filepath.Base(name), ftpRootName(name), "", fs.clientIP, fs.userUUID, 0)
    }
    return err
}

func (fs *MultiRootFs) Rename(oldName, newName string) error {
    oldPath, err := fs.resolvePath(oldName)
    if err != nil {
        return err
    }
    newPath, err := fs.resolvePath(newName)
    if err != nil {
        return err
    }
    return afero.NewOsFs().Rename(oldPath, newPath)
}

func (fs *MultiRootFs) Stat(name string) (os.FileInfo, error) {
    cleaned := cleanFtpPath(name)
    if strings.HasPrefix(cleaned, "..") {
        return nil, os.ErrPermission
    }

    // Virtual root
    if cleaned == "" || cleaned == "." {
        return &virtualDirInfo{name: "/"}, nil
    }

    prefix, rest := splitFtpPath(cleaned)
    if rest == "" {
        // Top-level virtual dirs (public, private)
        if prefix == "public" || prefix == "private" {
            return &virtualDirInfo{name: prefix}, nil
        }
        return nil, os.ErrNotExist
    }

    // For public paths, check if rest is just a root name (no sub-path)
    if prefix == "public" {
        subParts := strings.SplitN(rest, "/", 2)
        if len(subParts) == 1 {
            // Stat on a root name directory (/public/photos)
            rootPath, ok := fs.rootNames[subParts[0]]
            if ok {
                fi, err := afero.NewOsFs().Stat(rootPath)
                if err != nil {
                    // Root path doesn't exist on disk; return virtual dir
                    return &virtualDirInfo{name: subParts[0]}, nil
                }
                return fi, nil
            }
            return nil, os.ErrNotExist
        }
    }

    realPath, err := fs.resolvePath(name)
    if err != nil {
        return nil, err
    }
    return afero.NewOsFs().Stat(realPath)
}

func (fs *MultiRootFs) Name() string {
    return "MultiRootFs"
}

func (fs *MultiRootFs) Chmod(name string, mode os.FileMode) error {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return err
    }
    return afero.NewOsFs().Chmod(realPath, mode)
}

func (fs *MultiRootFs) Chtimes(name string, atime, mtime time.Time) error {
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return err
    }
    return afero.NewOsFs().Chtimes(realPath, atime, mtime)
}

// ---- virtual directory listing for FTP ----

// readDir returns a sorted list of directory entries for the given virtual path.
func (fs *MultiRootFs) readDir(name string) ([]os.FileInfo, error) {
    cleaned := cleanFtpPath(name)
    if strings.HasPrefix(cleaned, "..") {
        return nil, os.ErrPermission
    }

    // Virtual root → list public/ and private/
    if cleaned == "" || cleaned == "." {
        entries := []os.FileInfo{
            &virtualDirInfo{name: "public"},
        }
        if fs.privateDir != "" && fs.userUUID != "" {
            entries = append(entries, &virtualDirInfo{name: "private"})
        }
        return entries, nil
    }

    prefix, rest := splitFtpPath(cleaned)

    // /public → list root names (photos, documents, etc.)
    if prefix == "public" && rest == "" {
        entries := make([]os.FileInfo, 0, len(fs.rootNames))
        for name := range fs.rootNames {
            entries = append(entries, &virtualDirInfo{name: name})
        }
        sort.Slice(entries, func(i, j int) bool {
            return entries[i].Name() < entries[j].Name()
        })
        return entries, nil
    }

    // /private → list user's private directory
    if prefix == "private" && rest == "" {
        if fs.privateDir == "" || fs.userUUID == "" {
            return nil, os.ErrPermission
        }
        userDir := filepath.Join(fs.privateDir, "users", fs.userUUID)
        f, err := afero.NewOsFs().Open(userDir)
        if err != nil {
            return nil, err
        }
        defer f.Close()
        return f.Readdir(-1)
    }

    // Fall through to actual filesystem for deeper paths
    realPath, err := fs.resolvePath(name)
    if err != nil {
        return nil, err
    }
    f, err := afero.NewOsFs().Open(realPath)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    return f.Readdir(-1)
}

// ClientDriver returns an afero.Fs suitable for use as an FTP client driver.
// This wraps the MultiRootFs to add directory listing support via Readdir.
// For FTP usage, the driver should call readDir when listing directories.
func (fs *MultiRootFs) ClientDriver() afero.Fs {
    return &ftpDriverAdapter{fs: fs}
}

// ftpDriverAdapter wraps MultiRootFs so that Open on directories returns
// a file whose Readdir uses the virtual directory listing.
type ftpDriverAdapter struct {
    fs *MultiRootFs
}

func (a *ftpDriverAdapter) Create(name string) (afero.File, error)    { return a.fs.Create(name) }
func (a *ftpDriverAdapter) Mkdir(name string, perm os.FileMode) error { return a.fs.Mkdir(name, perm) }
func (a *ftpDriverAdapter) MkdirAll(name string, perm os.FileMode) error {
    return a.fs.MkdirAll(name, perm)
}

func (a *ftpDriverAdapter) Open(name string) (afero.File, error) {
    cleaned := cleanFtpPath(name)

    // Virtual root
    if cleaned == "" || cleaned == "." {
        return &virtualDir{fs: a.fs, name: name}, nil
    }

    prefix, rest := splitFtpPath(cleaned)

    // Top-level virtual dirs (/public, /private)
    if rest == "" {
        if prefix == "public" || (prefix == "private" && a.fs.privateDir != "" && a.fs.userUUID != "") {
            return &virtualDir{fs: a.fs, name: name}, nil
        }
        return nil, os.ErrPermission
    }

    // /public/<rootName> or deeper — check if this is a root-name level virtual dir
    if prefix == "public" {
        subParts := strings.SplitN(rest, "/", 2)
        if len(subParts) == 1 {
            // Just a root name — virtual directory listing
            return &virtualDir{fs: a.fs, name: name}, nil
        }
    }

    return a.fs.Open(name)
}

func (a *ftpDriverAdapter) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
    return a.fs.OpenFile(name, flag, perm)
}
func (a *ftpDriverAdapter) Remove(name string) error                         { return a.fs.Remove(name) }
func (a *ftpDriverAdapter) RemoveAll(name string) error                      { return a.fs.RemoveAll(name) }
func (a *ftpDriverAdapter) Rename(oldName, newName string) error             { return a.fs.Rename(oldName, newName) }
func (a *ftpDriverAdapter) Stat(name string) (os.FileInfo, error)            { return a.fs.Stat(name) }
func (a *ftpDriverAdapter) Name() string                                     { return "MultiRootFs" }
func (a *ftpDriverAdapter) Chmod(name string, mode os.FileMode) error        { return a.fs.Chmod(name, mode) }
func (a *ftpDriverAdapter) Chown(_ string, _, _ int) error                  { return os.ErrPermission }
func (a *ftpDriverAdapter) Chtimes(name string, atime, mtime time.Time) error {
    return a.fs.Chtimes(name, atime, mtime)
}

// virtualDir implements afero.File for virtual directories in the FTP filesystem.
type virtualDir struct {
    fs   *MultiRootFs
    name string
    pos  int
}

func (d *virtualDir) Close() error     { return nil }
func (d *virtualDir) Sync() error      { return nil }
func (d *virtualDir) Name() string { return d.name }

func (d *virtualDir) Read(p []byte) (int, error) {
    return 0, io.EOF
}
func (d *virtualDir) ReadAt(p []byte, off int64) (int, error) {
    return 0, io.EOF
}
func (d *virtualDir) Seek(offset int64, whence int) (int64, error) {
    return 0, nil
}
func (d *virtualDir) Write(p []byte) (int, error) {
    return 0, os.ErrPermission
}
func (d *virtualDir) WriteAt(p []byte, off int64) (int, error) {
    return 0, os.ErrPermission
}
func (d *virtualDir) WriteString(s string) (ret int, err error) {
    return 0, os.ErrPermission
}

func (d *virtualDir) Stat() (os.FileInfo, error) {
    return d.fs.Stat(d.name)
}

func (d *virtualDir) Readdir(count int) ([]os.FileInfo, error) {
    entries, err := d.fs.readDir(d.name)
    if err != nil {
        return nil, err
    }
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
    return entries, nil
}

func (d *virtualDir) Readdirnames(n int) ([]string, error) {
    entries, err := d.Readdir(n)
    if err != nil {
        return nil, err
    }
    names := make([]string, len(entries))
    for i, e := range entries {
        names[i] = e.Name()
    }
    return names, nil
}

func (d *virtualDir) Truncate(size int64) error {
    return os.ErrPermission
}

// sizeLimitedFile wraps an afero.File and enforces a maximum file size on writes.
type sizeLimitedFile struct {
    afero.File
    maxSize int64
    written int64
    path    string
}

func (f *sizeLimitedFile) Write(p []byte) (int, error) {
    if f.maxSize > 0 && f.written+int64(len(p)) > f.maxSize {
        return 0, fmt.Errorf("文件大小超过最大限制 (%d bytes)", f.maxSize)
    }
    n, err := f.File.Write(p)
    f.written += int64(n)
    return n, err
}

func (f *sizeLimitedFile) WriteAt(p []byte, off int64) (int, error) {
    if f.maxSize > 0 && off+int64(len(p)) > f.maxSize {
        return 0, fmt.Errorf("文件大小超过最大限制 (%d bytes)", f.maxSize)
    }
    n, err := f.File.WriteAt(p, off)
    if off+int64(n) > f.written {
        f.written = off + int64(n)
    }
    return n, err
}

func (f *sizeLimitedFile) WriteString(s string) (ret int, err error) {
    if f.maxSize > 0 && f.written+int64(len(s)) > f.maxSize {
        return 0, fmt.Errorf("文件大小超过最大限制 (%d bytes)", f.maxSize)
    }
    ret, err = f.File.WriteString(s)
    f.written += int64(ret)
    return ret, err
}

// virtualDirInfo implements os.FileInfo for virtual directories.
type virtualDirInfo struct {
    name string
}

func (d *virtualDirInfo) Name() string      { return d.name }
func (d *virtualDirInfo) Size() int64       { return 0 }
func (d *virtualDirInfo) Mode() os.FileMode { return os.ModeDir | 0755 }
func (d *virtualDirInfo) ModTime() time.Time { return time.Now() }
func (d *virtualDirInfo) IsDir() bool       { return true }
func (d *virtualDirInfo) Sys() interface{}  { return nil }
