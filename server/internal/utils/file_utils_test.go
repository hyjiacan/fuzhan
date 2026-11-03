package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// createDirLink 创建目录链接（Windows 用 junction，其余平台用符号链接）。
func createDirLink(link, target string) error {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
		return nil
	}
	return os.Symlink(target, link)
}

func TestEnsureNoSymlinkPath(t *testing.T) {
	root := t.TempDir()

	// 普通目录与文件：允许
	dir := filepath.Join(root, "docs")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNoSymlinkPath(root, filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("普通路径不应报错: %v", err)
	}

	// 尚未创建的层级：允许（MkdirAll 会创建普通目录）
	if err := EnsureNoSymlinkPath(root, filepath.Join(root, "new", "sub", "b.txt")); err != nil {
		t.Fatalf("未创建的路径不应报错: %v", err)
	}

	// 越出根目录：拒绝
	if err := EnsureNoSymlinkPath(root, filepath.Join(root, "..", "outside.txt")); err == nil {
		t.Fatal("越界路径应被拒绝")
	}

	// 任一层是链接：拒绝
	link := filepath.Join(root, "link")
	if err := createDirLink(link, dir); err != nil {
		t.Skipf("当前环境无法创建目录链接，跳过: %v", err)
	}
	if err := EnsureNoSymlinkPath(root, filepath.Join(link, "a.txt")); err == nil {
		t.Fatal("符号链接路径应被拒绝")
	}
	if err := EnsureNoSymlinkPath(root, filepath.Join(link, "not-exist.txt")); err == nil {
		t.Fatal("符号链接下的路径应被拒绝")
	}
}
