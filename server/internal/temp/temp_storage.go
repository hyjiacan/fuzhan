package temp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"fuzhan/internal/models"

	"github.com/zeebo/xxh3"
)

// Storage 临时上传的落盘路径策略：
// 目标为 `{path}/{Dir}/{码哈希}`，上传中临时文件为同目录 `. {码哈希}.uploading`。
// 与公开/私有一致，分片流程只关心这两个路径落在哪，业务差异在策略内收敛。
type Storage struct {
	basePath string
}

// NewStorage 创建临时上传的落盘路径策略。
func NewStorage(basePath string) *Storage {
	return &Storage{basePath: basePath}
}

// Paths 实现临时上传的路径构建与越界校验。
func (s *Storage) Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
	basePath := s.basePath
	if basePath == "" {
		err = fmt.Errorf("临时存储未配置")
		return
	}
	if session.Code == "" {
		err = fmt.Errorf("缺少访问码")
		return
	}

	cleanDir := session.TargetPath
	if cleanDir == "" || cleanDir == "/" {
		cleanDir = ""
	} else {
		cleanDir = strings.TrimSpace(cleanDir)
		cleanDir = strings.Trim(cleanDir, `/\`)
		cleanDir = filepath.Clean(cleanDir)
		if cleanDir == "." || filepath.IsAbs(cleanDir) || cleanDir == ".." ||
			strings.HasPrefix(cleanDir, ".."+string(filepath.Separator)) {
			err = fmt.Errorf("无效的目标路径")
			return
		}
	}

	dir := filepath.Join(basePath, cleanDir)
	if !isPathWithinRoot(basePath, dir) {
		err = fmt.Errorf("路径越界")
		return
	}

	hash := xxh3.Hash([]byte(session.Code + "fuzhan-secret"))
	safeFilename := fmt.Sprintf("%016x", hash)
	targetPath = filepath.Join(dir, safeFilename)
	uploadingPath = filepath.Join(dir, "."+safeFilename+".uploading")
	return
}

// generateTempCode 生成128bit访问码（32位十六进制，防在线枚举）
func generateTempCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}
