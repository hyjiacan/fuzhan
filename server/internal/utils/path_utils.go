package utils

import (
	"path/filepath"
	"strings"
)

// ResolveUploadPath 解析上传路径，避免路径重复
func ResolveUploadPath(rootPath, dir, filename string) (string, string) {
	// 获取根目录名称
	rootName := filepath.Base(rootPath)

	// 如果dir以rootName开头，去掉重复部分
	var cleanDir string
	if strings.HasPrefix(dir, rootName+"/") {
		cleanDir = strings.TrimPrefix(dir, rootName+"/")
	} else if dir == rootName {
		cleanDir = ""
	} else {
		cleanDir = dir
	}

	// 构建最终路径
	var pathParts []string
	pathParts = append(pathParts, rootPath)
	if cleanDir != "" {
		pathParts = append(pathParts, cleanDir)
	}
	pathParts = append(pathParts, filename)

	finalPath := filepath.Join(pathParts...)

	// 构建子路径（用于日志等）
	var subPath string
	if cleanDir != "" {
		subPath = filepath.Join(cleanDir, filename)
	} else {
		subPath = filename
	}

	return finalPath, subPath
}
