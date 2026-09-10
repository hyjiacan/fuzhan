package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
)

// UploadStorage 分片上传的落盘布局策略。
// 传输流程（会话/分片/续传/完成）与业务无关，唯一一处存储差异是：
// 目标文件放在哪、上传中的临时文件叫什么。公开/私有/临时三种存储通过各自实现注入。
type UploadStorage interface {
	// ResolvePaths 构建并验证目标文件路径和上传中文件路径（不创建目录）。
	// 需对文件名、目录做合法性/越界校验。
	ResolvePaths(session *models.UploadSession) (targetPath string, uploadingPath string, err error)
}

// EnsureUploadTargetDir 确保目标文件的父目录存在，不存在则创建。
// MkdirAll 是对任意解析结果都成立的通用文件系统操作，与具体存储类型无关，
// 因此作为流程内的通用辅助函数，而非存储策略的一部分。
func EnsureUploadTargetDir(targetPath string) error {
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	return nil
}

// PublicStorage 公开上传的落盘布局：目标文件落在共享根目录下的目标路径，
// 上传中临时文件为同目录下的 `.{basename}.uploading`。
type PublicStorage struct{}

// ResolvePaths 实现公开上传的路径构建与校验（源自原 UploadSessionService.resolvePaths）。
func (PublicStorage) ResolvePaths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
	rootPath := appconfig.RootNames[session.TargetRoot]
	if rootPath == "" {
		err = fmt.Errorf("无效的目标根目录")
		return
	}

	cleanDir := session.TargetPath
	if cleanDir == "" || cleanDir == "/" {
		cleanDir = ""
	} else {
		cleanDir = filepath.Clean(cleanDir)
	}
	if filepath.IsAbs(cleanDir) || cleanDir == ".." || (len(cleanDir) > 256 && cleanDir != "") {
		err = fmt.Errorf("无效的目标路径")
		return
	}

	targetDir := filepath.Join(rootPath, cleanDir)
	absTargetDir, absErr := filepath.Abs(targetDir)
	if absErr != nil {
		err = fmt.Errorf("路径解析失败")
		return
	}
	absRootPath, absErr := filepath.Abs(rootPath)
	if absErr != nil {
		err = fmt.Errorf("根路径解析失败")
		return
	}
	if absTargetDir != absRootPath && !strings.HasPrefix(absTargetDir, absRootPath+string(os.PathSeparator)) {
		err = fmt.Errorf("路径越界")
		return
	}

	cleanFilename := filepath.Clean(session.FileName)
	if strings.Contains(cleanFilename, "..") || strings.Contains(cleanFilename, "/") || strings.Contains(cleanFilename, "\\") {
		err = fmt.Errorf("无效的文件名")
		return
	}

	targetPath = filepath.Join(targetDir, cleanFilename)
	uploadingPath = filepath.Join(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".uploading")

	absTarget, _ := filepath.Abs(targetPath)
	if !strings.HasPrefix(absTarget, absRootPath+string(os.PathSeparator)) {
		err = fmt.Errorf("路径越界")
		return
	}

	return
}
