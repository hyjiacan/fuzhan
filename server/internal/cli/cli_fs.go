package cli

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/filedir"
	"fuzhan/internal/utils"
)

// searchInDirectory 在目录中搜索文件，通过通道实时返回结果
// 支持使用逗号分隔多个关键词，最后一个关键词可使用 .xxx 指定扩展名
// hashMap 为可选的文件路径到哈希值的映射，用于在搜索结果中附加哈希信息
func searchInDirectory(ctx context.Context, rootDir, query, rootName string, resultChan chan<- appconfig.FileInfo, hashMap map[string]string) {
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
				if !filedir.IsFileAllowed(filename) {
					return nil
				}
			}

			// 所有条件匹配成功，发送结果
			item := filedir.GetFileInfo(path, rootName)
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
