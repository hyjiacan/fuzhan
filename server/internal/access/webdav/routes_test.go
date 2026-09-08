package webdav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestSetupUnifiedRouterAtSubgroupNoConflict 复现 main.go 的集成方式：
// /api/v1 下先有同级静态路由（以 'a' 开头），WebDAV 挂载到 /api/v1/webdav 子组。
// 若误将 WebDAV 通配符直接挂到 /api/v1 层，gin 会因 catch-all 与静态路由冲突而 panic。
func TestSetupUnifiedRouterAtSubgroupNoConflict(t *testing.T) {
	tmpDir := t.TempDir()
	root1 := filepath.Join(tmpDir, "share1")
	if err := os.MkdirAll(root1, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	publicFS := NewPublicFileSystem(map[string]string{"share1": root1})
	unifiedFS := NewUnifiedFileSystem(publicFS, nil)
	davHandler := NewHandler("/api/v1/webdav", unifiedFS)
	authenticate := UserAuthenticator(func(username, password string) (string, bool, error) {
		return username, true, nil
	})

	r := gin.New()
	api := r.Group("/api/v1")
	// 与 main.go 相同的既有同级静态路由（'a' 开头段）
	api.GET("/avatars/:name", func(c *gin.Context) {})

	// 应挂到 /api/v1/webdav 子组，通配符不吞噬整层
	SetupUnifiedRouter(api.Group("/webdav"), davHandler, authenticate, 0)

	// 触发路由树最终化；若注册到 /api/v1 层，此处会 panic
	found := false
	for _, route := range r.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/webdav") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected webdav routes under /api/v1/webdav")
	}
}
