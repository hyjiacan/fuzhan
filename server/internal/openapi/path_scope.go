package openapi

import (
	"net/http"
	"strings"

	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// allowedPathEntries 返回当前调用者允许操作的公开目录路径条目；nil 表示不受路径限制。
// Web 会话与未配置允许路径的 API Key 均视为允许所有路径。
func allowedPathEntries(c *gin.Context) []string {
	v, ok := c.Get(allowedPathsContextKey)
	if !ok {
		return nil
	}
	switch paths := v.(type) {
	case string:
		return models.ParseAllowedPaths(paths)
	case []string:
		out := make([]string, 0, len(paths))
		for _, p := range paths {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

// pathWithinAllowed 判断路径是否等于或位于允许条目之下（复用模型层的匹配语义）。
func pathWithinAllowed(entries []string, cleanPath string) bool {
	if len(entries) == 0 {
		return true
	}
	return models.AllowsPath(strings.Join(entries, "\n"), cleanPath)
}

// pathRelatedToAllowed 判断路径是否与允许范围相关：命中允许路径，或是允许路径的上级目录。
// 列表接口需要支持上级目录（只列出允许范围内的条目）。
func pathRelatedToAllowed(entries []string, cleanPath string) bool {
	if len(entries) == 0 || pathWithinAllowed(entries, cleanPath) {
		return true
	}
	prefix := strings.TrimSuffix(cleanPath, "/") + "/"
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

// requirePathAllowed 校验调用者是否可操作该路径；不满足时写 403 并返回 false。
func requirePathAllowed(c *gin.Context, cleanPath string) bool {
	if pathWithinAllowed(allowedPathEntries(c), cleanPath) {
		return true
	}
	response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden,
		"API Key 无权操作该路径: "+cleanPath)
	return false
}

// applyAllowedPathFilter 把允许路径范围应用到文件索引查询（root_name + file_path 前缀）。
func applyAllowedPathFilter(db *gorm.DB, entries []string) *gorm.DB {
	if len(entries) == 0 {
		return db
	}
	conds := make([]string, 0, len(entries))
	args := make([]interface{}, 0, len(entries)*3)
	for _, entry := range entries {
		root, rel := splitRootPath(entry)
		if root == "" {
			continue
		}
		if rel == "" || rel == "/" {
			conds = append(conds, "root_name = ?")
			args = append(args, root)
			continue
		}
		conds = append(conds, "(root_name = ? AND (file_path = ? OR file_path LIKE ? ESCAPE '\\'))")
		args = append(args, root, rel, escapeLike(rel)+"/%")
	}
	if len(conds) == 0 {
		return db
	}
	return db.Where("("+strings.Join(conds, " OR ")+")", args...)
}

// duplicatePathScopes 把允许路径条目转换为重复文件查询的路径范围。
func duplicatePathScopes(entries []string) []index.PathScope {
	if len(entries) == 0 {
		return nil
	}
	scopes := make([]index.PathScope, 0, len(entries))
	for _, entry := range entries {
		root, rel := splitRootPath(entry)
		if root == "" {
			continue
		}
		scopes = append(scopes, index.PathScope{Root: root, Prefix: rel})
	}
	return scopes
}
