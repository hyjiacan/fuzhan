package main

import (
    "testing"

    "github.com/gin-gonic/gin"
)

// TestRouteConflictCandidates 复刻 main.go 中几个"疑似同层混用静态段与 :参数"
// 的路由集合，触发路由树构建，确认是否 panic。
func TestRouteConflictCandidates(t *testing.T) {
    gin.SetMode(gin.ReleaseMode)

    t.Run("openapi_files 静态download+参数:id", func(t *testing.T) {
        r := gin.New()
        v1 := r.Group("/api/open/v1")
        f := v1.Group("/files")
        f.GET("/download/*path", dummy)
        f.GET("/:id/depends", dummy)
        assertRoutes(t, r, "/api/open/v1/files/download/*path", "/api/open/v1/files/:id/depends")
    })

    t.Run("temp :code参数+upload静态", func(t *testing.T) {
        r := gin.New()
        v1 := r.Group("/api/v1")
        temp := v1.Group("/temp")
        temp.GET("/:code", dummy)
        temp.GET("/:code/download", dummy)
        temp.DELETE("/:code", dummy)
        up := v1.Group("/temp/upload")
        up.GET("/session/:uploadId", dummy)
        assertRoutes(t, r, "/api/v1/temp/:code", "/api/v1/temp/:code/download", "/api/v1/temp/upload/session/:uploadId")
    })

    t.Run("files 静态多段子树通配符", func(t *testing.T) {
        r := gin.New()
        v1 := r.Group("/api/v1")
        f := v1.Group("/files")
        f.GET("/preview/*path", dummy)
        f.GET("/preview-chunk/*path", dummy)
        f.PUT("/records/:id/notes", dummy)
        f.DELETE("/dependencies/:id", dummy)
        v1.GET("/files/depends/:id", dummy)
        assertRoutes(t, r, "/api/v1/files/preview/*path", "/api/v1/files/dependencies/:id")
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