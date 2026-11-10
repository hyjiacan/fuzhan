package main

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRouteConflictCandidates 复刻 main.go 中几个"疑似同层混用静态段与 :参数"
// 的路由集合，触发路由树构建，确认是否 panic。
func TestRouteConflictCandidates(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	t.Run("openapi_files 动作段通配", func(t *testing.T) {
		r := gin.New()
		v1 := r.Group("/api/v1/open")
		f := v1.Group("/files")
		f.GET("/download/*path", dummy)
		f.GET("/info/*path", dummy)
		f.GET("/list/*path", dummy)
		f.GET("/search", dummy)
		f.GET("/duplicates", dummy)
		f.GET("/upload/:id", dummy)
		f.POST("/exists", dummy)
		f.POST("/rename", dummy)
		f.DELETE("", dummy)
		assertRoutes(t, r, "/api/v1/open/files/download/*path", "/api/v1/open/files/info/*path",
			"/api/v1/open/files/list/*path", "/api/v1/open/files/search",
			"/api/v1/open/files/exists", "/api/v1/open/files/rename", "/api/v1/open/files")
	})

	t.Run("temp :code参数+upload静态", func(t *testing.T) {
		r := gin.New()
		v1 := r.Group("/api/v1")
		web := v1.Group("/web")
		temp := web.Group("/temp")
		temp.GET("/:code", dummy)
		temp.GET("/:code/download", dummy)
		temp.DELETE("/:code", dummy)
		up := web.Group("/temp/uploads")
		up.GET("/sessions/:id", dummy)
		assertRoutes(t, r, "/api/v1/web/temp/:code", "/api/v1/web/temp/:code/download", "/api/v1/web/temp/uploads/sessions/:id")
	})

	t.Run("files 静态多段子树通配符", func(t *testing.T) {
		r := gin.New()
		v1 := r.Group("/api/v1")
		web := v1.Group("/web")
		f := web.Group("/files")
		f.GET("/preview/*path", dummy)
		f.GET("/preview-chunk/*path", dummy)
		f.PUT("/records/:id/notes", dummy)
		f.DELETE("/dependencies/:id", dummy)
		web.GET("/files/depends/:id", dummy)
		assertRoutes(t, r, "/api/v1/web/files/preview/*path", "/api/v1/web/files/dependencies/:id")
	})
}

func assertRoutes(t *testing.T, r *gin.Engine, paths ...string) {
	t.Helper()
	// 触发路由树最终化与冲突检测
	registered := map[string]bool{}
	for _, rt := range r.Routes() {
		registered[rt.Path] = true
	}
	for _, p := range paths {
		if !registered[p] {
			t.Fatalf("route %q not registered", p)
		}
	}
}
