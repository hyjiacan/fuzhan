package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/studio-b12/gowebdav"
)

// WebDAVStorage 基于 WebDAV 服务（gowebdav 客户端）的存储实现。
//
// WebDAV 本身具有真实的目录层级（collection），与本地文件系统语义接近：
//   - Stat/Open/MkdirAll/Rename/Remove/RemoveAll/List 直接映射 WebDAV 原语；
//   - ServeContent 支持 Range：优先 ReadStreamRange（如果服务端支持 206），
//     否则降级为整段读取；
//   - OpenWriter 缓存到缓冲后调用 WriteStream 一次性提交（WebDAV 无随机写）。
type WebDAVStorage struct {
	client *gowebdav.Client
	// prefix 逻辑根目录前缀（相对 WebDAV 根），可空代表服务根目录本身。
	prefix string
}

// NewWebDAVStorage 创建 WebDAV 存储实例。
func NewWebDAVStorage(client *gowebdav.Client, prefix string) *WebDAVStorage {
	return &WebDAVStorage{
		client: client,
		prefix: strings.Trim(prefix, "/"),
	}
}

// Kind 返回存储类型标识。
func (s *WebDAVStorage) Kind() string { return "webdav" }

// resolve 将逻辑路径拼接为 WebDAV 完整路径。
func (s *WebDAVStorage) resolve(path string) string {
	p := strings.Trim(path, "/")
	if p == "" && s.prefix == "" {
		return "/"
	}
	if p == "" {
		return "/" + s.prefix
	}
	if s.prefix == "" {
		return "/" + p
	}
	return "/" + s.prefix + "/" + p
}

// notExistError 将 WebDAV 404 错误归一化为 os.ErrNotExist 语义。
func (s *WebDAVStorage) notExistError(err error) error {
	if err == nil {
		return nil
	}
	if gowebdav.IsErrNotFound(err) {
		return &os.PathError{Op: "stat", Path: "", Err: os.ErrNotExist}
	}
	return err
}

// Stat 返回文件/目录元信息。
func (s *WebDAVStorage) Stat(ctx context.Context, path string) (StatInfo, error) {
	fi, err := s.client.Stat(s.resolve(path))
	if err != nil {
		return StatInfo{}, s.notExistError(err)
	}
	return StatInfo{
		Name:    fi.Name(),
		Size:    fi.Size(),
		IsDir:   fi.IsDir(),
		Mode:    fi.Mode(),
		ModTime: fi.ModTime(),
	}, nil
}

// Open 打开文件用于流式读取（WebDAV GET）。
func (s *WebDAVStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	rc, err := s.client.ReadStream(s.resolve(path))
	if err != nil {
		return nil, s.notExistError(err)
	}
	return rc, nil
}

// OpenWriter 以整文件写入方式打开（WebDAV 不支持原地随机写）。
// 本实现缓冲到 Close 时通过 WriteStream 提交。
func (s *WebDAVStorage) OpenWriter(ctx context.Context, path string) (io.WriteCloser, error) {
	return &webdavWriter{
		ctx: ctx,
		s:   s,
		key: s.resolve(path),
		buf: &bytes.Buffer{},
	}, nil
}

// webdavWriter 缓冲写入数据，Close 时一次性提交。
type webdavWriter struct {
	ctx context.Context
	s   *WebDAVStorage
	key string
	buf *bytes.Buffer
}

func (w *webdavWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *webdavWriter) Close() error {
	return w.s.client.WriteStream(w.key, w.buf, 0664)
}

