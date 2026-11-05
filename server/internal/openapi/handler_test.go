package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupOpenAPIHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.FileRecordPublic{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

func newTestContext(method, target string, scopes []string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	if scopes != nil {
		c.Set(scopesContextKey, scopes)
	}
	return c, w
}

func TestListFilesRejectsInvalidParams(t *testing.T) {
	db := setupOpenAPIHandlerDB(t)
	h := NewHandler(db, nil, nil, map[string]string{}, nil, nil, nil, nil, 0)

	invalid := []string{
		"/?page=0",
		"/?page=abc",
		"/?pageSize=0",
		"/?pageSize=101",
		"/?sort=foo",
		"/?order=bad",
		"/?path=/docs/../other",
	}
	for _, target := range invalid {
		c, w := newTestContext(http.MethodGet, target, nil)
		h.ListFiles(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("目标 %s: 期望 400，实际 %d，响应 %s", target, w.Code, w.Body.String())
		}
	}

	valid := []string{"/", "/?page=1&pageSize=10&sort=size&order=desc", "/?path=/docs"}
	for _, target := range valid {
		c, w := newTestContext(http.MethodGet, target, nil)
		h.ListFiles(c)
		if w.Code != http.StatusOK {
			t.Errorf("目标 %s: 期望 200，实际 %d，响应 %s", target, w.Code, w.Body.String())
		}
	}
}

func TestListFilesDefaultsWhenParamsAbsent(t *testing.T) {
	db := setupOpenAPIHandlerDB(t)
	h := NewHandler(db, nil, nil, map[string]string{}, nil, nil, nil, nil, 0)

	c, w := newTestContext(http.MethodGet, "/", nil)
	h.ListFiles(c)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Path       string `json:"path"`
			Pagination struct {
				Page     int64 `json:"page"`
				PageSize int64 `json:"page_size"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !body.Success || body.Data.Path != "/" || body.Data.Pagination.Page != 1 || body.Data.Pagination.PageSize != 50 {
		t.Fatalf("默认值不符合预期: %s", w.Body.String())
	}
}

func TestOptionalIntQueryRejectsIllegalValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		def  int64
		min  int64
		max  int64
	}{
		{"minSize", "-1", 0, 0, 0},
		{"page", "0", 1, 1, 0},
		{"pageSize", "101", 50, 1, 100},
		{"pageSize", "abc", 50, 1, 100},
	}
	for _, tc := range cases {
		c, w := newTestContext(http.MethodGet, "/?"+tc.name+"="+tc.raw, nil)
		if _, ok := optionalIntQuery(c, tc.name, tc.def, tc.min, tc.max); ok {
			t.Errorf("%s=%s: 期望拒绝，实际接受", tc.name, tc.raw)
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=%s: 期望 400，实际 %d", tc.name, tc.raw, w.Code)
		}
	}

	c, _ := newTestContext(http.MethodGet, "/", nil)
	if v, ok := optionalIntQuery(c, "minSize", 0, 0, 0); !ok || v != 0 {
		t.Fatalf("缺省 minSize 应返回默认值 0，实际 %d，ok=%v", v, ok)
	}
}

func TestParseSearchExtensionsRejectsIllegalValues(t *testing.T) {
	invalid := []string{"pdf,,docx", "a*", "abcdefghijklmnopqrstuvwxyz", "1,2,3,4,5,6,7,8,9,10,11"}
	for _, raw := range invalid {
		if _, err := parseSearchExtensions(raw); err == nil {
			t.Errorf("ext=%q: 期望返回错误，实际接受", raw)
		}
	}

	got, err := parseSearchExtensions(".PDF, docx")
	if err != nil {
		t.Fatalf("合法扩展名解析失败: %v", err)
	}
	if len(got) != 2 || got[0] != "pdf" || got[1] != "docx" {
		t.Fatalf("扩展名解析不符合预期: %#v", got)
	}
}

func TestDeleteFilePermissionSplit(t *testing.T) {
	root := t.TempDir()
	// FileService.resolvePath 依赖全局根目录映射，测试中临时注册并在结束后恢复。
	origin := appconfig.RootNames
	appconfig.RootNames = map[string]string{"docs": root}
	t.Cleanup(func() { appconfig.RootNames = origin })

	storages := map[string]storage.Storage{"docs": storage.NewLocalStorage(root)}
	h := NewHandler(nil, nil, nil, map[string]string{"docs": root}, storages,
		nil, services.NewFileService(storages), nil, 0)

	prepare := func(t *testing.T) {
		t.Helper()
		_ = os.RemoveAll(filepath.Join(root, "dir"))
		_ = os.Remove(filepath.Join(root, "file.txt"))
		if err := os.MkdirAll(filepath.Join(root, "dir", "sub"), 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("创建文件失败: %v", err)
		}
	}

	cases := []struct {
		name          string
		scopes        []string
		path          string
		status        int
		expectDeleted string
		mustExist     string
	}{
		{"文件权限删文件", []string{models.ScopeFilesDelete}, "/docs/file.txt", http.StatusOK, filepath.Join(root, "file.txt"), ""},
		{"文件权限删目录", []string{models.ScopeFilesDelete}, "/docs/dir", http.StatusForbidden, "", filepath.Join(root, "dir")},
		{"目录权限删目录", []string{models.ScopeFilesDeleteDir}, "/docs/dir", http.StatusOK, filepath.Join(root, "dir"), ""},
		{"目录权限删文件", []string{models.ScopeFilesDeleteDir}, "/docs/file.txt", http.StatusForbidden, "", filepath.Join(root, "file.txt")},
		{"无权限删文件", nil, "/docs/file.txt", http.StatusForbidden, "", filepath.Join(root, "file.txt")},
		{"目标不存在", []string{models.ScopeFilesDelete}, "/docs/missing.txt", http.StatusNotFound, "", ""},
		{"删除根目录", []string{models.ScopeFilesDeleteDir}, "/docs", http.StatusBadRequest, "", filepath.Join(root, "dir")},
		{"非法路径", []string{models.ScopeFilesDeleteDir}, "/docs/../etc", http.StatusBadRequest, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepare(t)
			c, w := newTestContext(http.MethodDelete, "/?path="+tc.path, tc.scopes)
			h.DeleteFile(c)
			if w.Code != tc.status {
				t.Fatalf("期望 %d，实际 %d，响应 %s", tc.status, w.Code, w.Body.String())
			}
			if tc.expectDeleted != "" {
				if _, err := os.Stat(tc.expectDeleted); !os.IsNotExist(err) {
					t.Fatalf("目标 %s 应被删除，实际仍存在或检查失败: %v", tc.expectDeleted, err)
				}
			}
			if tc.mustExist != "" {
				if _, err := os.Stat(tc.mustExist); err != nil {
					t.Fatalf("目标 %s 不应被删除: %v", tc.mustExist, err)
				}
			}
		})
	}
}
