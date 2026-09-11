package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func dummy(c *gin.Context) {}

// TestMainRouteTreeNoConflict 复刻 main.go 中 /api/v1 与根路由的整棵路由注册
// （方法+路径，handler 用 dummy），触发 gin 路由树构建，任何 catch-all/static/param
// 冲突都会在这里 panic。用于确认 WebDAV 修复后再无同类路径问题。
func TestMainRouteTreeNoConflict(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	api := r.Group("/api/v1")
	api.GET("/health", dummy)
	api.GET("/ready", dummy)
	api.GET("/metrics", dummy)
	api.GET("/monitor/storage", dummy)
	api.GET("/monitor/access", dummy)
	api.GET("/monitor/keywords", dummy)
	api.GET("/monitor/recent", dummy)
	api.GET("/monitor/rankings", dummy)
	api.GET("/monitor/hot-downloads", dummy)
	api.GET("/options", dummy)
	api.GET("/files/search-records", dummy)
	api.GET("/files/list", dummy)
	api.GET("/files/depends/:id", dummy)
	api.GET("/download/*path", dummy)
	api.HEAD("/download/*path", dummy)
	api.GET("/search/*query", dummy)

	auth := api.Group("/auth")
	auth.POST("/register", dummy)
	auth.POST("/login", dummy)

	r.GET("/share/:code", dummy)
	r.HEAD("/share/:code", dummy)

	private := api.Group("/private")
	private.GET("/files", dummy)
	private.GET("/quota", dummy)
	private.POST("/upload", dummy)
	private.DELETE("/files/:code", dummy)
	pu := private.Group("/uploads")
	pu.POST("/session", dummy)
	pu.GET("/session/:id", dummy)
	pu.POST("/session/:id/resume", dummy)
	pu.DELETE("/session/:id", dummy)
	pu.POST("/chunk", dummy)
	pu.POST("/finalize", dummy)

	api.GET("/admin/search/*query", dummy)
	api.GET("/admin/download/*path", dummy)
	api.HEAD("/admin/download/*path", dummy)

	scan := api.Group("/admin/index")
	scan.POST("/scan", dummy)
	scan.POST("/scan/trigger", dummy)
	scan.GET("/scan/progress", dummy)
	scan.GET("/scan/status", dummy)

	admin := api.Group("/admin")
	admin.GET("/users", dummy)
	admin.PUT("/users/:uuid/reset-password", dummy)
	admin.PUT("/users/:uuid/disabled", dummy)
	admin.DELETE("/users/:uuid", dummy)
	admin.GET("/sessions", dummy)
	admin.POST("/sessions/cleanup", dummy)
	admin.POST("/records/clear", dummy)
	admin.POST("/upload-cert", dummy)
	admin.POST("/upload-key", dummy)
	admin.GET("/files/list", dummy)
	admin.POST("/files/move", dummy)
	admin.DELETE("/files", dummy)
	admin.GET("/url-tasks", dummy)
	admin.POST("/url-tasks/:id/retry", dummy)
	admin.DELETE("/url-tasks/:id", dummy)
	admin.GET("/tasks", dummy)
	admin.GET("/tasks/history", dummy)
	admin.GET("/tasks/:id", dummy)
	admin.POST("/tasks/:id/cancel", dummy)
	ak := admin.Group("/api-keys")
	ak.GET("", dummy)
	ak.POST("", dummy)
	ak.GET("/:id", dummy)
	ak.PUT("/:id/status", dummy)
	ak.DELETE("/:id", dummy)
	idx := admin.Group("/index")
	idx.GET("/records", dummy)
	idx.GET("/stats", dummy)
	idx.POST("/check", dummy)
	idx.DELETE("/records/:id", dummy)
	idx.GET("/duplicates", dummy)
	idx.POST("/duplicates/:id/keep", dummy)
	idx.PUT("/records/:id/notes", dummy)
	idx.GET("/dependencies", dummy)
	idx.POST("/dependencies", dummy)
	idx.GET("/records/:id/dependencies", dummy)
	idx.DELETE("/dependencies/:id", dummy)
	admin.GET("/open-api/stats", dummy)

	setup := api.Group("/setup")
	setup.GET("/status", dummy)
	setup.POST("/save", dummy)
	setup.POST("/validate-dir", dummy)
	setup.GET("/network/interfaces", dummy)
	setup.GET("/default-config", dummy)

	notif := api.Group("/notifications")
	notif.GET("", dummy)
	notif.POST("/read", dummy)
	notif.POST("/merge", dummy)

	cfgG := api.Group("/config")
	cfgG.GET("", dummy)
	cfgG.POST("", dummy)

	temp := api.Group("/temp")
	temp.GET("/list", dummy)
	temp.POST("/upload", dummy)
	temp.GET("/quota", dummy)
	temp.GET("/client-ip", dummy)
	temp.GET("/:code", dummy)
	temp.GET("/:code/download", dummy)
	temp.HEAD("/:code/download", dummy)
	temp.DELETE("/:code", dummy)
	tu := api.Group("/temp/upload")
	tu.POST("/session", dummy)
	tu.GET("/session/:uploadId", dummy)
	tu.POST("/session/:uploadId/resume", dummy)
	tu.DELETE("/session/:uploadId", dummy)
	tu.POST("/chunk", dummy)
	tu.POST("/finalize", dummy)

	uploads := api.Group("/uploads")
	uploads.POST("/session", dummy)
	uploads.GET("/session/:id", dummy)
	uploads.POST("/session/:id/resume", dummy)
	uploads.DELETE("/session/:id", dummy)
	uploads.POST("/chunk", dummy)
	uploads.POST("/finalize", dummy)
	uploads.POST("/url", dummy)
	uploads.GET("/url-task/:taskId", dummy)
	uploads.POST("/url_info", dummy)
	uploads.GET("/sessions", dummy)
	uploads.GET("/url-tasks", dummy)
	uploads.POST("/url-tasks/:taskId/cancel", dummy)
	uploads.POST("/url-tasks/:taskId/retry", dummy)
	uploads.DELETE("/url-tasks/:taskId", dummy)

	files := api.Group("/files")
	files.GET("/recent", dummy)
	files.GET("/recent/carousel", dummy)
	files.GET("/preview/*path", dummy)
	files.GET("/preview-chunk/*path", dummy)
	files.PUT("/records/:id/notes", dummy)
	files.GET("/records/:id/notes", dummy)
	files.POST("/dependencies", dummy)
	files.DELETE("/dependencies/:id", dummy)
	files.GET("/record", dummy)
	files.GET("/scan/status", dummy)

	protected := api.Group("/")
	protected.POST("/auth/refresh", dummy)
	protected.GET("/auth/user", dummy)
	protected.PUT("/auth/password", dummy)
	pf := protected.Group("/files")
	pf.POST("/rename", dummy)
	pf.POST("/move", dummy)
	protected.GET("/get_file_info", dummy)
	pfe := protected.Group("/examples")
	pfe.POST("/upload", dummy)
	pfe.POST("/rename", dummy)
	pfe.POST("/validation/upload", dummy)

	wd := api.Group("/webdav")
	for _, m := range []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS", "PROPFIND", "PROPPATCH", "MKCOL", "MOVE", "COPY", "LOCK", "UNLOCK"} {
		wd.Handle(m, "/*path", dummy)
		wd.Handle(m, "", dummy)
	}

	cli := api.Group("/cli")
	cli.GET("/", dummy)
	cli.GET("/search/*query", dummy)
	cli.GET("/list/*path", dummy)
	cli.GET("/install.sh", dummy)

	cliAlias := r.Group("/cli")
	cliAlias.GET("/", dummy)
	cliAlias.GET("/search/*query", dummy)
	cliAlias.GET("/list/*path", dummy)
	cliAlias.GET("/install.sh", dummy)

	downloadAlias := r.Group("/download")
	downloadAlias.GET("/*path", dummy)
	downloadAlias.HEAD("/*path", dummy)

	simple := r.Group("/simple")
	simple.GET("/*path", dummy)

	openAPI := r.Group("/api/open/v1")
	openAPI.GET("/files/list", dummy)
	openAPI.GET("/files/search", dummy)
	openAPI.GET("/files/download/*path", dummy)
	openAPI.GET("/files/duplicates", dummy)
	openAPI.GET("/files/notes", dummy)
	openAPI.GET("/files/:id/depends", dummy)
	openAPI.GET("/openapi.json", dummy)
	openAPI.GET("/docs", dummy)

	// 触发路由树最终化；若存在任何同类冲突，这里会 panic
	_ = r.Routes()
}

