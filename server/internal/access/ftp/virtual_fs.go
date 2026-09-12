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

	"fuzhan/internal/accessguard"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// OperationRecordFunc 操作记录回调函数
// rootName 是 filePath 所属的逻辑根名（来自配置的 rootNames key，或 "private"），
// filePath 为带前导 / 的、不含 rootName 的相对路径（与索引表 file_path 约定一致），
// 用于让调用方构造 FullPath（"/" + rootName + filePath）。
type OperationRecordFunc func(action, filePath, fileName, rootName, fileTypeTag, clientIP, userID string, fileSize int64)

// MultiRootFs implements afero.Fs for FTP with a virtual directory structure:
//
//	/
//	  public/        ← shared root dirs (anonymous + authenticated)
//	  private/       ← user private storage (authenticated only, absent for anon)
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
	backend           FtpBackend // 服务层注入：权限/索引/配额（nil 时权限校验保守拒绝管理操作）
}

// NewMultiRootFs creates a MultiRootFs. For anonymous users, privateDir and
// userUUID should be empty; the private/ directory will not appear.
//
// clientIP is used for operation recording. recordFn may be nil to skip recording.
// backend 为服务层注入的权限/索引/配额能力，可为 nil（此时管理类操作一律拒绝）。
func NewMultiRootFs(rootNames map[string]string, privateDir, userUUID, clientIP string, recordFn OperationRecordFunc, backend FtpBackend) *MultiRootFs {
	cfg := appconfig.GlobalConfig
	return &MultiRootFs{
		rootNames:         rootNames,
		privateDir:        privateDir,
		userUUID:          userUUID,
		clientIP:          clientIP,
		allowedExtensions: cfg.Storage.AllowedExtensions,
		maxFileSize:       cfg.Upload.MaxFileSize,
		recordFn:          recordFn,
		backend:           backend,
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

// recordPath 将虚拟路径拆为 (rootName, filePath)：
// filePath 为带前导 / 的、不含 rootName 的相对路径（与索引表/操作记录约定一致）。
// "public/files/sub/a.txt" -> ("files", "/sub/a.txt")
// "private/sub/a.txt"      -> ("private", "/sub/a.txt")
func (fs *MultiRootFs) recordPath(virtualPath string) (rootName, filePath string) {
	cleaned := cleanFtpPath(virtualPath)
	prefix, rest := splitFtpPath(cleaned)
	switch prefix {
	case "public":
		sub := strings.SplitN(rest, "/", 2)
		if len(sub) == 0 || sub[0] == "" {
			return "", ""
		}
		if len(sub) < 2 || sub[1] == "" {
			return sub[0], "/"
		}
		return sub[0], "/" + sub[1]
	case "private":
		if rest == "" {
			return "private", "/"
		}
		return "private", "/" + rest
	}
	return "", ""
}

// isPrivatePath reports whether the virtual path belongs to the private namespace.
func (fs *MultiRootFs) isPrivatePath(virtualPath string) bool {
	return fs.getNamespace(virtualPath) == "private"
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
	// 拒绝反斜杠分隔符：FTP 虚拟路径统一使用 "/" 作为分隔符，反斜杠在 Windows 上会被
	// filepath.Join 当作路径分隔符处理，可借此进行目录遍历逃逸（如 "private/..\..\evil"）。
	if strings.Contains(cleaned, `\`) {
		return "", os.ErrPermission
	}
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

// ---- permission helpers ----

// canCreatePublic 公共目录上传总开关校验（与 Web 上传一致，不豁免管理员）。
// backend 为 nil 时保持旧行为（不校验）。
func (fs *MultiRootFs) canCreatePublic() bool {
	return fs.backend == nil || fs.backend.UploadEnabled()
}

// canManagePublic 判定对 public 下已有文件执行覆盖/删除/重命名是否允许：
// 匿名一律拒绝；管理员放行；否则仅当索引记录上传者 IP 与当前客户端 IP 一致时放行。
// backend 为 nil 时一律拒绝（保守安全）。
func (fs *MultiRootFs) canManagePublic(rootName, relPath string) bool {
	if fs.backend == nil || rootName == "" || relPath == "" || relPath == "/" {
		return false
	}
	if fs.userUUID == "" {
		return false
	}
	if fs.backend.IsAdmin(fs.userUUID) {
		return true
	}
	ip := fs.backend.PublicUploaderIP(rootName, relPath)
	return ip != "" && ip == fs.clientIP
}

// existsOnDisk 判断磁盘上是否存在指定文件（目录视为存在）。
func existsOnDisk(realPath string) bool {
	_, err := afero.NewOsFs().Stat(realPath)
	return err == nil
}

// checkDiskSpace 创建/写入前的磁盘空间与配额检查（backend 为 nil 时跳过）。
func (fs *MultiRootFs) checkDiskSpace(realPath string, private bool) error {
	if fs.backend == nil {
		return nil
	}
	// required=1 仅验证分区可写；FTP 流式传输无法预知最终大小，
	// 真实大小上限由 sizeLimitedFile（偏移感知）兜底。
	if err := fs.backend.CheckDiskSpace(filepath.Dir(realPath), 1); err != nil {
		return err
	}
	if private && fs.userUUID != "" {
		if err := fs.backend.CheckPrivateQuota(fs.userUUID, 0); err != nil {
			return err
		}
	}
	return nil
}

// ---- afero.Fs implementation ----

func (fs *MultiRootFs) Create(name string) (afero.File, error) {
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return nil, err
	}
	isPublic := fs.isPublicPath(name)
	isPrivate := fs.isPrivatePath(name)
	if isPublic {
		if !fs.canCreatePublic() {
			return nil, os.ErrPermission
		}
		if err := fs.validateExtension(filepath.Base(name)); err != nil {
			return nil, err
		}
	}
	if err := fs.checkDiskSpace(realPath, isPrivate); err != nil {
		return nil, err
	}
	f, err := afero.NewOsFs().Create(realPath)
	if err != nil {
		return nil, err
	}
	// 包装：传输完成后（Close）记录操作并同步索引/触发哈希
	return fs.wrapWriteFile(f, name, realPath, isPublic, isPrivate)
}

func (fs *MultiRootFs) Mkdir(name string, perm os.FileMode) error {
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return err
	}
	if err := afero.NewOsFs().Mkdir(realPath, perm); err != nil {
		return err
	}
	fs.recordMkdir(name)
	return nil
}

func (fs *MultiRootFs) MkdirAll(name string, perm os.FileMode) error {
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return err
	}
	if err := afero.NewOsFs().MkdirAll(realPath, perm); err != nil {
		return err
	}
	fs.recordMkdir(name)
	return nil
}

// recordMkdir 记录创建目录操作（与 WebDAV 行为一致）。
func (fs *MultiRootFs) recordMkdir(name string) {
	if fs.recordFn == nil {
		return
	}
	rootName, relPath := fs.recordPath(name)
	fs.recordFn("mkdir", relPath, filepath.Base(name), rootName, "", fs.clientIP, fs.userUUID, 0)
}

func (fs *MultiRootFs) Open(name string) (afero.File, error) {
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return nil, err
	}
	f, err := afero.NewOsFs().Open(realPath)
	if err != nil {
		return nil, err
	}
	// RETR 下载记录（仅文件，非目录）+ 匿名下载限流
	fi, serr := f.Stat()
	if serr == nil && !fi.IsDir() {
		if derr := fs.recordDownload(name, fi.Size()); derr != nil {
			f.Close()
			return nil, derr
		}
	}
	return f, nil
}

func (fs *MultiRootFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return nil, err
	}
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	isPublic := fs.isPublicPath(name)
	isPrivate := fs.isPrivatePath(name)

	if isWrite {
		if isPublic {
			if !fs.canCreatePublic() {
				return nil, os.ErrPermission
			}
			if err := fs.validateExtension(filepath.Base(name)); err != nil {
				return nil, err
			}
			// 覆盖已有文件需管理权限（上传者 IP 一致或管理员）
			if existsOnDisk(realPath) {
				rootName, relPath := fs.recordPath(name)
				if !fs.canManagePublic(rootName, relPath) {
					return nil, os.ErrPermission
				}
			}
		}
		if isPrivate {
			// 与 Web 私有上传一致：不允许覆盖已有文件（每次上传生成独立分享码）
			if existsOnDisk(realPath) {
				return nil, os.ErrPermission
			}
		}
		if err := fs.checkDiskSpace(realPath, isPrivate); err != nil {
			return nil, err
		}
	} else {
		// 读模式（RETR）：打开成功后记录下载 + 匿名下载限流
		f, err := afero.NewOsFs().OpenFile(realPath, flag, perm)
		if err != nil {
			return nil, err
		}
		if fi, serr := f.Stat(); serr == nil && !fi.IsDir() {
			if derr := fs.recordDownload(name, fi.Size()); derr != nil {
				f.Close()
				return nil, derr
			}
		}
		return f, nil
	}

	f, err := afero.NewOsFs().OpenFile(realPath, flag, perm)
	if err != nil {
		return nil, err
	}
	return fs.wrapWriteFile(f, name, realPath, isPublic, isPrivate)
}

// recordDownload 记录下载操作（含真实文件大小）。
// 匿名 FTP 下载是免认证下载面，按共享下载限流配置做 per-IP 频率限制（accessguard，
// max_requests=0/未配置时无限）；认证用户下载走登录态，不参与匿名限流。
func (fs *MultiRootFs) recordDownload(name string, size int64) error {
	if fs.userUUID == "" && !accessguard.Acquire(accessguard.SCOPE_FTP, fs.clientIP) {
		return os.ErrPermission
	}
	if fs.recordFn == nil {
		return nil
	}
	rootName, relPath := fs.recordPath(name)
	fs.recordFn("download", relPath, filepath.Base(name), rootName, "", fs.clientIP, fs.userUUID, size)
	return nil
}

// wrapWriteFile 包装写文件：传输完成后（Close）记录上传操作并同步索引/触发哈希；
// 同时叠加偏移感知的大小限制（public/private 一致）。
func (fs *MultiRootFs) wrapWriteFile(f afero.File, name, realPath string, isPublic, isPrivate bool) (afero.File, error) {
	var limited afero.File = f
	if fs.maxFileSize > 0 {
		limited = &sizeLimitedFile{File: f, maxSize: fs.maxFileSize}
	}
	rec := &recordingFile{
		File: limited,
		path: realPath,
	}
	rootName, relPath := fs.recordPath(name)
	rec.onClose = func(size int64) error {
		if fs.recordFn != nil {
			fs.recordFn("upload", relPath, filepath.Base(name), rootName, "", fs.clientIP, fs.userUUID, size)
		}
		if fs.backend == nil {
			return nil
		}
		if isPublic {
			fs.backend.SyncPublicFile(rootName, relPath, fs.clientIP)
			return nil
		}
		if isPrivate && fs.userUUID != "" {
			if err := fs.backend.AddPrivateFile(fs.userUUID, relPath, filepath.Base(name), size); err != nil {
				utils.Warn("FTP 私有上传写入索引失败",
					utils.String("path", relPath),
					utils.Err(err))
				return err
			}
		}
		return nil
	}
	return rec, nil
}

func (fs *MultiRootFs) Remove(name string) error {
	if fs.isPublicPath(name) {
		rootName, relPath := fs.recordPath(name)
		if !fs.canManagePublic(rootName, relPath) {
			return os.ErrPermission
		}
	}
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return err
	}
	if err := afero.NewOsFs().Remove(realPath); err != nil {
		return err
	}
	fs.recordRemove(name)
	return nil
}

func (fs *MultiRootFs) RemoveAll(name string) error {
	if fs.isPublicPath(name) {
		rootName, relPath := fs.recordPath(name)
		if !fs.canManagePublic(rootName, relPath) {
			return os.ErrPermission
		}
	}
	realPath, err := fs.resolvePath(name)
	if err != nil {
		return err
	}
	if err := afero.NewOsFs().RemoveAll(realPath); err != nil {
		return err
	}
	fs.recordRemove(name)
	return nil
}

// recordRemove 记录删除操作并同步索引（公开软删 file_records_public，私有软删 file_records_private）。
func (fs *MultiRootFs) recordRemove(name string) {
	isPublic := fs.isPublicPath(name)
	rootName, relPath := fs.recordPath(name)
	if fs.recordFn != nil {
		fs.recordFn("delete", relPath, filepath.Base(name), rootName, "", fs.clientIP, fs.userUUID, 0)
	}
	if fs.backend == nil {
		return
	}
	if isPublic {
		fs.backend.RemovePublicFile(rootName, relPath)
	} else if fs.isPrivatePath(name) && fs.userUUID != "" {
		fs.backend.SoftDeletePrivateFile(fs.userUUID, relPath)
	}
}

func (fs *MultiRootFs) Rename(oldName, newName string) error {
	oldNS := fs.getNamespace(oldName)
	newNS := fs.getNamespace(newName)
	// 禁止跨命名空间（public↔private 互移会窃取/泄露文件）
	if oldNS == "" || oldNS != newNS {
		return os.ErrPermission
	}
	oldPath, err := fs.resolvePath(oldName)
	if err != nil {
		return err
	}
	newPath, err := fs.resolvePath(newName)
	if err != nil {
		return err
	}

	oldRoot, oldRel := fs.recordPath(oldName)
	newRoot, newRel := fs.recordPath(newName)

	if oldNS == "public" {
		// 仅同根内重命名；匿名一律禁止；管理员/上传者本人放行
		if oldRoot != newRoot {
			return os.ErrPermission
		}
		if !fs.canManagePublic(oldRoot, oldRel) {
			return os.ErrPermission
		}
	}
	// 目标已存在（文件或目录）→ 拒绝覆盖
	if existsOnDisk(newPath) {
		return os.ErrExist
	}
	if err := afero.NewOsFs().Rename(oldPath, newPath); err != nil {
		return err
	}
	if fs.recordFn != nil {
		fs.recordFn("rename", newRel, filepath.Base(newName), newRoot, "", fs.clientIP, fs.userUUID, 0)
	}
	if fs.backend == nil {
		return nil
	}
	if oldNS == "public" {
		fs.backend.MovePublicFile(oldRoot, oldRel, newRel)
	} else if oldNS == "private" && fs.userUUID != "" {
		fs.backend.RenamePrivateFile(fs.userUUID, oldRel, newRel)
	}
	return nil
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
		switch prefix {
		case "public":
			return &virtualDirInfo{name: prefix}, nil
		case "private":
			// 私有未启用或匿名用户时，不应暴露私有存储的存在性
			if fs.privateDir == "" || fs.userUUID == "" {
				return nil, os.ErrPermission
			}
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
				if err != nil || !fi.IsDir() {
					// 根目录在磁盘上不存在/不可访问时按不存在处理（避免幽灵目录）
					return nil, os.ErrNotExist
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
		// 只列出磁盘上真实存在的根目录，避免幽灵目录
		entries := make([]os.FileInfo, 0, len(fs.rootNames))
		for name, rootPath := range fs.rootNames {
			if fi, err := afero.NewOsFs().Stat(rootPath); err == nil && fi.IsDir() {
				entries = append(entries, &virtualDirInfo{name: name})
			}
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
			// Just a root name — virtual directory listing (仅磁盘存在的根)
			if rootPath, ok := a.fs.rootNames[subParts[0]]; ok {
				if fi, err := afero.NewOsFs().Stat(rootPath); err == nil && fi.IsDir() {
					return &virtualDir{fs: a.fs, name: name}, nil
				}
			}
			return nil, os.ErrNotExist
		}
	}

	return a.fs.Open(name)
}

func (a *ftpDriverAdapter) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return a.fs.OpenFile(name, flag, perm)
}
func (a *ftpDriverAdapter) Remove(name string) error    { return a.fs.Remove(name) }
func (a *ftpDriverAdapter) RemoveAll(name string) error { return a.fs.RemoveAll(name) }
func (a *ftpDriverAdapter) Rename(oldName, newName string) error {
	return a.fs.Rename(oldName, newName)
}
func (a *ftpDriverAdapter) Stat(name string) (os.FileInfo, error)     { return a.fs.Stat(name) }
func (a *ftpDriverAdapter) Name() string                              { return "MultiRootFs" }
func (a *ftpDriverAdapter) Chmod(name string, mode os.FileMode) error { return a.fs.Chmod(name, mode) }
func (a *ftpDriverAdapter) Chown(_ string, _, _ int) error            { return os.ErrPermission }
func (a *ftpDriverAdapter) Chtimes(name string, atime, mtime time.Time) error {
	return a.fs.Chtimes(name, atime, mtime)
}

// virtualDir implements afero.File for virtual directories in the FTP filesystem.
type virtualDir struct {
	fs   *MultiRootFs
	name string
	pos  int
}

func (d *virtualDir) Close() error { return nil }
func (d *virtualDir) Sync() error  { return nil }
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
// offset 跟踪底层文件的逻辑写入位置（含 Seek），REST 续传偏移也纳入上限判定，
// 避免通过 REST 大偏移 + 少量写入生成超限稀疏文件。
type sizeLimitedFile struct {
	afero.File
	maxSize int64
	offset  int64 // 当前写入偏移（Seek/Write/WriteAt 同步维护）
}

func (f *sizeLimitedFile) Seek(offset int64, whence int) (int64, error) {
	n, err := f.File.Seek(offset, whence)
	if err == nil {
		f.offset = n
	}
	return n, err
}

func (f *sizeLimitedFile) Write(p []byte) (int, error) {
	if f.maxSize > 0 && f.offset+int64(len(p)) > f.maxSize {
		return 0, fmt.Errorf("文件大小超过最大限制 (%d bytes)", f.maxSize)
	}
	n, err := f.File.Write(p)
	f.offset += int64(n)
	return n, err
}

func (f *sizeLimitedFile) WriteAt(p []byte, off int64) (int, error) {
	if f.maxSize > 0 && off+int64(len(p)) > f.maxSize {
		return 0, fmt.Errorf("文件大小超过最大限制 (%d bytes)", f.maxSize)
	}
	n, err := f.File.WriteAt(p, off)
	if off+int64(n) > f.offset {
		f.offset = off + int64(n)
	}
	return n, err
}

func (f *sizeLimitedFile) WriteString(s string) (ret int, err error) {
	return f.Write([]byte(s))
}

// recordingFile 包装写文件：传输完成后（Close）以真实大小执行 onClose 回调
// （记录上传操作、同步索引、触发哈希），避免在创建时留下 0 大小的失真记录。
// onClose 返回错误时（如私有配额超限）回传 FTP 客户端，使 STOR 以失败结束。
type recordingFile struct {
	afero.File
	path    string
	onClose func(size int64) error
	closed  bool
}

func (f *recordingFile) Close() error {
	if f.closed {
		return f.File.Close()
	}
	f.closed = true
	err := f.File.Close()
	if err != nil {
		return err
	}
	size := int64(0)
	if fi, serr := afero.NewOsFs().Stat(f.path); serr == nil {
		size = fi.Size()
	}
	if f.onClose != nil {
		return f.onClose(size)
	}
	return nil
}

// virtualDirInfo implements os.FileInfo for virtual directories.
type virtualDirInfo struct {
	name string
}

func (d *virtualDirInfo) Name() string       { return d.name }
func (d *virtualDirInfo) Size() int64        { return 0 }
func (d *virtualDirInfo) Mode() os.FileMode  { return os.ModeDir | 0755 }
func (d *virtualDirInfo) ModTime() time.Time { return time.Now() }
func (d *virtualDirInfo) IsDir() bool        { return true }
func (d *virtualDirInfo) Sys() interface{}   { return nil }
