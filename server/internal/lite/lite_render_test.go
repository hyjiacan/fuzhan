package lite

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/search"

	"github.com/gin-gonic/gin"
)

// TestBrowseRender 验证 /lite 根级与子目录浏览渲染。
func TestBrowseRender(t *testing.T) {
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
	Handle(w, req, nil, nil)
	body := w.Body.String()
	for _, want := range []string{"root", "/lite/root", "根目录"} {
		if !strings.Contains(body, want) {
			t.Errorf("根级页面缺少 %q\n%s", want, body)
		}
	}

	// 进入根目录：应包含 file.txt、dir1（已移除"上级目录"行）
	req = httptest.NewRequest("GET", "/lite/root", nil)
	w = httptest.NewRecorder()
	Handle(w, req, nil, nil)
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
	if !strings.Contains(body, "轻量版 / root") {
		t.Errorf("子目录页面标题未随路径变化，期望含「轻量版 / root」\n%s", body)
	}
}

// TestSecurity 验证路径遍历拦截与不存在的根目录。
func TestSecurity(t *testing.T) {
	tmp := t.TempDir()
	oldNames := appconfig.RootNames
	appconfig.RootNames = map[string]string{"root": tmp}
	defer func() { appconfig.RootNames = oldNames }()

	req := httptest.NewRequest("GET", "/lite/root/../etc/passwd", nil)
	w := httptest.NewRecorder()
	Handle(w, req, nil, nil)
	if w.Code != 403 {
		t.Errorf("期望 403 拦截路径遍历，实际 %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/lite/not-exist", nil)
	w = httptest.NewRecorder()
	Handle(w, req, nil, nil)
	if w.Code != 400 {
		t.Errorf("期望 400 不存在的根目录，实际 %d", w.Code)
	}
}

// TestAuthPageRender 验证登录/注册认证页模板可解析渲染，
// 且登录页含「去注册」链接、注册页含「去登录」链接。
func TestAuthPageRender(t *testing.T) {
	oldCfg := appconfig.GlobalConfig
	defer func() { appconfig.GlobalConfig = oldCfg }()
	appconfig.GlobalConfig.Account.AllowRegistration = true

	gin.SetMode(gin.TestMode)

	tmpl, err := authTmpl()
	if err != nil {
		t.Fatalf("解析认证页模板失败: %v", err)
	}
	v := authErrView{
		Title:    "测试 - 登录 - 轻量版",
		AppName:  "测试",
		Heading:  "登录",
		Redirect: "/lite/dir",
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, v); err != nil {
		t.Fatalf("渲染登录页失败: %v", err)
	}
	body := buf.String()
	// 页间跳转链接：Redirect 由 html/template 自动做 URL 上下文转义（/ 会随上下文编码），
	// 只需断言链接携带 redirect 参数名且指向注册页即可；无 Redirect 时不应出现 ?redirect=
	for _, want := range []string{"去注册", "/lite/login", "/lite/register"} {
		if !strings.Contains(body, want) {
			t.Errorf("登录页缺少 %q\n%s", want, body)
		}
	}
	if !strings.Contains(body, "?redirect=") {
		t.Errorf("登录页应携带 redirect 查询参数\n%s", body)
	}

	// 「允许注册」关闭：登录页不应再出现「去注册」
	appconfig.GlobalConfig.Account.AllowRegistration = false
	buf.Reset()
	if err := tmpl.Execute(&buf, v); err != nil {
		t.Fatalf("渲染登录页失败: %v", err)
	}
	if strings.Contains(buf.String(), "去注册") {
		t.Errorf("关闭「允许注册」后登录页不应出现「去注册」\n%s", buf.String())
	}
	appconfig.GlobalConfig.Account.AllowRegistration = true

	v.Heading = "注册"
	buf.Reset()
	if err := tmpl.Execute(&buf, v); err != nil {
		t.Fatalf("渲染注册页失败: %v", err)
	}
	body = buf.String()
	if !strings.Contains(body, "去登录") || !strings.Contains(body, "/lite/register") {
		t.Errorf("注册页应含「去登录」且表单 action 指向 /lite/register\n%s", body)
	}
}

func TestSearchRel(t *testing.T) {
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
		if got := searchRel(c.path, c.rootName); got != c.want {
			t.Errorf("searchRel(%q, %q)=%q，期望 %q", c.path, c.rootName, got, c.want)
		}
	}
}

// TestBuildSuggestions 验证检索推荐/纠错的生成：索引可用时产出超链接，
// 且排除当前查询词、Href 为 /lite?q= 格式；索引为 nil 时返回空。
func TestBuildSuggestions(t *testing.T) {
	r, c := buildSuggestions(nil, "x")
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

	recommends, corrections := buildSuggestions(idx, "REEDME")
	merged := append(append([]suggestion{}, recommends...), corrections...)
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
