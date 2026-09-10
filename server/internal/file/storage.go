package file

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	configPkg "fuzhan/internal/appconfig"
	"fuzhan/internal/models"
)

// privateRootName 私有文件在索引表中的根名（固定值）。
const privateRootName = "private"

// GenerateShareCode 生成128bit分享码（32位十六进制，防在线枚举爆破）。
// 仅作为私有文件的分享/下载凭证，与存储布局无关。
func GenerateShareCode() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), nil
}

// GetPrivateUserPath 获取用户私有存储根路径
func GetPrivateUserPath(basePath, userID string) string {
	return filepath.Join(basePath, "users", userID)
}

// ListPrivateDirectory 列出指定目录下的一层私有文件（查 DB）与子目录（扫文件系统）。
// files 为 file_records_private 中的当前层文件记录（含分享码），subdirs 为该层子目录名。
func ListPrivateDirectory(basePath, userID, dir string) (files []models.FileRecordPrivate, subdirs []string, err error) {
	db := configPkg.GetDB()
	if db == nil {
		return nil, nil, fmt.Errorf("数据库未就绪")
	}

	userDir := GetPrivateUserPath(basePath, userID)
	targetDir := userDir
	if dir != "" {
		targetDir = filepath.Join(userDir, dir)
		absTarget, aerr := filepath.Abs(targetDir)
		absRoot, rerr := filepath.Abs(userDir)
		if aerr != nil || rerr != nil {
			return nil, nil, fmt.Errorf("路径解析失败")
		}
		if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
			return nil, nil, fmt.Errorf("路径越权")
		}
	}

	// 子目录来自文件系统一层
	if es, serr := os.ReadDir(targetDir); serr == nil {
		for _, e := range es {
			if e.IsDir() {
				subdirs = append(subdirs, e.Name())
			}
		}
	}

	// 文件条目来自 DB：过滤当前层。统一要求私有 file_path 以 / 开头（排除存量 shareCode 旧记录）。
	prefix := strings.Trim(dir, "/")
	like, notDeeper := layerLikePatterns(prefix)
	if err := db.Where("owner_id = ? AND status = ? AND is_dir = ?",
		userID, models.FileStatusActive, false).
		Where("file_path LIKE ?", "/%").
		Where("file_path LIKE ?", like).
		Where("file_path NOT LIKE ?", notDeeper).
		Find(&files).Error; err != nil {
		return nil, nil, err
	}
	return files, subdirs, nil
}

// layerLikePatterns 生成当前目录一层的 LIKE 参数（全部参数化绑定，防注入）。
// 根层：pattern "/%"，排除更深的 "/%/%"；子层：恰以 "/prefix/" 开头且不再含更深目录段。
func layerLikePatterns(prefix string) (like, notDeeper string) {
	if prefix == "" {
		return "/%", "/%/%"
	}
	return "/" + prefix + "/%", "/" + prefix + "/%/%"
}

// GetUserUsedQuota 计算用户已使用的总空间（从 file_records_private 查询，索引未就绪则返回 0）
func GetUserUsedQuota(_ string, userID string) int64 {
	db := configPkg.GetDB()
	if db == nil {
		return 0
	}

	var total int64
	if err := db.Table("file_records_private").
		Where("owner_id = ? AND status = ? AND is_dir = ?",
			userID, "active", false).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&total).Error; err != nil {
		return 0
	}
	return total
}
