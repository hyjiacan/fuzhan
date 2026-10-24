package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage 本地磁盘存储实现。
// 直接基于 os 包操作真实文件系统，是默认的后端，语义与原先 appconfig.RootNames
// 下的物理路径完全一致。
type LocalStorage struct {
	// rootPath 本存储实例持有的根目录（物理绝对路径）。
	rootPath string
}

// NewLocalStorage 创建本地存储实例。rootPath 为物理根目录。
func NewLocalStorage(rootPath string) *LocalStorage {
	return &LocalStorage{rootPath: filepath.Clean(rootPath)}
}

// Kind 返回存储类型标识。
func (l *LocalStorage) Kind() string { return "local" }

// resolve 将逻辑路径拼接为物理绝对路径，并做越界校验。
// path 为空或 "/" 时返回根目录本身。
func (l *LocalStorage) resolve(path string) (string, error) {
	if path == "" || path == "/" {
		return l.rootPath, nil
	}
	clean := filepath.Clean(strings.TrimPrefix(path, "/"))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("路径越界")
	}
	full := filepath.Join(l.rootPath, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("路径解析失败: %w", err)
	}
	absRoot, err := filepath.Abs(l.rootPath)
	if err != nil {
		return "", fmt.Errorf("根路径解析失败: %w", err)
	}
	if abs != absRoot && !strings.HasPrefix(abs, absRoot+string(filepath.Separator)) {
		return "", errors.New("路径越界")
	}
	return abs, nil
}

// Stat 返回文件/目录元信息。
func (l *LocalStorage) Stat(ctx context.Context, path string) (StatInfo, error) {
	full, err := l.resolve(path)
	if err != nil {
		return StatInfo{}, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		return StatInfo{}, err
	}
	return StatInfo{
		Name:    fi.Name(),
		Size:    fi.Size(),
		IsDir:   fi.IsDir(),
		Mode:    fi.Mode(),
		ModTime: fi.ModTime(),
	}, nil
}

// Open 打开文件用于顺序读取（返回 *os.File，可直接参与 io 流）。
func (l *LocalStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	full, err := l.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

// OpenWriter 打开文件用于顺序写入（不存在则创建，已存在则截断）。
func (l *LocalStorage) OpenWriter(ctx context.Context, path string) (io.WriteCloser, error) {
	full, err := l.resolve(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return os.Create(full)
}

// ReadFile 读取整个文件内容。
func (l *LocalStorage) ReadFile(ctx context.Context, path string) ([]byte, error) {
	full, err := l.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// MkdirAll 确保目录存在。
func (l *LocalStorage) MkdirAll(ctx context.Context, dir string) error {
	full, err := l.resolve(dir)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0755)
}

// Rename 重命名/移动文件。
func (l *LocalStorage) Rename(ctx context.Context, oldPath, newPath string) error {
	fullOld, err := l.resolve(oldPath)
	if err != nil {
		return err
	}
	fullNew, err := l.resolve(newPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullNew), 0755); err != nil {
		return err
	}
	return os.Rename(fullOld, fullNew)
}

// Remove 删除单个文件。
func (l *LocalStorage) Remove(ctx context.Context, path string) error {
	full, err := l.resolve(path)
	if err != nil {
		return err
	}
	return os.Remove(full)
}

// RemoveAll 递归删除目录或文件。
func (l *LocalStorage) RemoveAll(ctx context.Context, path string) error {
	full, err := l.resolve(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(full)
}

// List 列举目录下的直接子项。
func (l *LocalStorage) List(ctx context.Context, dir string) (ListResult, error) {
	full, err := l.resolve(dir)
	if err != nil {
		return ListResult{}, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Objects: make([]ObjectInfo, 0, len(entries))}
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		result.Objects = append(result.Objects, ObjectInfo{
			Name:    e.Name(),
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			ModTime: info.ModTime(),
		})
	}
	return result, nil
}

// ServeContent 流式输出文件内容，支持 Range/206 断点续传。
func (l *LocalStorage) ServeContent(ctx context.Context, w http.ResponseWriter, r *http.Request, name, path string) error {
	info, err := l.Stat(ctx, path)
	if err != nil {
		return err
	}
	if info.IsDir {
		return errors.New("目标为目录")
	}
	file, err := l.Open(ctx, path)
	if err != nil {
		return err
	}
	defer file.Close()
	// 本地实现 Open 返回 *os.File，天然满足 io.ReadSeeker（ServeContent 依赖 Seek 支持 Range）。
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		return errors.New("存储实现不支持随机读取")
	}
	modTime := info.ModTime
	http.ServeContent(w, r, name, modTime, seeker)
	return nil
}
