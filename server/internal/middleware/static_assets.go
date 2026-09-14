package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 静态资源子路径（SPA 前端与 scalar 文档页），命中时启用压缩与长缓存。
var staticAssetPrefixes = []string{"/assets/", "/scalar/assets/"}

// 可压缩的文本类资源。图片、字体等本身已压缩或为二进制，跳过以免二次减压徒增 CPU。
var compressExts = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".svg": true,
	".json": true, ".map": true, ".html": true, ".txt": true, ".xml": true,
}

// gzipWriter 包装 gin 的 ResponseWriter，将文本类静态资源以 gzip 写入。
// 实现 http.Flusher，保证 http.ServeContent 的文件流式输出可正常 flush。
type gzipWriter struct {
	gin.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
}

func (w *gzipWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(w.Status())
	}
	return w.gz.Write(b)
}

func (w *gzipWriter) Flush() {
	w.gz.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipWriter) Close() {
	if w.gz != nil {
		w.gz.Close()
	}
}

// StaticAssetsMiddleware 为带内容哈希的文本类静态资源提供：
//  1. 长缓存（Cache-Control: public, max-age=31536000, immutable）
//  2. gzip 压缩（按 Accept-Encoding 协商）
//
// 仅命中 /assets 与 /scalar/assets 前缀，不侵入 API/下载等其它路由。
func StaticAssetsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if !hasStaticPrefix(p) {
			c.Next()
			return
		}

		// 带哈希的资源/app 类文本资源均可长缓存；非哈希资源退化为无缓存验证
		if hasCacheableExt(p) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Header("Cache-Control", "no-cache")
		}

		// 仅当客户端支持 gzip 且目标为可压缩类型时才启用压缩
		if shouldCompress(p) && acceptsGzip(c) {
			gz := gzip.NewWriter(c.Writer)
			writer := &gzipWriter{ResponseWriter: c.Writer, gz: gz}
			defer writer.Close()
			c.Writer = writer
		}
		c.Next()
	}
}

func hasStaticPrefix(p string) bool {
	for _, prefix := range staticAssetPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func hasCacheableExt(p string) bool {
	ext := cacheBaseExt(p)
	// 长缓存仅针对构建产物名（main-[hash].js 之类）或稳定的文本/字体资源
	if ext == "" {
		return false
	}
	return compressExts[ext] || ext == ".woff" || ext == ".woff2" || ext == ".ttf" || ext == ".eot"
}

func shouldCompress(p string) bool {
	return compressExts[cacheBaseExt(p)]
}

func cacheBaseExt(p string) string {
	p = strings.SplitN(p, "?", 2)[0]
	idx := strings.LastIndexByte(p, '.')
	if idx < 0 {
		return ""
	}
	ext := strings.ToLower(p[idx:])
	// 带路径分离符的后续部分不应当作扩展名
	if strings.ContainsAny(p[idx:], "/") {
		return ""
	}
	return ext
}

func acceptsGzip(c *gin.Context) bool {
	ae := c.Request.Header.Get("Accept-Encoding")
	return strings.Contains(ae, "gzip")
}
