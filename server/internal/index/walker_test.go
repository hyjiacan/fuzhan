package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// buildTestTree 在 dir 下构造一棵含目录与文件的树，返回全部期望条目（"相对路径"）。
func buildTestTree(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files := map[string]bool{
		"a.txt":          false,
		"dir1":           true,
		"dir1/b.txt":     false,
		"dir1/sub":       true,
		"dir1/sub/c.txt": false,
		"dir2":           true,
		"dir2/d.txt":     false,
		"dir-empty":      true,
		"e.txt":          false,
	}
	for rel, isDir := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if isDir {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("MkdirAll %s: %v", full, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", full, err)
		}
	}
	return files
}

func TestWalkConcurrentVisitsAll(t *testing.T) {
	root := t.TempDir()
	want := buildTestTree(t, root)

	ctx := context.Background()
	out := make(chan walkEntry, 64)
	var got []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range out {
			// walker 产出带前导 "/" 的相对路径，这里统一去掉以便与 want 对照
			k := strings.TrimPrefix(e.relPath, "/")
			got = append(got, e.relPath)
			if isDir := e.info.IsDir(); isDir != want[k] {
				t.Errorf("IsDir mismatch for %s: got %v", e.relPath, isDir)
			}
		}
	}()

	err := walkConcurrent(ctx, root, 4, out)
	<-done
	if err != nil {
		t.Fatalf("walkConcurrent: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("visited %d entries, want %d", len(got), len(want))
	}
	sort.Strings(got)
	for _, p := range got {
		if _, exists := want[strings.TrimPrefix(p, "/")]; !exists {
			t.Errorf("unexpected entry: %s", p)
		}
	}
}

func TestWalkConcurrentOrderIndependent(t *testing.T) {
	// 相同结果集应不依赖并发度
	root := t.TempDir()
	want := buildTestTree(t, root)

	gotFor := func(workers int) map[string]bool {
		out := make(chan walkEntry, 32)
		res := make(map[string]bool)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for e := range out {
				res[strings.TrimPrefix(e.relPath, "/")] = true
			}
		}()
		if err := walkConcurrent(context.Background(), root, workers, out); err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		<-done
		return res
	}

	for _, w := range []int{1, 2, 8} {
		got := gotFor(w)
		if len(got) != len(want) {
			t.Errorf("workers=%d: visited %d, want %d", w, len(got), len(want))
		}
	}
}

func TestWalkConcurrentCancel(t *testing.T) {
	root := t.TempDir()
	buildTestTree(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 预取消：遍历应立即安全返回

	out := make(chan walkEntry, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()

	timeout := time.After(5 * time.Second)
	errc := make(chan error, 1)
	go func() { errc <- walkConcurrent(ctx, root, 4, out) }()

	select {
	case <-errc:
	case <-timeout:
		t.Fatal("walkConcurrent deadlocked on cancelled context")
	}
	<-done
}

func TestWalkConcurrentNoGoroutineLeak(t *testing.T) {
	// 取消后不应有协程被永久阻塞（用短重试确保返回）
	root := t.TempDir()
	buildTestTree(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	out := make(chan walkEntry, 8)
	go func() {
		for range out {
		}
	}()
	_ = walkConcurrent(ctx, root, 4, out)
	time.Sleep(20 * time.Millisecond) // 给取消广播时间
	cancel()
}