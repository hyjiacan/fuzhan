package cli

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// searchInDirectory 在目录中搜索文件，通过通道实时返回结果
// 支持使用逗号分隔多个关键词，最后一个关键词可使用 .xxx 指定扩展名
// hashMap 为可选的文件路径到哈希值的映射，用于在搜索结果中附加哈希信息
func searchInDirectory(ctx context.Context, rootDir, query, rootName string, resultChan chan<- FileInfo, hashMap map[string]string) {
	defer close(resultChan)
	query = strings.TrimSpace(query)
	query, _ = url.QueryUnescape(query)

	// 用逗号分割多个关键词
	keywords := strings.Split(query, ",")
	var ext string
	var hasExtension bool

	// 检查最后一个关键词是否包含扩展名
	lastIdx := len(keywords) - 1
	lastKw := strings.TrimSpace(keywords[lastIdx])
	if strings.Contains(lastKw, ".") {
		parts := strings.SplitN(lastKw, ".", 2)
		ext = strings.ToLower(parts[1])
		hasExtension = true
		keywords[lastIdx] = parts[0]
	}

	// 清理关键词，移除空字符串并转为小写
	var cleanKeywords []string
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" {
			cleanKeywords = append(cleanKeywords, kw)
		}
	}

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err != nil {
				return err
			}
			// 指定了扩展名时跳过目录
			if hasExtension && info.IsDir() {
				return nil
			}

			filename := info.Name()
			fileNameLower := strings.ToLower(filename)

			// 匹配所有关键词（AND 逻辑）
			for _, kw := range cleanKeywords {
				if !strings.Contains(fileNameLower, kw) {
					return nil
				}
			}

			// 匹配扩展名
			if hasExtension {
				fileExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
				if fileExt != ext {
					return nil
				}
				if !isFileAllowed(filename) {
					return nil
				}
			}

			// 所有条件匹配成功，发送结果
			item := getFileInfo(path, rootName)
			if hashMap != nil {
				if h, ok := hashMap[item.Path]; ok {
					item.Xxh3Hash = h
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case resultChan <- item:
			}
			return nil
		}
	})
	if err != nil && err != context.Canceled {
		utils.Error("搜索文件时出错", utils.String("rootDir", rootDir), utils.Err(err))
	}
}

// isFileAllowed 检查文件类型是否允许
func isFileAllowed(filename string) bool {
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

// getFileInfo 获取文件信息（不含根目录大小计算，由索引表提供）
func getFileInfo(itemPath, rootName string) FileInfo {
	info, err := os.Stat(itemPath)
	if err != nil {
		return FileInfo{
			Name:         filepath.Base(itemPath),
			Type:         "unknown",
			Path:         "",
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

	return FileInfo{
		Name:         filepath.Base(itemPath),
		Type:         itemType,
		Path:         relativePath,
		ModifiedTime: modifiedTime,
		Size:         fileSize,
	}
}

// GetDirectoryItems 获取目录下的文件信息（不含根目录大小计算）
// hashMap 为可选的文件路径到哈希值的映射，用于在结果中附加哈希信息
func GetDirectoryItems(dirPath, rootName string, hashMap map[string]string) []FileInfo {
	items := []FileInfo{}
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
		itemInfo := getFileInfo(itemPath, rootName)
		if hashMap != nil {
			if h, ok := hashMap[itemInfo.Path]; ok {
				itemInfo.Xxh3Hash = h
			}
		}
		if itemInfo.Type == "file" && !isFileAllowed(itemInfo.Name) {
			continue
		}
		items = append(items, itemInfo)
	}
	return items
}
