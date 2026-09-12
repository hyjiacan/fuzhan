package cli

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/search"
)

// TestLiteBrowseRender 验证 /lite 根级与子目录浏览渲染。
func TestLiteBrowseRender(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "dir1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldNames := appconfig.RootNames
	appconfig.RootNames = map[string]string{"root": tmp}
	defer func() { appconfig.RootNames = oldNames }()

	// 根级：列出根目录
	req := httptest.NewRequest("GET", "/lite", nil)
	w := httptest.NewRecorder()
	HandleLite(w, req, nil, nil)
	body := w.Body.String()
	for _, want := range []string{"root", "/lite/root", "根目录"} {
		if !strings.Contains(body, want) {
			t.Errorf("根级页面缺少 %q\n%s", want, body)
		}
	}

	// 进入根目录：应包含 file.txt、dir1（已移除"上级目录"行）
	req = httptest.NewRequest("GET", "/lite/root", nil)
	w = httptest.NewRecorder()
	HandleLite(w, req, nil, nil)
	body = w.Body.String()
	for _, want := range []string{"file.txt", "dir1", "/download/root/file.txt", "/lite/root/dir1", "根目录"} {
		if !strings.Contains(body, want) {
			t.Errorf("子目录页面缺少 %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "上级目录") {
		t.Errorf("子目录页面不应再包含上级目录行\n%s", body)
	}

	// 页面标题应随当前路径变化
	if !strings.Contains(body, "简洁浏览 / root") {
		t.Errorf("子目录页面标题未随路径变化，期望含「简洁浏览 / root」\n%s", body)
	}
}

// TestLiteSecurity 验证路径遍历拦截与不存在的根目录。
func TestLiteSecurity(t *testing.T) {
	tmp := t.TempDir()
	oldNames := appconfig.RootNames
	appconfig.RootNames = map[string]string{"root": tmp}
	defer func() { appconfig.RootNames = oldNames }()

	req := httptest.NewRequest("GET", "/lite/root/../etc/passwd", nil)
	w := httptest.NewRecorder()
	HandleLite(w, req, nil, nil)
	if w.Code != 403 {
		t.Errorf("期望 403 拦截路径遍历，实际 %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/lite/not-exist", nil)
	w = httptest.NewRecorder()
	HandleLite(w, req, nil, nil)
	if w.Code != 400 {
		t.Errorf("期望 400 不存在的根目录，实际 %d", w.Code)
	}
}

// TestLiteSearchRel 验证检索完整路径还原为相对路径，避免 encodeRelPath 二次前置 rootName。
func TestLiteSearchRel(t *testing.T) {
	cases := []struct {
		path, rootName, want string
	}{
		{"/root/sub/file.txt", "root", "/sub/file.txt"},
		{"/root/file.txt", "root", "/file.txt"},
		{"/root/中文 文件名.txt", "root", "/中文 文件名.txt"},
		{"", "root", ""},
		{"/root", "root", ""},
		{"/other/sub/file.txt", "root", ""},
		{"/root/sub/file.txt", "", ""},
	}
	for _, c := range cases {
		if got := liteSearchRel(c.path, c.rootName); got != c.want {
			t.Errorf("liteSearchRel(%q, %q)=%q，期望 %q", c.path, c.rootName, got, c.want)
		}
	}
}

// TestBuildLiteSuggestions 验证检索推荐/纠错的生成：索引可用时产出超链接，
// 且排除当前查询词、Href 为 /lite?q= 格式；索引为 nil 时返回空。
func TestBuildLiteSuggestions(t *testing.T) {
	r, c := buildLiteSuggestions(nil, "x")
	if len(r) != 0 || len(c) != 0 {
		t.Fatalf("nil 索引应返回空，实际 推荐=%v 纠错=%v", r, c)
	}

	idx, err := search.OpenIndex(t.TempDir())
	if err != nil {
		t.Skipf("无法打开检索索引: %v", err)
	}
	defer idx.CloseWriter()
	for id, name := range []string{"README.md", "README_EN.md", "ISO9001质量体系文档.docx"} {
		if err := idx.IndexFile(int64(id+1), name); err != nil {
			t.Fatal(err)
		}
	}

	recommends, corrections := buildLiteSuggestions(idx, "REEDME")
	merged := append(append([]liteSuggestion{}, recommends...), corrections...)
	if len(merged) == 0 {
		t.Fatal("索引内含可命中项，推荐/纠错不应为空")
	}
	for _, s := range merged {
		if s.Keyword == "REEDME" {
			t.Fatalf("不应包含当前查询词：%s", s.Keyword)
		}
		if !strings.HasPrefix(s.Href, "/lite?q=") {
			t.Fatalf("Href 应指向 /lite?q= 重新检索，实际 %s", s.Href)
		}
	}
}
