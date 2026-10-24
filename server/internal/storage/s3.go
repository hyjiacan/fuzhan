package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
)

// S3Storage 基于 S3 协议（minio-go）的对象存储实现，兼容 S3 / MinIO。
//
// 对象存储没有真正的目录层级，本实现以"目录前缀 + '/' 分隔"模拟目录结构：
//   - 每个对象位于逻辑路径 key = prefix + "/" + path；
//   - 目录本身可为空（无占位对象），List 依据 key 前缀去重推导间接目录；
//   - Rename 通过 CopyObject + RemoveObject 降级实现（非原子）；
//   - MkdirAll 视为无操作；分片上传的随机写/覆盖语义由上层以"上传临时对象 + 最终落地"降级。
type S3Storage struct {
	client *minio.Client
	bucket string
	// prefix 所有对象的前缀（逻辑根目录，如公开根目录名/私有/临时）。
	prefix string
}

// NewS3Storage 创建 S3 存储实例。bucket 为桶名，prefix 为该存储实例的逻辑根前缀（可空）。
func NewS3Storage(client *minio.Client, bucket, prefix string) *S3Storage {
	return &S3Storage{
		client: client,
		bucket: bucket,
		prefix: strings.Trim(prefix, "/"),
	}
}

// Kind 返回存储类型标识。
func (s *S3Storage) Kind() string { return "s3" }

// resolve 将逻辑路径转换为对象 key。
// path 为空或 "/" 时返回前缀本身（根目录）。
func (s *S3Storage) resolve(path string) string {
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

// notExistError 将 S3 NoSuchKey / NoSuchBucket 错误归一化为 os.ErrNotExist 语义，
// 保证上层 IsNotExist 判定一致。
func (s *S3Storage) notExistError(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" || resp.StatusCode == http.StatusNotFound {
		return &os.PathError{Op: "stat", Path: "", Err: os.ErrNotExist}
	}
	return err
}

// Stat 返回对象元信息。
// 目录（其下存在对象的前缀）返回 IsDir=true；否则对具体对象做 Head。
func (s *S3Storage) Stat(ctx context.Context, path string) (StatInfo, error) {
	p := s.resolve(path)
	if p == "" {
		// 根目录视为恒存在。
		return StatInfo{Name: "/", IsDir: true, Mode: os.ModeDir | 0755}, nil
	}
	// 目录检测：若存在以 p+"/" 开头的对象，则视为目录。
	if strings.HasSuffix(p, "/") {
		for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix:    p,
			Recursive: false,
			MaxKeys:   1,
		}) {
			if o.Err != nil {
				return StatInfo{}, o.Err
			}
			return StatInfo{Name: pathpkg.Base(p), IsDir: true, Mode: os.ModeDir | 0755, ModTime: o.LastModified}, nil
		}
		return StatInfo{}, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
	}
	info, err := s.client.StatObject(ctx, s.bucket, p, minio.StatObjectOptions{})
	if err != nil {
		return StatInfo{}, s.notExistError(err)
	}
	return StatInfo{
		Name:    pathpkg.Base(info.Key),
		Size:    info.Size,
		IsDir:   false,
		Mode:    0644,
		ModTime: info.LastModified,
	}, nil
}

// Open 打开对象用于流式读取。
// minio.Object 实现了 Read/Seek/Close，天然满足 io.ReadSeeker 与 io.ReadAt。
func (s *S3Storage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, s.resolve(path), minio.GetObjectOptions{})
	if err != nil {
		return nil, s.notExistError(err)
	}
	return obj, nil
}

// OpenWriter 以整对象写入方式打开（对象存储不支持原地随机写）。
// 本实现先获取既有对象大小用于写入（实际由上层仅在顺序写场景使用），
// 依次顺序追加。由于对象存储无增量写，这里采用"现有内容 + 新内容"合并上传。
//
// 说明：分片上传并非通过本方法实现（对象存储接入后由上层以"临时对象 + 合并"完成），
// 本方法仅服务临时文件等顺序写场景。实现为缓冲到 Close 时 Put 一次。
func (s *S3Storage) OpenWriter(ctx context.Context, path string) (io.WriteCloser, error) {
	return &s3Writer{
		ctx:   ctx,
		s:     s,
		key:   s.resolve(path),
		body:  &strings.Builder{},
	}, nil
}

// s3Writer 缓冲写入对象数据，Close 时一次性 Put 提交。
type s3Writer struct {
	ctx  context.Context
	s    *S3Storage
	key  string
	body *strings.Builder
	// pending 记录写入是否发生，避免空文件被省略。
	written bool
}

