// Package storage 定义文件存储抽象层。
//
// 本包将"文件落盘/读取"从具体介质（本地磁盘 / S3 / MinIO / OSS / WebDAV）中解耦，
// 上层业务（分片上传、下载、预览、索引扫描、哈希计算等）只依赖 Storage 接口，
// 通过工厂按配置选择具体后端，实现存储模式可插拔。
//
// 设计约束：
//   - 所有方法均以"逻辑路径 / 相对路径"为入参，实现内部负责拼接根目录与越界校验；
//   - 分片上传依赖随机写（Seek + WriteAt），本地与对象存储分别以降级语义实现；
//   - 下载/预览统一走 Open + ServeContent 流式，避免对象存储整对象读入内存。
package storage

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"
)

// StatInfo 文件/目录元信息（与 os.FileInfo 对齐的可移植子集）。
type StatInfo struct {
	Name    string
	Size    int64
	IsDir   bool
	Mode    os.FileMode
	ModTime time.Time
}

// ObjectInfo 对象存储列举返回的条目信息。
type ObjectInfo struct {
	Name string
	Size int64
	// IsDir 表示目录占位对象（部分对象存储以空对象或 "/" 结尾 key 表示目录）。
	IsDir bool
	// ModTime 修改时间，可能为零值（对象存储未返回）。
	ModTime time.Time
}

// ListResult 目录列举结果。
type ListResult struct {
	Objects []ObjectInfo
}

// Storage 统一的文件存储接口。
//
// 方法语义尽量对齐标准库 os 包；对于对象存储后端，不支持的语义（如目录层级、
// 随机写）通过合理降级实现，保证上层业务差异最小化。
type Storage interface {
	// Kind 返回存储类型标识（local/s3/minio/oss/webdav），用于日志与调试。
	Kind() string

	// Stat 返回文件/目录元信息；不存在时返回 *os.PathError（IsNotExist 可判定）。
	Stat(ctx context.Context, path string) (StatInfo, error)

	// Open 打开文件用于顺序流式读取（下载/预览/哈希）。本地实现返回 *os.File；
	// 对象存储实现返回流式 Reader 包装。调用方负责关闭。
	Open(ctx context.Context, path string) (io.ReadCloser, error)

	// OpenWriter 打开文件用于顺序写入（对象存储以整对象 Put 完成）。
	// 本地实现是普通文件写；对象存储实现缓冲到 Close 时提交。
	// 返回的 writer 必须调用 Close 提交。
	OpenWriter(ctx context.Context, path string) (io.WriteCloser, error)

	// ReadFile 读取整个文件内容（用于文本预览等小文件场景）。
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// MkdirAll 确保目录存在（对象存储视为无操作成功返回）。
	MkdirAll(ctx context.Context, dir string) error

	// Rename 重命名/移动文件。对象存储实现为复制+删除（非原子）。
	Rename(ctx context.Context, oldPath, newPath string) error

	// Remove 删除单个文件；不存在时返回 os.ErrNotExist 语义（IsNotExist 可判定）。
	Remove(ctx context.Context, path string) error

	// RemoveAll 递归删除目录或文件（对象存储实现为按前缀列举删除）。
	RemoveAll(ctx context.Context, path string) error

	// List 列举目录下的直接子项（不含递归），返回条目列表。
	List(ctx context.Context, dir string) (ListResult, error)

	// ServeContent 流式输出文件内容到 http.ResponseWriter（支持 Range/206 断点续传）。
	// 内部先 Stat 获取大小与修改时间，再 Open 流式写入。
	ServeContent(ctx context.Context, w http.ResponseWriter, r *http.Request, name string, path string) error
}

// IsNotExist 判断错误是否为"不存在"语义。
// 各后端均已将缺失错误归一化为 *os.PathError / os.ErrNotExist，
// 因此统一使用 os.IsNotExist 判定，上层无需感知后端差异。
func IsNotExist(err error) bool {
	return os.IsNotExist(err)
}
