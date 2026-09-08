package utils

import (
	"embed"
	"os"
	"path/filepath"
)

// GetAssetContent 根据相对路径获取资源内容
func GetAssetContent(relativePath string, workDir string, webAssets embed.FS) ([]byte, error) {
	localPath := filepath.Join(workDir, "web/"+relativePath)
	if _, err := os.Stat(localPath); err == nil {
		content, err := os.ReadFile(localPath)
		if err != nil {
			Error("从本地读取失败", String("path", localPath), Err(err))
		}
		return content, err
	} else if !os.IsNotExist(err) {
		Error("检查文件存在性时出错", String("path", localPath), Err(err))
	}

	content, err := webAssets.ReadFile("web/" + relativePath)
	if err != nil {
		Error("从嵌入资源中读取失败", String("path", relativePath), Err(err))
	}

	return content, err
}
