package filedir

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// IsFileAllowed 检查文件类型是否允许
func IsFileAllowed(filename string) bool {
	if len(appconfig.GlobalConfig.Storage.AllowedExtensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || len(ext) <= 1 {
		return false
	}
	ext = ext[1:] // 移除开头的点
	for _, allowedExt := range appconfig.GlobalConfig.Storage.AllowedExtensions {
		if allowedExt == ext {
			return true
		}
	}
	return false
}

// GetFileInfo 获取文件信息（不含根目录大小计算，由索引表提供）。
// 返回统一的 appconfig.FileInfo 文件信息模型。
func GetFileInfo(itemPath, rootName string) appconfig.FileInfo {
	info, err := os.Stat(itemPath)
	if err != nil {
		return appconfig.FileInfo{
			Name:         filepath.Base(itemPath),
			Type:         "unknown",
			ModifiedTime: "",
			Size:         0,
		}
	}
	itemType := "file"
	if info.IsDir() {
		itemType = "directory"
	}
	relPath, _ := filepath.Rel(appconfig.RootNames[rootName], itemPath)
	relativePath := filepath.Join(rootName, relPath)
	if relativePath != "" {
		relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	}
	modifiedTime := info.ModTime().Format(time.RFC3339)
	var fileSize int64
	if itemType == "file" {
		fileSize = info.Size()
	}

	return appconfig.FileInfo{
		Name:         filepath.Base(itemPath),
		Type:         itemType,
		Path:         relativePath,
		ModifiedTime: modifiedTime,
		Size:         fileSize,
	}
}

// GetDirectoryItems 获取目录下的文件信息（不含根目录大小计算）
// hashMap 为可选的文件路径到哈希值的映射，用于在结果中附加哈希信息
func GetDirectoryItems(dirPath, rootName string, hashMap map[string]string) []appconfig.FileInfo {
	items := []appconfig.FileInfo{}
	files, err := os.ReadDir(dirPath)
	if err != nil {
		utils.Error("获取目录内容时出错", utils.String("path", dirPath), utils.Err(err))
		return items
	}
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			utils.Error("获取文件信息失败", utils.Err(err))
			continue
		}
		if !info.IsDir() && strings.HasPrefix(file.Name(), ".") && strings.HasSuffix(file.Name(), ".uploading") {
			continue
		}
		itemPath := filepath.Join(dirPath, file.Name())
		itemInfo := GetFileInfo(itemPath, rootName)
		if hashMap != nil {
			if h, ok := hashMap[itemInfo.Path]; ok {
				itemInfo.Xxh3Hash = h
			}
		}
		if itemInfo.Type == "file" && !IsFileAllowed(itemInfo.Name) {
			continue
		}
		items = append(items, itemInfo)
	}
	return items
}
