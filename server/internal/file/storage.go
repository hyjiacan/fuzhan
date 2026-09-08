package file

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	configPkg "fuzhan/internal/appconfig"
)

// PrivateFileMeta 私有文件元数据
type PrivateFileMeta struct {
	Filename   string    `json:"filename"`
	UploadTime time.Time `json:"uploadTime"`
	FileSize   int64     `json:"fileSize"`
	Owner      string    `json:"owner"`
	Code       string    `json:"code"`
}

// GenerateShareCode 生成8位分享码
func GenerateShareCode() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), nil
}

// GetPrivateUserPath 获取用户私有存储路径
func GetPrivateUserPath(basePath, userID string) string {
	return filepath.Join(basePath, "users", userID)
}

// GetPrivateFileDir 获取用户文件目录
func GetPrivateFileDir(basePath, userID, code string) string {
	return filepath.Join(GetPrivateUserPath(basePath, userID), code)
}

// LoadPrivateFileMetadata 加载文件元数据
func LoadPrivateFileMetadata(fileDir string) (*PrivateFileMeta, error) {
	metaPath := filepath.Join(fileDir, "meta.json")
	file, err := os.Open(metaPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var meta PrivateFileMeta
	if err := json.NewDecoder(file).Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SavePrivateFileMetadata 保存文件元数据
func SavePrivateFileMetadata(fileDir string, meta *PrivateFileMeta) error {
	if err := os.MkdirAll(fileDir, 0755); err != nil {
		return err
	}

	metaPath := filepath.Join(fileDir, "meta.json")
	file, err := os.Create(metaPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(meta)
}

// FindPrivateFileByCode 根据分享码查找文件（支持嵌套目录）
func FindPrivateFileByCode(basePath, userID, code string) (string, *PrivateFileMeta, error) {
	userDir := GetPrivateUserPath(basePath, userID)
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("文件不存在")
	}

	var foundDir string
	var foundMeta *PrivateFileMeta

	err := filepath.Walk(userDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		// 查找 meta.json 文件所在目录
		if !info.IsDir() && strings.EqualFold(info.Name(), "meta.json") {
			fileDir := filepath.Dir(path)
			// 检查目录名是否匹配分享码
			if filepath.Base(fileDir) == code {
				meta, loadErr := LoadPrivateFileMetadata(fileDir)
				if loadErr != nil {
					return nil
				}
				foundDir = fileDir
				foundMeta = meta
				return filepath.SkipAll // 找到后停止遍历
			}
		}
		return nil
	})

	if foundDir == "" || foundMeta == nil {
		return "", nil, fmt.Errorf("文件不存在")
	}
	return foundDir, foundMeta, err
}

// ListPrivateDirectory 列出指定目录下的文件和子目录（仅一层，不递归）
func ListPrivateDirectory(basePath, userID, dirPath string) (files []PrivateFileMeta, subdirs []string, err error) {
	userDir := GetPrivateUserPath(basePath, userID)
	targetDir := userDir
	if dirPath != "" {
		targetDir = filepath.Join(userDir, dirPath)
		// 验证路径安全
		absTarget, _ := filepath.Abs(targetDir)
		absRoot, _ := filepath.Abs(userDir)
		if !strings.HasPrefix(absTarget, absRoot) {
			return nil, nil, fmt.Errorf("路径越权")
		}
	}
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return []PrivateFileMeta{}, nil, nil
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(targetDir, entry.Name())
		metaPath := filepath.Join(entryPath, "meta.json")

		if _, statErr := os.Stat(metaPath); statErr == nil {
			// 包含 meta.json → 文件条目
			meta, loadErr := LoadPrivateFileMetadata(entryPath)
			if loadErr == nil {
				files = append(files, *meta)
			}
		} else if os.IsNotExist(statErr) {
			// 不含 meta.json → 子目录条目
			subdirs = append(subdirs, entry.Name())
		}
	}
	return files, subdirs, nil
}

// GetUserUsedQuota 计算用户已使用的总空间（所有目录）
// 从 file_records_private 查询，索引未就绪则返回 0
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
