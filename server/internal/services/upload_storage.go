package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
)

// UploadStorage 分片上传的落盘路径策略。
// 无论何种存储类型，分片上传流程都一致：分片写入 uploading 临时文件，
// Finalize 时转为最终的 target 正式文件。不同存储的唯一差异是这两个路径
// 落在哪——公开：共享根目录下的逻辑路径+真实文件名；私有：用户私有根目录下
// 的逻辑路径+真实文件名。分享码属于业务凭证，不参与路径构造。
type UploadStorage interface {
	// Paths 返回最终目标文件路径与上传中临时文件路径（含路径越界校验，不创建目录）。
	Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error)
}

// EnsureUploadDir 确保上传目录存在（uploading 临时文件所在的父目录）。
// MkdirAll(filepath.Dir(uploadingPath)) 对任意存储的上传落点都成立，
// 因此作为流程内的通用辅助函数，而非存储策略的一部分。
func EnsureUploadDir(uploadingPath string) error {
	if err := os.MkdirAll(filepath.Dir(uploadingPath), 0755); err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}
	return nil
}

// PublicStorage 公开上传的落盘路径策略。
// 目标文件落在共享根目录下的逻辑目录+真实文件名；上传中临时文件为同目录下的
// `.{basename}.uploading`，二者路径语义一致、与分享无关。
type PublicStorage struct{}

// Paths 实现公开上传的路径构建与校验（源自原 UploadSessionService.resolvePaths）。
func (PublicStorage) Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
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
