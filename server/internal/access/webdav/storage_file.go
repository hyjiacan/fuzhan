package webdav

import (
	"context"
	"io"
	"io/fs"
	"os"
	"time"

	"golang.org/x/net/webdav"

	"fuzhan/internal/storage"
)

// 以下类型把 storage.Storage 后端适配为 webdav.File，使公开共享目录在
// 远端后端（S3/MinIO/OSS/WebDAV）下也能通过 WebDAV 协议浏览、下载、上传。
// 本地后端不进入这些适配路径（仍走原始 os 操作），因此 Seek/Range 等能力
// 在本地与 s3（minio.Object 天然 io.ReadSeeker）下完整保留，仅 oss/webdav
// 等流式后端按能力降级。

// storageFileInfo 将 storage.StatInfo（字段）适配为 os.FileInfo。
type storageFileInfo struct {
	info storage.StatInfo
}

func (f *storageFileInfo) Name() string { return f.info.Name }

func (f *storageFileInfo) Size() int64 { return f.info.Size }

func (f *storageFileInfo) Mode() os.FileMode {
	if f.info.Mode != 0 {
		return f.info.Mode
	}
	if f.info.IsDir {
		return os.ModeDir | 0755
	}
	return 0644
}

func (f *storageFileInfo) ModTime() time.Time { return f.info.ModTime }

func (f *storageFileInfo) IsDir() bool { return f.info.IsDir }

func (f *storageFileInfo) Sys() interface{} { return nil }

var _ fs.FileInfo = (*storageFileInfo)(nil)

// storageListInfo 将 storage.ObjectInfo 条目适配为 os.FileInfo（目录列表用）。
type storageListInfo struct {
	info storage.ObjectInfo
}

func (f *storageListInfo) Name() string { return f.info.Name }

func (f *storageListInfo) Size() int64 { return f.info.Size }

func (f *storageListInfo) Mode() os.FileMode {
	if f.info.IsDir {
		return os.ModeDir | 0755
	}
	return 0644
}

func (f *storageListInfo) ModTime() time.Time { return f.info.ModTime }

func (f *storageListInfo) IsDir() bool { return f.info.IsDir }

func (f *storageListInfo) Sys() interface{} { return nil }

var _ fs.FileInfo = (*storageListInfo)(nil)

// storageDirFile 适配远端存储的目录打开：Stat 走 st.Stat，Readdir 走 st.List。
// WebDAV handler 对目录 PROPFIND/GET 会调用 OpenFile 返回文件的 Readdir 与 Stat。
type storageDirFile struct {
	st     storage.Storage
	ctx    context.Context
	name   string // 逻辑相对路径（无前导斜杠）
	pos    int
	closed bool
}

func (f *storageDirFile) Close() error {
	f.closed = true
	return nil
}

func (f *storageDirFile) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (f *storageDirFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageDirFile) Seek(offset int64, whence int) (int64, error) {
	// 目录只用于列表，Seek 无实际语义；仅支持重置到开头。
	if offset == 0 && whence == 0 {
		f.pos = 0
		return 0, nil
	}
	return 0, os.ErrInvalid
}

func (f *storageDirFile) Stat() (os.FileInfo, error) {
	info, err := f.st.Stat(f.ctx, f.name)
	if err != nil {
		return nil, err
	}
	return &storageFileInfo{info: info}, nil
}

func (f *storageDirFile) Readdir(count int) ([]os.FileInfo, error) {
	list, err := f.st.List(f.ctx, f.name)
	if err != nil {
		return nil, err
	}
	entries := make([]os.FileInfo, 0, len(list.Objects))
	for _, o := range list.Objects {
		entries = append(entries, &storageListInfo{info: o})
	}
	if f.pos >= len(entries) {
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	entries = entries[f.pos:]
	if count > 0 && count < len(entries) {
		entries = entries[:count]
	}
	f.pos += len(entries)
	return entries, nil
}

var _ webdav.File = (*storageDirFile)(nil)

// storageReadFile 适配远端存储的只读文件打开。
// Read 直通底层流；Seek 按后端能力降级：底层流实现 io.ReadSeeker（s3 的
// minio.Object）则完整支持（Range/206 可用），否则返回 os.ErrInvalid。
type storageReadFile struct {
	rc     io.ReadCloser
	seeker io.Seeker // 非 nil 表示底层流支持 Seek（如 s3）
	info   os.FileInfo
}

func (f *storageReadFile) Close() error {
	if f.rc == nil {
		return nil
	}
	return f.rc.Close()
}

func (f *storageReadFile) Read(p []byte) (int, error) {
	return f.rc.Read(p)
}

func (f *storageReadFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageReadFile) Seek(offset int64, whence int) (int64, error) {
	if f.seeker == nil {
		// 后端流不支持随机定位（oss/webdav）：降级拒绝，Range 由上层降级处理。
		return 0, os.ErrInvalid
	}
	return f.seeker.Seek(offset, whence)
}

func (f *storageReadFile) Stat() (os.FileInfo, error) {
	return f.info, nil
}

func (f *storageReadFile) Readdir(_ int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

var _ webdav.File = (*storageReadFile)(nil)

// storageSyncedFile 适配远端存储的新建公开文件：包装 st.OpenWriter，
// 关闭后（Close）判定目标确实已创建再同步索引/哈希/上传者 IP（H2 链路）。
// s3/oss/webdav 的 OpenWriter 均在 Close 时提交，因此 Close 后 Stat 可见。
type storageSyncedFile struct {
	wc       io.WriteCloser
	st       storage.Storage
	ctx      context.Context
	backend  Backend
	rootName string
	relPath  string
	clientIP string
	closed   bool
}

func (f *storageSyncedFile) Close() error {
	if f.closed {
		if f.wc != nil {
			return f.wc.Close()
		}
		return nil
	}
	f.closed = true
	if f.wc == nil {
		return nil
	}
	err := f.wc.Close()
	if err != nil {
		return err
	}
	if f.backend != nil {
		// 文件确实已创建（远端 Stat 成功且为文件）才同步。
		if info, serr := f.st.Stat(f.ctx, f.relPath); serr == nil && !info.IsDir {
			f.backend.SyncPublicFile(f.rootName, f.relPath, f.clientIP)
		}
	}
	return nil
}

func (f *storageSyncedFile) Read(_ []byte) (int, error) {
	return 0, os.ErrInvalid
}

func (f *storageSyncedFile) Seek(offset int64, whence int) (int64, error) {
	// 写文件（上传）不参与 Seek 语义：远端后端无原地随机写。
	return 0, os.ErrInvalid
}

func (f *storageSyncedFile) Write(p []byte) (int, error) {
	if f.wc == nil {
		return 0, os.ErrInvalid
	}
	return f.wc.Write(p)
}

func (f *storageSyncedFile) Stat() (os.FileInfo, error) {
	if f.wc == nil {
		return nil, os.ErrInvalid
	}
	info, err := f.st.Stat(f.ctx, f.relPath)
	if err != nil {
		return nil, err
	}
	return &storageFileInfo{info: info}, nil
}

func (f *storageSyncedFile) Readdir(_ int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

var _ webdav.File = (*storageSyncedFile)(nil)