func (w *s3Writer) Write(p []byte) (int, error) {
	n, err := w.body.Write(p)
	if n > 0 {
		w.written = true
	}
	return n, err
}

func (w *s3Writer) Close() error {
	body := w.body.String()
	_, err := w.s.client.PutObject(w.ctx, w.s.bucket, w.key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{})
	return err
}

// ReadFile 读取整个对象内容。
func (s *S3Storage) ReadFile(ctx context.Context, path string) ([]byte, error) {
	obj, err := s.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// MkdirAll 对象存储无真实目录，视为无操作成功返回。
func (s *S3Storage) MkdirAll(ctx context.Context, dir string) error {
	return nil
}

// Rename 通过 CopyObject + RemoveObject 降级实现（非原子，复制后删除源）。
func (s *S3Storage) Rename(ctx context.Context, oldPath, newPath string) error {
	srcKey := s.resolve(oldPath)
	dstKey := s.resolve(newPath)
	// 目标已存在时先删除，简化覆盖语义。
	if err := s.client.RemoveObject(ctx, s.bucket, dstKey, minio.RemoveObjectOptions{}); err != nil {
		if resp := minio.ToErrorResponse(err); resp.StatusCode != http.StatusNotFound {
			return err
		}
	}
	if _, err := s.client.CopyObject(ctx, minio.CopyDestOptions{Bucket: s.bucket, Object: dstKey},
		minio.CopySrcOptions{Bucket: s.bucket, Object: srcKey}); err != nil {
		return err
	}
	return s.client.RemoveObject(ctx, s.bucket, srcKey, minio.RemoveObjectOptions{})
}

// Remove 删除单个对象；不存在时返回 os.ErrNotExist 语义。
func (s *S3Storage) Remove(ctx context.Context, path string) error {
	key := s.resolve(path)
	if key == "" {
		return errors.New("无法删除根目录")
	}
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return s.notExistError(err)
	}
	return nil
}

// RemoveAll 递归删除前缀下的所有对象（模拟目录递归删除）。
func (s *S3Storage) RemoveAll(ctx context.Context, path string) error {
	prefix := s.resolve(path)
	if prefix == "" {
		return errors.New("无法删除根目录")
	}
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	objs := make([]string, 0)
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if o.Err != nil {
			return o.Err
		}
		objs = append(objs, o.Key)
	}
	for _, k := range objs {
		if err := s.client.RemoveObject(ctx, s.bucket, k, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// List 列举目录下的直接子项（不含递归）。
// 对象存储无目录实体，依据 key 前缀与 '/' 分隔符去重推导：
//   - 直接对象（子文件）；
//   - 子目录（子对象 key 中 '/' 之下的首段）。
func (s *S3Storage) List(ctx context.Context, dir string) (ListResult, error) {
	dirPrefix := s.resolve(dir)
	prefix := dirPrefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	seen := make(map[string]ObjectInfo)
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}) {
		if o.Err != nil {
			return ListResult{}, o.Err
		}
		rest := strings.TrimPrefix(o.Key, prefix)
		// 跳过目录占位对象自身。
		if rest == "" {
			continue
		}
		if idx := strings.Index(rest, "/"); idx >= 0 {
			// 子目录
			dirName := rest[:idx]
			if _, ok := seen["__dir__/"+dirName]; !ok {
				seen["__dir__/"+dirName] = ObjectInfo{
					Name:    dirName,
					Size:    0,
					IsDir:   true,
					ModTime: o.LastModified,
				}
			}
		} else {
			// 直接对象
			seen["__file__/"+rest] = ObjectInfo{
				Name:    rest,
				Size:    o.Size,
				IsDir:   false,
				ModTime: o.LastModified,
			}
		}
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

// ServeContent 流式输出对象内容，支持 Range/206 断点续传。
// minio.Object 实现 io.ReadSeeker（Seek 会按 offset 发起新的 Range 请求），
// 因此可直接交由 http.ServeContent 处理多段/单段 Range。
func (s *S3Storage) ServeContent(ctx context.Context, w http.ResponseWriter, r *http.Request, name, path string) error {
	info, err := s.Stat(ctx, path)
	if err != nil {
		return err
	}
	if info.IsDir {
		return errors.New("目标为目录")
	}
	obj, err := s.Open(ctx, path)
	if err != nil {
		return err
	}
	defer obj.Close()
	seeker, ok := obj.(io.ReadSeeker)
	if !ok {
		return errors.New("存储实现不支持随机读取")
	}
	http.ServeContent(w, r, name, info.ModTime, seeker)
	return nil
}

// ensureInterfaceCompile 编译期断言：S3Storage 实现 Storage。
var _ Storage = (*S3Storage)(nil)
