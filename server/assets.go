package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed web/index.html all:web/assets fuzhan.sample.yaml scalar/scalar.html all:scalar/assets
var webAssets embed.FS

// assetsFS 提供对嵌入的前端资源的访问
var assetsFS, _ = fs.Sub(webAssets, "web/assets")

// scalarAssetsFS 提供对嵌入的 scalar（OpenAPI 文档页）独立构建资源的访问
var scalarAssetsFS, _ = fs.Sub(webAssets, "scalar/assets")

// extractWebAssets 将嵌入的 web 资源提取到目标目录
func extractWebAssets(assets embed.FS, targetDir string, roots ...string) error {
	for _, root := range roots {
		if err := fs.WalkDir(assets, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			targetPath := filepath.Join(targetDir, path)

			if d.IsDir() {
				return os.MkdirAll(targetPath, 0755)
			}

			content, err := fs.ReadFile(assets, path)
			if err != nil {
				return fmt.Errorf("读取文件 %s 失败: %w", path, err)
			}

			if err := os.WriteFile(targetPath, content, 0644); err != nil {
				return fmt.Errorf("写入文件 %s 失败: %w", targetPath, err)
			}

			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