// ReadFile 读取整个文件内容。
func (s *WebDAVStorage) ReadFile(ctx context.Context, path string) ([]byte, error) {
	rc, err := s.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// MkdirAll 递归创建目录。
func (s *WebDAVStorage) MkdirAll(ctx context.Context, dir string) error {
	return s.client.MkdirAll(s.resolve(dir), 0775)
}

// Rename 重命名/移动文件（WebDAV MOVE）。
func (s *WebDAVStorage) Rename(ctx context.Context, oldPath, newPath string) error {
	return s.client.Rename(s.resolve(oldPath), s.resolve(newPath), true)
}

// Remove 删除单个文件；不存在时返回 os.ErrNotExist 语义。
func (s *WebDAVStorage) Remove(ctx context.Context, path string) error {
	err := s.client.Remove(s.resolve(path))
	if err != nil {
		return s.notExistError(err)
	}
	return nil
}

// RemoveAll 递归删除目录（WebDAV DELETE collection）。
func (s *WebDAVStorage) RemoveAll(ctx context.Context, path string) error {
	err := s.client.RemoveAll(s.resolve(path))
	if err != nil {
		return s.notExistError(err)
	}
	return nil
}

// List 列举目录下的直接子项。
func (s *WebDAVStorage) List(ctx context.Context, dir string) (ListResult, error) {
	fis, err := s.client.ReadDir(s.resolve(dir))
	if err != nil {
		return ListResult{}, s.notExistError(err)
	}
	result := ListResult{Objects: make([]ObjectInfo, 0, len(fis))}
	for _, fi := range fis {
		result.Objects = append(result.Objects, ObjectInfo{
			Name:    fi.Name(),
			Size:    fi.Size(),
			IsDir:   fi.IsDir(),
			ModTime: fi.ModTime(),
		})
	}
	sort.Slice(result.Objects, func(i, j int) bool {
		if result.Objects[i].IsDir != result.Objects[j].IsDir {
			return result.Objects[i].IsDir // 目录在前
		}
		return result.Objects[i].Name < result.Objects[j].Name
	})
	return result, nil
}

// ServeContent 流式输出文件内容，支持 Range/206 断点续传。
// 优先请求带 Range 头，若服务端支持则直接透传 206；否则整体读取下发。
func (s *WebDAVStorage) ServeContent(ctx context.Context, w http.ResponseWriter, r *http.Request, name, path string) error {
	info, err := s.Stat(ctx, path)
	if err != nil {
		return err
	}
	if info.IsDir {
		return errors.New("目标为目录")
	}

	// 显式解析 Range（兼容不支持 206 的 WebDAV 服务端）。
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		// 无 Range：整段下发。
		rc, err := s.Open(ctx, path)
		if err != nil {
			return err
		}
		defer rc.Close()
		ct, body := sniffContentType(name, rc)
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", itoa(info.Size))
		if name != "" {
			w.Header().Set("Content-Disposition", contentDispositionAttachment(name))
		}
		w.WriteHeader(http.StatusOK)
		_, err = io.Copy(w, body)
		return err
	}

	// 有 Range：尝试按范围读取。
	start, end, ok := parseByteRange(rangeHeader, info.Size)
	if !ok {
		http.Error(w, "Invalid Range", http.StatusRequestedRangeNotSatisfiable)
		return nil
	}
	length := end - start + 1
	rc, err := s.client.ReadStreamRange(s.resolve(path), start, length)
	if err != nil {
		return err
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentTypeFor(name))
	w.Header().Set("Content-Range", contentRange(start, end, info.Size))
	w.Header().Set("Content-Length", itoa(length))
	w.WriteHeader(http.StatusPartialContent)
	_, err = io.Copy(w, rc)
	return err
}

// parseByteRange 解析单个 byte range，格式 "bytes=start-end"；不存在返回 ok=false。
// end 为空表示从 start 到文件末尾；start 为空表示最后 N 字节（如 bytes=-500）。
func parseByteRange(v string, size int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(v, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(v, "bytes=")
	if idx := strings.Index(spec, ","); idx >= 0 {
		spec = spec[:idx] // 只支持单一 range
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	st := strings.TrimSpace(parts[0])
	en := strings.TrimSpace(parts[1])
	if st == "" {
		// suffix range: bytes=-N
		suffixStart, err1 := strconvParseInt(en)
		if err1 != nil || suffixStart <= 0 {
			return 0, 0, false
		}
		if suffixStart > size {
			suffixStart = size
		}
		return size - suffixStart, size - 1, true
	}
	start, err := strconvParseInt(st)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if en == "" {
		return start, size - 1, true
	}
	end, err = strconvParseInt(en)
	if err != nil {
		return 0, 0, false
	}
	if end >= size {
		end = size - 1
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

// strconvParseInt 简易解析辅助（保证 ctx 无关的纯函数）。
func strconvParseInt(s string) (int64, error) {
	var n int64
	if len(s) == 0 {
		return 0, errors.New("empty string")
	}
	neg := false
	idx := 0
	if s[0] == '-' {
		neg = true
		idx = 1
	} else if s[0] == '+' {
		idx = 1
	}
	for ; idx < len(s); idx++ {
		c := s[idx]
		if c < '0' || c > '9' {
			return 0, errors.New("invalid number")
		}
		n = n*10 + int64(c-'0')
		if n < 0 {
			return 0, errors.New("overflow")
		}
	}
	if neg {
		n = -n
	}
	return n, nil
}

// contentRange 构造 Content-Range 响应头值。
func contentRange(start, end, size int64) string {
	return "bytes " + itoa(start) + "-" + itoa(end) + "/" + itoa(size)
}

// ensureWebDAVInterfaceCompile 编译期断言：WebDAVStorage 实现 Storage。
var _ Storage = (*WebDAVStorage)(nil)