// TestCLIRedirectNoLoop 验证 /cli 与 /cli/ 之间不存在无限重定向：
// /cli 应被 Gin 重定向到 /cli/（一次），/cli/ 直接命中 handler 返回 200，
// 两者不再互相跳转。回归用例：曾注册为 GET("") 导致 /cli→/cli/→/cli 循环。
func TestCLIRedirectNoLoop(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	handle := func(c *gin.Context) {
		c.String(http.StatusOK, "help")
	}

	// 与 registerTopLevelRoutes 中 /cli 别名保持一致
	cliAlias := r.Group("/cli")
	{
		cliAlias.GET("/", handle)
		cliAlias.GET("/search/*query", handle)
		cliAlias.GET("/list/*path", handle)
	}
	_ = r.Routes()

	// /cli 无尾斜杠 → gin 301 重定向到 /cli/（single redirect）
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/cli", nil))
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("/cli 期望 %d 重定向，实际 %d", http.StatusMovedPermanently, w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/cli/" {
		t.Fatalf("/cli 期望重定向到 /cli/，实际 Location=%q", loc)
	}

	// /cli/ 带尾斜杠 → 直接命中 handler，返回 200，不应再重定向
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/cli/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/cli/ 期望 200，实际 %d (Location=%q)",
			w.Code, w.Header().Get("Location"))
	}
}

// TestIsIEBrowser 验证 IE/Trident 判定，并排除 Edge（Chromium/旧版）与 curl 等非浏览器。
func TestIsIEBrowser(t *testing.T) {
	cases := []struct {
		ua   string
		want bool
	}{
		{"Mozilla/5.0 (compatible; MSIE 9.0; Windows NT 6.1; Trident/5.0)", true},
		{"Mozilla/5.0 (Windows NT 10.0; Trident/7.0; rv:11.0) like Gecko", true},
		{"Mozilla/5.0 (Windows NT 6.1; WOW64; Trident/7.0; rv:11.0) like Gecko", true},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edge/18.10240", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", false},
		{"curl/8.0.1", false},
		{"wget/1.21", false},
		{"", false},
		{"some generic client", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("User-Agent", c.ua)
		if got := isIEBrowser(req); got != c.want {
			t.Errorf("isIEBrowser(%q) = %v, 期望 %v", c.ua, got, c.want)
		}
	}
}
