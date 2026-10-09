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
				// WebDAV 操作异常记入 webdav.log（Warn 级）：既能在渠道日志中跟踪，
				// 又避免把客户端常见的 404/405 之类刷进 error.log。
				utils.WebDAVWarn("WebDAV 操作异常",
					utils.String("method", r.Method),
					utils.String("path", r.URL.Path),
					utils.Err(err))
			}
		},
	}
}
