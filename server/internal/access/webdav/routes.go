package webdav

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

// webdavMethods lists all HTTP methods that WebDAV handles, including
// standard methods and WebDAV extension methods.
var webdavMethods = []string{
    "GET", "HEAD", "PUT", "DELETE", "OPTIONS",
    "PROPFIND", "PROPPATCH", "MKCOL", "MOVE", "COPY",
    "LOCK", "UNLOCK",
}

// registerWebDAVRoutes registers the davHandler for all WebDAV methods
// on both /*path and root paths, with the given middlewares.
func registerWebDAVRoutes(rg *gin.RouterGroup, davHandler http.Handler, middlewares ...gin.HandlerFunc) {
    for _, method := range webdavMethods {
        rg.Handle(method, "/*path", append(middlewares, gin.WrapH(davHandler))...)
        rg.Handle(method, "", append(middlewares, gin.WrapH(davHandler))...)
    }
}

// SetupRouter registers WebDAV routes on the given Gin router group.
// The handler serves PROPFIND/GET/HEAD/PUT/MKCOL/MOVE/DELETE requests.
func SetupRouter(rg *gin.RouterGroup, davHandler http.Handler) {
    registerWebDAVRoutes(rg, davHandler, depthMiddleware())
}

// SetupUnifiedRouter registers the unified WebDAV endpoint on /api/v1/webdav
// with optional Basic Auth. Auth is optional — unauthenticated requests see
// only public/ directories.
// maxFileSize is the per-request body limit for write operations (0 = unlimited).
func SetupUnifiedRouter(rg *gin.RouterGroup, davHandler http.Handler, authenticate UserAuthenticator, maxFileSize int64) {
    middlewares := []gin.HandlerFunc{OptionalBasicAuthMiddleware(authenticate), depthMiddleware()}
    if maxFileSize > 0 {
        middlewares = append(middlewares, bodyLimitMiddleware(maxFileSize))
    }
    registerWebDAVRoutes(rg, davHandler, middlewares...)
}

// depthMiddleware restricts PROPFIND Depth header to at most 1.
func depthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method == "PROPFIND" {
            depth := c.Request.Header.Get("Depth")
            if depth == "infinity" || (depth != "" && depth != "0" && depth != "1") {
                c.Header("DAV", "1")
                c.Header("Depth", "1")
                c.AbortWithStatus(http.StatusForbidden) // 403 Forbidden
                return
            }
        }
        c.Next()
    }
}

// bodyLimitMiddleware limits request body size for write operations to prevent
// memory exhaustion from large uploads that bypass the web upload controls.
func bodyLimitMiddleware(maxSize int64) gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method == "PUT" || c.Request.Method == "PROPPATCH" {
            c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
        }
        c.Next()
    }
}

// isReadOnlyMethod checks if a WebDAV method is read-only.
func isReadOnlyMethod(method string) bool {
    switch method {
    case "GET", "HEAD", "PROPFIND", "OPTIONS":
        return true
    }
    return false
}

// readOnlyMiddleware blocks write methods (PUT/POST/MKCOL/DELETE/MOVE/COPY)
// and returns 403 Forbidden before reaching the WebDAV handler.
func readOnlyMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if !isReadOnlyMethod(c.Request.Method) {
            c.AbortWithStatus(http.StatusForbidden)
            return
        }
        c.Next()
    }
}

// SetupPublicRouter registers public read-only WebDAV routes on the given
// Gin router group. Write methods are blocked with 403 before reaching the handler.
func SetupPublicRouter(rg *gin.RouterGroup, davHandler http.Handler) {
    registerWebDAVRoutes(rg, davHandler, readOnlyMiddleware(), depthMiddleware())
}

// SetupPrivateRouter registers private WebDAV routes with Basic Auth.
// The authFunc is called to verify credentials against the user database.
func SetupPrivateRouter(rg *gin.RouterGroup, davHandler http.Handler, authFunc UserAuthenticator) {
    for _, method := range webdavMethods {
        rg.Handle(method, "/webdav/private/*path", BasicAuthMiddleware(authFunc), depthMiddleware(), gin.WrapH(davHandler))
        rg.Handle(method, "/webdav/private", BasicAuthMiddleware(authFunc), depthMiddleware(), gin.WrapH(davHandler))
    }
}
