// Package pathutils 提供文件路径安全校验和解析工具
package pathutils

import (
	"net/url"
	"path/filepath"
	"strings"
)

// URLDecode 解码URL
func URLDecode(encoded string) (string, error) {
	return url.QueryUnescape(encoded)
}

// GetFileType 获取文件类型（扩展名，不含点号）
func GetFileType(filename string) string {
	ext := filepath.Ext(filename)
	if len(ext) > 0 {
		return ext[1:]
	}
	return ""
}

// IsFileAllowed 检查文件类型是否允许
func IsFileAllowed(allowedExtensions []string, filename string) bool {
	if len(allowedExtensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	ext = ext[1:]
	for _, allowed := range allowedExtensions {
		if allowed == ext {
			return true
		}
	}
	return false
}
