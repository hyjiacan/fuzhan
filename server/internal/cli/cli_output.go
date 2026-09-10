package cli

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// outputItems 输出排序后的文件和目录信息
func outputItems(w http.ResponseWriter, baseUrl string, cliPrefix string, items []FileInfo) {
	var totalCount int
	for _, item := range items {
		totalCount++
		filePath := strings.ReplaceAll(item.Path, "\\", "/")
		filePath = strings.ReplaceAll(filePath, " ", "%20")
		var fullUrl string
		var icon string
		if item.Type == "directory" {
			// 目录路径追加到 /cli/list/ 后
			fullUrl = fmt.Sprintf("%s%s/list/%s", baseUrl, cliPrefix, filePath)
			icon = "[目录]"
		} else {
			// 文件使用 download 路径
			fullUrl = fmt.Sprintf("%s/download/%s", baseUrl, filePath)
			icon = "[文件]"
		}
		// 格式化修改时间
		modTime := formatModTime(item.ModifiedTime)
		hash := item.Xxh3Hash
		if hash == "" {
			hash = "-"
		}
		fmt.Fprintf(w, "%s %s %s %s\n", modTime, hash, icon, fullUrl)
	}
	// 发送完成消息
	dataMsg := fmt.Sprintf("\n目录列表获取完成，共找到 %d 个条目", totalCount)
	fmt.Fprintf(w, "%s\n", dataMsg)
}

// formatModTime 将修改时间格式化为 2006-01-02 15:04:05
func formatModTime(modifiedTime string) string {
	t, err := time.Parse(time.RFC3339, modifiedTime)
	if err != nil {
		return modifiedTime
	}
	return t.Format("2006-01-02 15:04:05")
}
