package utils

import (
	"path/filepath"
	"strings"
	"sync"
)

// 预览配置缓存
var (
	previewMu            sync.RWMutex
	previewMimes         map[string]bool
	previewExtensions    map[string]bool
	previewMimesWildcard []string
	previewExtWildcard   []string
	previewInitOnce      sync.Once
	previewAllowMimes    string
	previewAllowExts     string
	previewUnrestricted  bool
)

// InitPreviewConfig 初始化预览配置
func InitPreviewConfig(allowMimes, allowExts string) {
	previewMu.Lock()
	defer previewMu.Unlock()
	previewAllowMimes = allowMimes
	previewAllowExts = allowExts
	previewMimes = make(map[string]bool)
	previewExtensions = make(map[string]bool)
	previewMimesWildcard = []string{}
	previewExtWildcard = []string{}

	for _, m := range strings.Split(allowMimes, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if strings.HasSuffix(m, "/*") {
			previewMimesWildcard = append(previewMimesWildcard, m[:len(m)-1])
		} else {
			previewMimes[strings.ToLower(m)] = true
		}
	}

	for _, e := range strings.Split(allowExts, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		e = strings.TrimPrefix(e, ".")
		if strings.HasSuffix(e, "*") {
			previewExtWildcard = append(previewExtWildcard, e[:len(e)-1])
		} else {
			previewExtensions[strings.ToLower(e)] = true
		}
	}

	// 未配置任何可预览的 MIME/扩展名时表示不限制（所有文件均可预览）
	previewUnrestricted = len(previewMimes) == 0 && len(previewExtensions) == 0 &&
		len(previewMimesWildcard) == 0 && len(previewExtWildcard) == 0
}

// ReloadPreviewConfig 重新加载预览配置（配置更新后调用）
func ReloadPreviewConfig() {
	previewInitOnce = sync.Once{}
	InitPreviewConfig(previewAllowMimes, previewAllowExts)
}

// IsMimePreviewable 检查 MIME 类型是否可预览
func IsMimePreviewable(mimeType string) bool {
	previewMu.RLock()
	defer previewMu.RUnlock()
	if mimeType == "" {
		return false
	}
	mimeType = strings.ToLower(mimeType)

	if previewMimes[mimeType] {
		return true
	}

	for _, prefix := range previewMimesWildcard {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

// IsPreviewable 检查文件是否可预览
func IsPreviewable(filePath string, mimeType string) bool {
	previewMu.RLock()
	defer previewMu.RUnlock()
	// 未配置任何预览类型时表示不限制（所有文件均可预览）
	if previewUnrestricted {
		return true
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))

	if mimeType != "" && IsMimePreviewable(mimeType) {
		return true
	}

	if previewExtensions[ext] {
		return true
	}

	for _, prefix := range previewExtWildcard {
		if strings.HasPrefix(ext, prefix) {
			return true
		}
	}

	return false
}

// IsTextMime 检查 MIME 类型是否为文本类型
func IsTextMime(mimeType string) bool {
	if mimeType == "" {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "text/") {
		return false
	}
	previewMu.RLock()
	defer previewMu.RUnlock()
	mimeType = strings.ToLower(mimeType)
	if previewMimes[mimeType] {
		return true
	}
	for _, prefix := range previewMimesWildcard {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

// IsTextExtension 检查扩展名是否为文本类型
func IsTextExtension(ext string) bool {
	previewMu.RLock()
	defer previewMu.RUnlock()
	ext = strings.ToLower(ext)
	return previewExtensions[ext]
}

// IsImageExtension 检查扩展名是否为图片类型
func IsImageExtension(ext string) bool {
	ext = strings.ToLower(ext)
	imageExts := map[string]bool{
		"png": true, "jpg": true, "jpeg": true, "gif": true,
		"bmp": true, "webp": true, "svg": true, "ico": true,
	}
	return imageExts[ext]
}

// GetMimeType 获取常见扩展名的 MIME 类型
func GetMimeType(ext string) string {
	mimeMap := map[string]string{
		"txt":  "text/plain",
		"html": "text/html",
		"htm":  "text/html",
		"css":  "text/css",
		"js":   "application/javascript",
		"json": "application/json",
		"xml":  "application/xml",
		"md":   "text/markdown",
		"yml":  "text/yaml",
		"yaml": "text/yaml",
		"csv":  "text/csv",
		"go":   "text/plain",
		"java": "text/plain",
		"py":   "text/plain",
		"c":    "text/plain",
		"cpp":  "text/plain",
		"h":    "text/plain",
		"hpp":  "text/plain",
		"cs":   "text/plain",
		"rb":   "text/plain",
		"php":  "text/plain",
		"rs":   "text/plain",
		"sh":   "text/plain",
		"bat":  "text/plain",
		"ps1":  "text/plain",
		"sql":  "text/plain",
		"vue":  "text/plain",
		"tsx":  "text/plain",
		"jsx":  "text/plain",
		"ts":   "text/plain",
		"scss": "text/plain",
		"less": "text/plain",
		"ini":  "text/plain",
		"conf": "text/plain",
		"cfg":  "text/plain",
		"log":  "text/plain",
		"png":  "image/png",
		"jpg":  "image/jpeg",
		"jpeg": "image/jpeg",
		"gif":  "image/gif",
		"webp": "image/webp",
		"svg":  "image/svg+xml",
		"bmp":  "image/bmp",
		"ico":  "image/x-icon",
		"pdf":  "application/pdf",
	}
	if mime, ok := mimeMap[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}
