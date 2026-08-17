package webdav

import (
    "net/http"

    "golang.org/x/net/webdav"

    "fuzhan/internal/utils"
)

// NewHandler creates a new WebDAV handler with the given filesystem.
// prefix is the URL path prefix to strip from resource paths.
func NewHandler(prefix string, fs webdav.FileSystem) *webdav.Handler {
    return &webdav.Handler{
        Prefix:     prefix,
        FileSystem: fs,
        LockSystem: webdav.NewMemLS(),
        Logger: func(r *http.Request, err error) {
            if err != nil {
                utils.Debug("WebDAV operation error",
                    utils.String("method", r.Method),
                    utils.String("path", r.URL.Path),
                    utils.Err(err))
            }
        },
    }
}
