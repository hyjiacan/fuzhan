package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// OSSStorage 阿里云 OSS 对象存储实现。
//
// 与 S3Storage 一致：以"目录前缀 + '/' 分隔"模拟目录结构；
// Rename 通过 CopyObject + DeleteObject 降级实现；MkdirAll 无操作。
// OSS 的 GetObject 返回的响应体不具备 Seek 能力，因此 ServeContent 采用降级策略：
//   - 小对象（<= maxSeekMemory）整体读入内存后基于 bytes.Reader 提供 Seek；
//   - 大对象降级为逐段读取（部分 Range 支持）。
type OSSStorage struct {
	client *oss.Client
	bucket *oss.Bucket
	// prefix 所有对象的前缀（逻辑根目录）。
	prefix string
}

// maxSeekMemory 内存缓存上限：对象大小 <= 该值时才缓存为 Seekable 流。
const maxSeekMemory = 8 << 20 // 8 MiB

// NewOSSStorage 创建 OSS 存储实例。需要已通过 client.Bucket(bucketName) 取得 bucket。
func NewOSSStorage(client *oss.Client, bucketName, prefix string) (*OSSStorage, error) {
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, err
	}
	return &OSSStorage{
		client: client,
		bucket: bucket,
		prefix: strings.Trim(prefix, "/"),
	}, nil
}

// Kind 返回存储类型标识。
func (s *OSSStorage) Kind() string { return "oss" }

// resolve 将逻辑路径转换为对象 key。
func (s *OSSStorage) resolve(path string) string {
	p := strings.Trim(path, "/")
	if p == "" && s.prefix == "" {
		return ""
	}
	if p == "" {
		return s.prefix
	}
	if s.prefix == "" {
		return p
	}
	return s.prefix + "/" + p
}

// notExistError 将 OSS NotFound 错误归一化为 os.ErrNotExist 语义。
func (s *OSSStorage) notExistError(err error) error {
	if err == nil {
		return nil
	}
	var se oss.ServiceError
	if errors.As(err, &se) && se.StatusCode == http.StatusNotFound {
		return &os.PathError{Op: "stat", Path: "", Err: os.ErrNotExist}
	}
	return err
}

// Stat 返回对象元信息。
func (s *OSSStorage) Stat(ctx context.Context, path string) (StatInfo, error) {
	p := s.resolve(path)
	if strings.Trim(path, "/") == "" {
		// 根目录视为恒存在（即使配置了对象前缀且前缀下暂无对象）。
		return StatInfo{Name: "/", IsDir: true, Mode: os.ModeDir | 0755}, nil
	}
	headers, err := s.bucket.GetObjectMeta(p)
	if err == nil {
		size := parseContentLength(headers.Get("Content-Length"))
		modTime, _ := http.ParseTime(headers.Get("Last-Modified"))
		return StatInfo{
			Name:    pathpkg.Base(p),
			Size:    size,
			IsDir:   false,
			Mode:    0644,
			ModTime: modTime,
		}, nil
	}
	// 仅当确认对象不存在时才探测目录，鉴权/网络等其它错误直接返回。
	var se oss.ServiceError
	if !errors.As(err, &se) || se.StatusCode != http.StatusNotFound {
		return StatInfo{}, err
	}
	// 目录检测：存在以 p+"/" 开头的对象则视为目录。
	base := strings.TrimSuffix(p, "/")
	res, err := s.bucket.ListObjects(oss.Prefix(base+"/"), oss.MaxKeys(1), oss.Delimiter("/"))
	if err != nil {
		return StatInfo{}, err
	}
	if len(res.Objects) > 0 || len(res.CommonPrefixes) > 0 {
		return StatInfo{Name: pathpkg.Base(base), IsDir: true, Mode: os.ModeDir | 0755}, nil
	}
	return StatInfo{}, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
}

