package ftp

import (
	"context"
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
	"fuzhan/internal/models"
	"fuzhan/internal/security"
	"fuzhan/internal/storage"
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
	backend           FtpBackend                 // 服务层注入：权限/索引/配额（nil 时权限校验保守拒绝管理操作）
	storages          map[string]storage.Storage // 公开多根目录的存储后端映射（rootName → Storage）
	ctx               context.Context            // 远端存储操作使用的上下文（存储实现内部使用）
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
		storages:          nil,
		ctx:               context.Background(),
	}
}

// SetStorages 注入公开多根目录的存储后端映射（rootName → Storage）。
// 本地后端可不注入（保持 rootNames 物理路径语义零变化）；远端后端（s3/minio/oss/webdav）
// 注入后使 FTP 的列表/读取/写入通过 Storage 接口访问远端对象存储。
func (fs *MultiRootFs) SetStorages(storages map[string]storage.Storage) {
	fs.storages = storages
}

// storageFor 返回 rootName 对应的远端存储后端；本地/未注入返回 nil, false。
// 与 index/webdav 的 storageFor 保持同一回退范式：Kind()=="local" 时回退物理路径。
func (fs *MultiRootFs) storageFor(rootName string) (storage.Storage, bool) {
	st, ok := fs.storages[rootName]
	if !ok || st == nil || st.Kind() == "local" {
		return nil, false
	}
	return st, true
}

