package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// doGET 在隔离的 gin 上下文上执行一次 GET 请求，返回响应记录器
func doGET(h *Handler, method, path, query string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rr := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rr)
	req := httptest.NewRequest(method, path+"?"+query, nil)
	ctx.Request = req
	switch method {
	case "Autocomplete":
		h.Autocomplete(ctx)
	case "SpellCheck":
		h.SpellCheck(ctx)
	}
	return rr
}

// decodeData 解析 utils.HandleSuccess 的响应体，取 data 字段（string 数组）
func decodeData(t *testing.T, rr *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v, body=%s", err, rr.Body.String())
	}
	return body.Data
}

func TestAutocompleteHandler(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	if err := idx.IndexFile(1, "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexFile(2, "年度财务报表.pdf"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(idx)

	// 正常前缀（返回关键词，而非完整文件名）
	rr := doGET(h, "Autocomplete", "/api/v1/search-suggest", "q=read")
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	names := decodeData(t, rr)
	if len(names) == 0 || names[0] != "readme" {
		t.Fatalf("q=read 应建议关键词 readme，实际 %v", names)
	}

	// 空 q 返回空数组
	rr = doGET(h, "Autocomplete", "/api/v1/search-suggest", "q=")
	names = decodeData(t, rr)
	if names == nil {
		t.Fatal("空 q 应返回空数组而非 null")
	}
}

func TestSpellCheckHandler(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	if err := idx.IndexFile(1, "README.md"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(idx)

	rr := doGET(h, "SpellCheck", "/api/v1/search-spellcheck", "q=REEDME")
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	names := decodeData(t, rr)
	found := false
	for _, n := range names {
		if n == "readme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("REEDME 纠错应含关键词 readme，实际 %v", names)
	}
}

func TestNilIndexHandler(t *testing.T) {
	// idx 为 nil 时接口应返回空数组而不 panic
	h := NewHandler(nil)
	rr := doGET(h, "Autocomplete", "/api/v1/search-suggest", "q=read")
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rr.Code)
	}
	if names := decodeData(t, rr); names == nil {
		t.Fatal("nil 索引应返回空数组")
	}
	rr = doGET(h, "SpellCheck", "/api/v1/search-spellcheck", "q=REEDME")
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rr.Code)
	}
}
