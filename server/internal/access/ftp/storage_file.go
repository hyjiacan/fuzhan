package ftp

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"time"

	"github.com/spf13/afero"

	"fuzhan/internal/storage"
)

// 以下类型把 storage.Storage 后端适配为 afero.File，使公开共享目录在
// 远端后端（S3/MinIO/OSS/WebDAV）下也能通过 FTP 协议浏览、下载、上传。
// 本地后端不进入这些适配路径（仍走原始 os 操作），因此 Seek/WriteAt 等能力
// 在本地与 s3（minio.Object 天然 io.ReadSeeker/io.ReaderAt）下完整保留，
// 仅 oss/webdav 等流式后端按能力降级（上传由本地缓存 + Close 一次性写入兜底）。

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
// 实现 afero.File 全接口（FTP 的 LIST/NLST/MLSD 依赖 Readdir/Read 语义）。
type storageDirFile struct {
	st     storage.Storage
	ctx    context.Context
	name   string // 逻辑相对路径（subPath，带前导 /，与 storage 后端约定一致）
	pos    int
	closed bool
}

func (f *storageDirFile) Close() error {
	f.closed = true
	return nil
}

func (f *storageDirFile) Sync() error { return nil }

func (f *storageDirFile) Name() string { return path.Base(f.name) }

func (f *storageDirFile) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (f *storageDirFile) ReadAt(_ []byte, _ int64) (int, error) {
	return 0, io.EOF
}

func (f *storageDirFile) Seek(offset int64, whence int) (int64, error) {
	// 目录只用于列表，Seek 无实际语义；仅支持重置到开头。
	if offset == 0 && whence == 0 {
		f.pos = 0
		return 0, nil
	}
	return 0, os.ErrInvalid
}

func (f *storageDirFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageDirFile) WriteAt(_ []byte, _ int64) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageDirFile) WriteString(_ string) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageDirFile) Truncate(_ int64) error {
	return os.ErrPermission
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

func (f *storageDirFile) Readdirnames(n int) ([]string, error) {
	entries, err := f.Readdir(n)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

var _ afero.File = (*storageDirFile)(nil)

// storageReadFile 适配远端存储的只读文件打开（RETR）。
// Read 直通底层流；Seek/ReadAt 按后端能力降级：底层流实现 io.ReadSeeker /
// io.ReaderAt（s3 的 minio.Object）则完整支持（RETR+REST 断点续传可用），
// 否则（oss/webdav）拒绝随机定位降级为顺序读。
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

func (f *storageReadFile) Sync() error { return nil }

func (f *storageReadFile) Name() string { return "" }

func (f *storageReadFile) Read(p []byte) (int, error) {
	return f.rc.Read(p)
}

func (f *storageReadFile) ReadAt(p []byte, off int64) (int, error) {
	if ra, ok := f.rc.(io.ReaderAt); ok {
		return ra.ReadAt(p, off)
	}
	return 0, os.ErrInvalid
}

func (f *storageReadFile) Seek(offset int64, whence int) (int64, error) {
	if f.seeker == nil {
		// 后端流不支持随机定位（oss/webdav）：降级拒绝，REST 由上层降级处理。
		return 0, os.ErrInvalid
	}
	return f.seeker.Seek(offset, whence)
}

func (f *storageReadFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageReadFile) WriteAt(_ []byte, _ int64) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageReadFile) WriteString(_ string) (int, error) {
	return 0, os.ErrPermission
}

func (f *storageReadFile) Truncate(_ int64) error {
	return os.ErrPermission
}

func (f *storageReadFile) Stat() (os.FileInfo, error) {
	return f.info, nil
}

func (f *storageReadFile) Readdir(_ int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

func (f *storageReadFile) Readdirnames(_ int) ([]string, error) {
	return nil, os.ErrInvalid
}

var _ afero.File = (*storageReadFile)(nil)