// Open 打开对象用于流式读取（OSS 返回的响应体不支持 Seek）。
func (s *OSSStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	reader, err := s.bucket.GetObject(s.resolve(path))
	if err != nil {
		return nil, s.notExistError(err)
	}
	return reader, nil
}

// OpenWriter 以整对象写入方式打开（对象存储不支持原地随机写）。
func (s *OSSStorage) OpenWriter(ctx context.Context, path string) (io.WriteCloser, error) {
	return &ossWriter{
		ctx: ctx,
		s:   s,
		key: s.resolve(path),
		buf: &bytes.Buffer{},
	}, nil
}

// ossWriter 缓冲写入对象数据，Close 时一次性 Put 提交。
type ossWriter struct {
	ctx context.Context
	s   *OSSStorage
	key string
	buf *bytes.Buffer
}

func (w *ossWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *ossWriter) Close() error {
	return w.s.bucket.PutObject(w.key, w.buf)
}

// ReadFile 读取整个对象内容。
func (s *OSSStorage) ReadFile(ctx context.Context, path string) ([]byte, error) {
	reader, err := s.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// MkdirAll 对象存储无真实目录，视为无操作成功返回。
func (s *OSSStorage) MkdirAll(ctx context.Context, dir string) error {
	return nil
}

// Rename 通过 CopyObject + DeleteObject 降级实现（非原子）。
func (s *OSSStorage) Rename(ctx context.Context, oldPath, newPath string) error {
	srcKey := s.resolve(oldPath)
	dstKey := s.resolve(newPath)
	// OSS CopyObject 目标已存在时自动覆盖，无需先删除。
	if _, err := s.bucket.CopyObject(srcKey, dstKey); err != nil {
		return err
	}
	return s.bucket.DeleteObject(srcKey)
}

// Remove 删除单个对象；不存在时返回 os.ErrNotExist 语义。
func (s *OSSStorage) Remove(ctx context.Context, path string) error {
	key := s.resolve(path)
	if key == "" {
		return errors.New("无法删除根目录")
	}
	err := s.bucket.DeleteObject(key)
	if err != nil {
		return s.notExistError(err)
	}
	return nil
}

// RemoveAll 递归删除前缀下的所有对象（模拟目录递归删除）。
func (s *OSSStorage) RemoveAll(ctx context.Context, path string) error {
	prefix := s.resolve(path)
	if prefix == "" {
		return errors.New("无法删除根目录")
	}
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	marker := ""
	for {
		res, err := s.bucket.ListObjects(oss.Prefix(prefix), oss.Marker(marker))
		if err != nil {
			return err
		}
		for _, o := range res.Objects {
			if err := s.bucket.DeleteObject(o.Key); err != nil {
				return err
			}
		}
		if !res.IsTruncated {
			break
		}
		marker = res.NextMarker
	}
	return nil
}

// List 列举目录下的直接子项（不含递归）。
// OSS 的 ListObjects 配合 Delimiter("/") 可直接返回直接子对象与 CommonPrefixes（子目录）。
func (s *OSSStorage) List(ctx context.Context, dir string) (ListResult, error) {
	dirPrefix := s.resolve(dir)
	prefix := dirPrefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	seen := make(map[string]ObjectInfo)
	marker := ""
	for {
		res, err := s.bucket.ListObjects(oss.Prefix(prefix), oss.Delimiter("/"), oss.Marker(marker))
		if err != nil {
			return ListResult{}, err
		}
		// 子目录
		for _, cp := range res.CommonPrefixes {
			cp = strings.TrimPrefix(cp, prefix)
			cp = strings.TrimSuffix(cp, "/")
			if cp == "" {
				continue
			}
			if idx := strings.Index(cp, "/"); idx >= 0 {
				cp = cp[:idx]
			}
			if _, ok := seen["__dir__/"+cp]; !ok {
				seen["__dir__/"+cp] = ObjectInfo{Name: cp, IsDir: true}
			}
		}
		// 直接对象
		for _, o := range res.Objects {
			rest := strings.TrimPrefix(o.Key, prefix)
			if rest == "" {
				continue
			}
			if _, ok := seen["__file__/"+rest]; !ok {
				seen["__file__/"+rest] = ObjectInfo{
					Name:    rest,
					Size:    o.Size,
					IsDir:   false,
					ModTime: o.LastModified,
				}
			}
		}
		if !res.IsTruncated {
			break
		}
		marker = res.NextMarker
	}
	result := ListResult{Objects: make([]ObjectInfo, 0, len(seen))}
	for _, info := range seen {
		result.Objects = append(result.Objects, info)
	}
	sort.Slice(result.Objects, func(i, j int) bool {
		if result.Objects[i].IsDir != result.Objects[j].IsDir {
			return result.Objects[i].IsDir // 目录在前
		}
		return result.Objects[i].Name < result.Objects[j].Name
	})
	return result, nil
}

// ServeContent 流式输出对象内容，支持 Range/206 断点续传（降级实现）。
// OSS 响应体不支持 Seek，因此：
//   - 小对象整体读入内存转 bytes.Reader（完整支持 Range/多段）；
//   - 大对象暂不支持 Seek，退化为整对象下发（由上层保证仅预览/下载场景调用）。
func (s *OSSStorage) ServeContent(ctx context.Context, w http.ResponseWriter, r *http.Request, name, path string) error {
	info, err := s.Stat(ctx, path)
	if err != nil {
		return err
	}
	if info.IsDir {
		return errors.New("目标为目录")
	}
	reader, err := s.Open(ctx, path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if info.Size > maxSeekMemory {
		// 大对象：不能 Seek，回退到分段手动实现（先尝试 Range 支持）。
		return s.serveLarge(ctx, w, r, name, path, info)
	}
	// 小对象：全量读入内存后支持 Seek。
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	seeker := bytes.NewReader(data)
	http.ServeContent(w, r, name, info.ModTime, seeker)
	return nil
}

// serveLarge 大对象降级：能力受限，仅做整对象下发。
// 对象存储下大文件预览/下载通常由前端直传签名 URL 完成，此处保证功能可用即可。
func (s *OSSStorage) serveLarge(ctx context.Context, w http.ResponseWriter, r *http.Request, name, path string, info StatInfo) error {
	reader, err := s.Open(ctx, path)
	if err != nil {
		return err
	}
	defer reader.Close()
	ct, body := sniffContentType(name, reader)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Length", itoa(info.Size))
	if name != "" {
		w.Header().Set("Content-Disposition", contentDispositionAttachment(name))
	}
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, body)
	return err
}

// parseContentLength 解析 Content-Length 头，失败返回 0。
func parseContentLength(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n
}

// contentDispositionAttachment 构造下载响应头 Content-Disposition。
func contentDispositionAttachment(name string) string {
	return `attachment; filename="` + strings.ReplaceAll(name, `"`, `_`) + `"`
}

// contentTypeFor 按文件名扩展名推断 Content-Type；无法识别时按二进制流处理。
func contentTypeFor(name string) string {
	if name != "" {
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			if ct := mime.TypeByExtension(ext); ct != "" {
				return ct
			}
		}
	}
	return "application/octet-stream"
}

// sniffContentType 读取前 512 字节嗅探真实内容类型，并返回可继续完整读取的 Reader。
func sniffContentType(name string, r io.Reader) (string, io.Reader) {
	head := make([]byte, 512)
	n, _ := io.ReadFull(r, head)
	if n == 0 {
		return contentTypeFor(name), r
	}
	return http.DetectContentType(head[:n]), io.MultiReader(bytes.NewReader(head[:n]), r)
}

// itoa 将整数转为十进制字符串。
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// ensureOSSInterfaceCompile 编译期断言：OSSStorage 实现 Storage。
var _ Storage = (*OSSStorage)(nil)