// recordSecurity 记录一条 FTP 访问拒绝类安全事件（guard=ftp）。
// identity 取认证用户 UUID，匿名用户记为 "anonymous"。
func (fs *MultiRootFs) recordSecurity(eventType, severity, operation, target, reason string) {
	identity := fs.userUUID
	if identity == "" {
		identity = "anonymous"
	}
	if r := []rune(target); len(r) > 512 {
		target = string(r[:512])
	}
	security.Record(&models.SecurityEvent{
		EventType: eventType,
		Severity:  severity,
		ClientIP:  fs.clientIP,
		Identity:  identity,
		Operation: operation,
		Target:    target,
		Guard:     models.GuardFTP,
		Reason:    reason,
	})
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

// localWriteRoot 返回本地写入的物理根目录（用于符号链接校验）；远端根返回空。
func (fs *MultiRootFs) localWriteRoot(rootName string, isPublic bool) string {
	if isPublic {
		return fs.rootNames[rootName]
	}
	if fs.privateDir != "" && fs.userUUID != "" {
		return fs.privateDir
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

// publicRootPath 返回 public 下 rootName 对应的物理配置根路径（rootNames 的 value）。
// 远端模式下它是虚拟前缀，仅用于磁盘分区检测（CheckDiskSpace 依赖本地分区上的
// 某个可写路径），不代表真实存储位置。
func (fs *MultiRootFs) publicRootPath(rootName string) (string, bool) {
	rootPath, ok := fs.rootNames[rootName]
	return rootPath, ok
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
// op 为调用方操作名（create/mkdir/open/...），命中拒绝分支时以 "file."+op 记入安全事件。
func (fs *MultiRootFs) resolvePath(virtualPath string, op string) (string, error) {
	cleaned := cleanFtpPath(virtualPath)
	// 拒绝反斜杠分隔符：FTP 虚拟路径统一使用 "/" 作为分隔符，反斜杠在 Windows 上会被
	// filepath.Join 当作路径分隔符处理，可借此进行目录遍历逃逸（如 "private/..\..\evil"）。
	if strings.Contains(cleaned, `\`) {
		fs.recordSecurity(models.EventAccessForbidden, models.SeverityHigh, "file."+op, virtualPath, "路径穿越拒绝（反斜杠分隔符）")
		return "", os.ErrPermission
	}
	if strings.HasPrefix(cleaned, "..") || path.IsAbs(cleaned) {
		fs.recordSecurity(models.EventAccessForbidden, models.SeverityHigh, "file."+op, virtualPath, "路径穿越拒绝（上级目录或绝对路径）")
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
		fs.recordSecurity(models.EventAccessForbidden, models.SeverityHigh, "file."+op, virtualPath, "未知命名空间拒绝（非 public/private 顶层路径）")
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

// canCreatePublic 公共目录上传总开关 + 按目录开关校验（与 Web 上传一致，不豁免管理员）。
// backend 为 nil 时保留旧行为（不校验全局开关），但仍校验按目录配置。
// 注意：允许上传的子目录（如 /public/photos）在配置关闭上传时同样不可写。
func (fs *MultiRootFs) canCreatePublic(rootName string) bool {
	if fs.backend != nil && !fs.backend.UploadEnabled() {
		return false
	}
	return appconfig.PublicDirUploadAllowed(rootName)
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
// location 为物理磁盘路径（远端模式下传物理配置根路径，用于分区检测）；
// private 为 true 时额外校验私有配额。
func (fs *MultiRootFs) checkDiskSpace(location string, private bool) error {
	if fs.backend == nil {
		return nil
	}
	// required=1 仅验证分区可写；FTP 流式传输无法预知最终大小，
	// 真实大小上限由 sizeLimitedFile（偏移感知）兜底。
	if err := fs.backend.CheckDiskSpace(filepath.Dir(location), 1); err != nil {
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
	realPath, err := fs.resolvePath(name, "create")
	if err != nil {
		return nil, err
	}
	isPublic := fs.isPublicPath(name)
	isPrivate := fs.isPrivatePath(name)

	// 远端后端：本地临时缓存 + Close 一次性写入远端（对象存储无原地随机写/续传）。
	if isPublic {
		if st, remote := fs.storageFor(ftpRootName(name)); remote {
			return fs.openRemoteWrite(st, name, isPublic, isPrivate)
		}
	}

	if isPublic {
		if !fs.canCreatePublic(ftpRootName(name)) {
			return nil, os.ErrPermission
		}
		if err := fs.validateExtension(filepath.Base(name)); err != nil {
			return nil, err
		}
	}
	if isPrivate {
		// 与公共分支一致：私有上传同样校验扩展名白名单（大小由 wrapWriteFile 兜底）
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
	realPath, err := fs.resolvePath(name, "mkdir")
	if err != nil {
		return err
	}
	if fs.isPublicPath(name) {
		rootName, subPath := fs.recordPath(name)
		if st, remote := fs.storageFor(rootName); remote {
			// 远端后端：公开写入口校验（上传开关+按目录）+ Storage 接口建虚拟目录
			// （对象存储 s3/oss 无 op，远端 WebDAV 真实建目录）。
			if !fs.canCreatePublic(rootName) {
				return os.ErrPermission
			}
			if err := st.MkdirAll(fs.ctx, subPath); err != nil {
				return err
			}
			fs.recordMkdir(name)
			return nil
		}
	}
	if err := afero.NewOsFs().Mkdir(realPath, perm); err != nil {
		return err
	}
	fs.recordMkdir(name)
	return nil
}

func (fs *MultiRootFs) MkdirAll(name string, perm os.FileMode) error {
	realPath, err := fs.resolvePath(name, "mkdir")
	if err != nil {
		return err
	}
	if fs.isPublicPath(name) {
		rootName, subPath := fs.recordPath(name)
		if st, remote := fs.storageFor(rootName); remote {
			if !fs.canCreatePublic(rootName) {
				return os.ErrPermission
			}
			if err := st.MkdirAll(fs.ctx, subPath); err != nil {
				return err
			}
			fs.recordMkdir(name)
			return nil
		}
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
	realPath, err := fs.resolvePath(name, "open")
	if err != nil {
		return nil, err
	}
	isPublic := fs.isPublicPath(name)
	if isPublic {
		if st, remote := fs.storageFor(ftpRootName(name)); remote {
			return fs.openRemoteOpen(st, name)
		}
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

// openRemoteOpen 远端后端打开路径（Open 语义）：先 Stat 判存在与类型。
//   - 目录：返回 storageDirFile（Readdir 走 st.List，供 LIST/NLST/MLSD 使用）；
//   - 文件：返回 storageReadFile（RetrR 走 st.Open，Seek 按后端能力降级）。
func (fs *MultiRootFs) openRemoteOpen(st storage.Storage, name string) (afero.File, error) {
	_, subPath := fs.recordPath(name)
	info, err := st.Stat(fs.ctx, subPath)
	if err != nil {
		if storage.IsNotExist(err) {
			return nil, err
		}
		return nil, err
	}
	if info.IsDir {
		// 目录：直接列表（FTP LIST/NLST 经 driver.Open + Readdir）
		return &storageDirFile{st: st, ctx: fs.ctx, name: subPath}, nil
	}
	// 文件：先记录下载（含匿名免认证面限流），再打开流
	if derr := fs.recordDownload(name, info.Size); derr != nil {
		return nil, derr
	}
	rc, err := st.Open(fs.ctx, subPath)
	if err != nil {
		return nil, err
	}
	seeker, _ := rc.(io.Seeker)
	return &storageReadFile{
		rc:     rc,
		seeker: seeker,
		info:   &storageFileInfo{info: info},
	}, nil
}

// openRemoteWrite 远端后端写入入口（Create/STOR/APPE/REST 统一落点）。
//
// 策略（用户决策：后端不支持续传的存储先在本地缓存，完成后再写入后端）：
// 对象存储（s3/oss/webdav）没有原地随机写/续传（OpenWriter 缓冲到 Close 才整对象提交），
// 因此 FTP 的 STOR/APPE（追加）以及 REST（断点续传，需 Seek 到偏移）无法直接流式写远端。
// 本方法在本地临时目录缓存完整文件（支持全量 Seek/WriteAt/Write、追加语义，避免大文件
// 占满内存），FTP 传输全部落到缓存文件；Close 时经 st.OpenWriter 一次性写入远端后端，
// 再由后端在 Close 时以整对象提交；随后执行操作记录与索引同步。
func (fs *MultiRootFs) openRemoteWrite(st storage.Storage, name string, isPublic, isPrivate bool) (afero.File, error) {
	rootName, subPath := fs.recordPath(name)
	rootPath, ok := fs.publicRootPath(rootName)
	if !ok {
		return nil, os.ErrPermission
	}

	// 公开写入口：全局上传开关 + 按目录开关 + 扩展名白名单 + 磁盘空间。
	if isPublic && !fs.canCreatePublic(rootName) {
		return nil, os.ErrPermission
	}
	if err := fs.validateExtension(filepath.Base(name)); err != nil {
		return nil, err
	}
	// 远端模式下 rootPath 是虚拟前缀：CheckDiskSpace 需要本地物理分区的可写路径用于检测，
	// 这里传配置物理根路径的父目录（与 webdav openRemote 一致的降级）。
	if fs.backend != nil {
		if err := fs.backend.CheckDiskSpace(filepath.Dir(rootPath), 1); err != nil {
			return nil, err
		}
		if isPrivate && fs.userUUID != "" {
			if err := fs.backend.CheckPrivateQuota(fs.userUUID, 0); err != nil {
				return nil, err
			}
		}
	}

	// 本地临时缓存文件（系统临时目录）：提供完整的 Seek/WriteAt/追加语义，
	// 避免对象存储不支持原地随机写的问题；本地缓存作为 *os.File 本身就是 afero.File。
	cached, err := os.CreateTemp("", "ftp-*")
	if err != nil {
		return nil, err
	}

	// 叠加偏移感知的大小限制（与本地 wrapWriteFile 一致）。
	var limited afero.File = cached
	if fs.maxFileSize > 0 {
		limited = &sizeLimitedFile{File: cached, maxSize: fs.maxFileSize}
	}
	rec := &remoteRecordingFile{
		File:   limited,
		cached: cached,
		// Close 回调：写入远端（防覆盖 + 记录上传 + 索引同步）；本地缓存由 Close 统一清理。
		onClose: func(size int64) error {
			return fs.remoteUpload(cached, st, rootName, subPath, name, size, isPublic, isPrivate)
		},
	}
	return rec, nil
}

// remoteUpload 把本地缓存文件一次性写入远端后端，并完成操作记录与索引同步。
// 在 remoteRecordingFile.Close 中（缓存文件已 Seek 回开头）调用。
func (fs *MultiRootFs) remoteUpload(cached *os.File, st storage.Storage, rootName, subPath, name string, size int64, isPublic, isPrivate bool) error {
	// 远端写入前做防覆盖检查：公开目录仅允许新建（与 webdav 一致，替代本地 O_EXCL）。
	if isPublic {
		info, err := st.Stat(fs.ctx, subPath)
		if err == nil && !info.IsDir {
			return os.ErrPermission
		}
		if err != nil && !storage.IsNotExist(err) {
			return err
		}
	}
	if err := fs.recordUpload(name, size, isPublic, isPrivate); err != nil {
		return err
	}
	wc, err := st.OpenWriter(fs.ctx, subPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(wc, cached); err != nil {
		wc.Close()
		return err
	}
	return wc.Close()
}

// openRemoteRead 远端后端读取打开（OpenFile O_RDONLY 语义，RETR 用）。
// 先 Stat 判存在与大小（记录下载 + 匿名限流），再 st.Open 返回按后端能力
// 支持 Seek 的 storageReadFile（s3 完整支持 RETR+REST 续传；oss/webdav 降级顺序读）。
func (fs *MultiRootFs) openRemoteRead(st storage.Storage, name, subPath string) (afero.File, error) {
	info, err := st.Stat(fs.ctx, subPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir {
		return nil, os.ErrInvalid // RETR 目录无意义；上层会回退到列表
	}
	if derr := fs.recordDownload(name, info.Size); derr != nil {
		return nil, derr
	}
	rc, err := st.Open(fs.ctx, subPath)
	if err != nil {
		return nil, err
	}
	seeker, _ := rc.(io.Seeker)
	return &storageReadFile{
		rc:     rc,
		seeker: seeker,
		info:   &storageFileInfo{info: info},
	}, nil
}

func (fs *MultiRootFs) recordUpload(name string, size int64, isPublic, isPrivate bool) error {
	if fs.recordFn != nil {
		rootName, relPath := fs.recordPath(name)
		fs.recordFn("upload", relPath, filepath.Base(name), rootName, "", fs.clientIP, fs.userUUID, size)
	}
	if fs.backend == nil {
		return nil
	}
	if isPublic {
		rootName, relPath := fs.recordPath(name)
		fs.backend.SyncPublicFile(rootName, relPath, fs.clientIP)
		return nil
	}
	if isPrivate && fs.userUUID != "" {
		_, relPath := fs.recordPath(name)
		if err := fs.backend.AddPrivateFile(fs.userUUID, relPath, filepath.Base(name), size); err != nil {
			utils.FTPWarn("FTP 私有上传写入索引失败",
				utils.String("path", relPath),
				utils.Err(err))
			return err
		}
	}
	return nil
}

func (fs *MultiRootFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	realPath, err := fs.resolvePath(name, "openfile")
	if err != nil {
		return nil, err
	}
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	isPublic := fs.isPublicPath(name)
	isPrivate := fs.isPrivatePath(name)
	rootName := ftpRootName(name)
	_, subPath := fs.recordPath(name)

	if isWrite {
		// 远端后端：本地临时缓存 + Close 一次性写入远端（STOR/APPE/REST 全部落此分支）。
		if isPublic {
			if st, remote := fs.storageFor(rootName); remote {
				return fs.openRemoteWrite(st, name, isPublic, isPrivate)
			}
		}

		// 禁止上传写入符号链接路径（含 Windows 目录联接），避免穿透根目录
		if root := fs.localWriteRoot(rootName, isPublic); root != "" {
			if err := utils.EnsureNoSymlinkPath(root, realPath); err != nil {
				return nil, os.ErrPermission
			}
		}

		if isPublic {
			if !fs.canCreatePublic(rootName) {
				return nil, os.ErrPermission
			}
			if err := fs.validateExtension(filepath.Base(name)); err != nil {
				return nil, err
			}
			// 覆盖已有文件需管理权限（上传者 IP 一致或管理员）
			if existsOnDisk(realPath) {
				rootName, relPath := fs.recordPath(name)
				if !fs.canManagePublic(rootName, relPath) {
					fs.recordSecurity(models.EventAccessForbidden, models.SeverityMedium, "file.overwrite", name, "匿名或非归属用户尝试管理公开文件")
					return nil, os.ErrPermission
				}
			}
		}
		if isPrivate {
			// 与公共分支一致：私有上传同样校验扩展名白名单（大小由 wrapWriteFile 兜底）
			if err := fs.validateExtension(filepath.Base(name)); err != nil {
				return nil, err
			}
			// 与 Web 私有上传一致：不允许覆盖已有文件（每次上传生成独立分享码）
			if existsOnDisk(realPath) {
				return nil, os.ErrPermission
			}
			if err := fs.checkDiskSpace(realPath, true); err != nil {
				return nil, err
			}
		} else {
			if err := fs.checkDiskSpace(realPath, false); err != nil {
				return nil, err
			}
		}
	} else {
		// 读模式（RETR）：远端后端经 Storage 流式读取（Seek 按后端能力降级）。
		if isPublic {
			if st, remote := fs.storageFor(rootName); remote {
				return fs.openRemoteRead(st, name, subPath)
			}
		}
		// 本地后端（或私有路径）：打开成功后记录下载 + 匿名下载限流
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
		fs.recordSecurity(models.EventRateLimited, models.SeverityLow, "file.download", name, "匿名下载触发频率限制")
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
				utils.FTPWarn("FTP 私有上传写入索引失败",
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
	isPublic := fs.isPublicPath(name)
	if isPublic {
		rootName, relPath := fs.recordPath(name)
		if !fs.canManagePublic(rootName, relPath) {
			fs.recordSecurity(models.EventAccessForbidden, models.SeverityMedium, "file.remove", name, "匿名或非归属用户尝试管理公开文件")
			return os.ErrPermission
		}
		if st, remote := fs.storageFor(rootName); remote {
			// 远端后端：经 Storage 接口删除对象；先 Stat 确认存在（删除不存在的路径静默转移错误）。
			_, err := st.Stat(fs.ctx, relPath)
			if err != nil {
				if storage.IsNotExist(err) {
					return os.ErrNotExist
				}
				return err
			}
			if err := st.Remove(fs.ctx, relPath); err != nil {
				return err
			}
			fs.recordRemove(name)
			return nil
		}
	}
	realPath, err := fs.resolvePath(name, "remove")
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
	isPublic := fs.isPublicPath(name)
	if isPublic {
		rootName, relPath := fs.recordPath(name)
		if !fs.canManagePublic(rootName, relPath) {
			fs.recordSecurity(models.EventAccessForbidden, models.SeverityMedium, "file.removeall", name, "匿名或非归属用户尝试管理公开文件")
			return os.ErrPermission
		}
		if st, remote := fs.storageFor(rootName); remote {
			if _, err := st.Stat(fs.ctx, relPath); err != nil {
				if storage.IsNotExist(err) {
					return os.ErrNotExist
				}
				return err
			}
			if err := st.RemoveAll(fs.ctx, relPath); err != nil {
				return err
			}
			fs.recordRemove(name)
			return nil
		}
	}
	realPath, err := fs.resolvePath(name, "removeall")
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
		fs.recordSecurity(models.EventAccessForbidden, models.SeverityHigh, "file.rename", oldName+" -> "+newName, "跨命名空间重命名拒绝")
		return os.ErrPermission
	}
	oldPath, err := fs.resolvePath(oldName, "rename")
	if err != nil {
		return err
	}
	newPath, err := fs.resolvePath(newName, "rename")
	if err != nil {
		return err
	}

	oldRoot, oldRel := fs.recordPath(oldName)
	newRoot, newRel := fs.recordPath(newName)

	if oldNS == "public" {
		// 目标根不允许上传时，禁止重命名/移动进入该目录
		if !fs.canCreatePublic(newRoot) {
			return os.ErrPermission
		}
		// 仅同根内重命名；匿名一律禁止；管理员/上传者本人放行
		if oldRoot != newRoot {
			return os.ErrPermission
		}
		if !fs.canManagePublic(oldRoot, oldRel) {
			fs.recordSecurity(models.EventAccessForbidden, models.SeverityMedium, "file.rename", oldName, "匿名或非归属用户尝试管理公开文件")
			return os.ErrPermission
		}
		// 远端后端：经 Storage 接口重命名（对象存储 CopyObject+RemoveObject 模拟移动）。
		if st, remote := fs.storageFor(oldRoot); remote {
			// 目标已存在（文件或目录）→ 拒绝覆盖（远端经 Storage Stat 判定，替代磁盘 existsOnDisk）
			if _, serr := st.Stat(fs.ctx, newRel); serr == nil {
				return os.ErrExist
			} else if !storage.IsNotExist(serr) {
				return serr
			}
			if err := st.Rename(fs.ctx, oldRel, newRel); err != nil {
				return err
			}
			if fs.recordFn != nil {
				fs.recordFn("rename", newRel, filepath.Base(newName), newRoot, "", fs.clientIP, fs.userUUID, 0)
			}
			if fs.backend == nil {
				return nil
			}
			fs.backend.MovePublicFile(oldRoot, oldRel, newRel)
			return nil
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
			rootName := subParts[0]
			rootPath, ok := fs.rootNames[rootName]
			if !ok {
				return nil, os.ErrNotExist
			}
			// 远端后端：根目录恒视为存在（对象存储无真实物理目录，磁盘 Stat 恒失败）。
			if _, remote := fs.storageFor(rootName); remote {
				return &virtualDirInfo{name: rootName}, nil
			}
			fi, err := afero.NewOsFs().Stat(rootPath)
			if err != nil || !fi.IsDir() {
				// 根目录在磁盘上不存在/不可访问时按不存在处理（避免幽灵目录）
				return nil, os.ErrNotExist
			}
			return fi, nil
		}
	}

	realPath, err := fs.resolvePath(name, "stat")
	if err != nil {
		return nil, err
	}
	return afero.NewOsFs().Stat(realPath)
}

func (fs *MultiRootFs) Name() string {
	return "MultiRootFs"
}

func (fs *MultiRootFs) Chmod(name string, mode os.FileMode) error {
	realPath, err := fs.resolvePath(name, "chmod")
	if err != nil {
		return err
	}
	return afero.NewOsFs().Chmod(realPath, mode)
}

func (fs *MultiRootFs) Chtimes(name string, atime, mtime time.Time) error {
	realPath, err := fs.resolvePath(name, "chtimes")
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
		// 远端后端（对象存储/远端 WebDAV）恒列出（无真实物理目录）；本地只列磁盘真实存在的根
		entries := make([]os.FileInfo, 0, len(fs.rootNames))
		for name, rootPath := range fs.rootNames {
			if _, remote := fs.storageFor(name); remote {
				entries = append(entries, &virtualDirInfo{name: name})
				continue
			}
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
	realPath, err := fs.resolvePath(name, "list")
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
			// （远端后端恒视为存在；本地要求磁盘真实存在的根，避免幽灵目录）
			rootName := subParts[0]
			rootPath, ok := a.fs.rootNames[rootName]
			if !ok {
				return nil, os.ErrNotExist
			}
			if _, remote := a.fs.storageFor(rootName); remote {
				return &virtualDir{fs: a.fs, name: name}, nil
			}
			if fi, err := afero.NewOsFs().Stat(rootPath); err == nil && fi.IsDir() {
				return &virtualDir{fs: a.fs, name: name}, nil
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

// remoteRecordingFile 远端写入的本地缓存文件包装：FTP 传输全部落到本地临时文件
// （支持完整 Seek/WriteAt/追加/续传语义），Close 时经 remoteUpload 一次性写入远端
// 后端，再清理本地缓存。返回值错误（如配额超限/远端写入失败）回传 FTP 客户端。
type remoteRecordingFile struct {
	afero.File          // 缓存文件（*os.File 链 sizeLimitedFile），继承全部读写方法
	cached     *os.File // 本地缓存真实句柄：Close 前 Seek 回开头 + 清理用
	onClose    func(size int64) error
	closed     bool
}

func (f *remoteRecordingFile) Close() error {
	if f.closed {
		return f.File.Close()
	}
	f.closed = true
	// 回到文件开头，使 remoteUpload 的 io.Copy(wc, rec.cached) 从偏移 0 读出全部内容。
	if _, err := f.cached.Seek(0, 0); err != nil {
		return err
	}
	size := int64(0)
	if fi, serr := f.cached.Stat(); serr == nil {
		size = fi.Size()
	}
	// 远端写入期间缓存文件必须保持打开（io.Copy 依赖 rec.cached 句柄）。
	var upErr error
	if f.onClose != nil {
		upErr = f.onClose(size)
	}
	// 远端写入完成后关闭并清理本地临时缓存（无论成败都清理）。
	closeErr := f.File.Close()
	os.Remove(f.cached.Name())
	if upErr != nil {
		return upErr
	}
	return closeErr
}

func (f *remoteRecordingFile) Sync() error {
	return f.File.Sync()
}

func (f *remoteRecordingFile) Name() string {
	return f.File.Name()
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
