package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// FileService 文件服务（提供文件/目录的 CRUD 操作）
type FileService struct {
	uploadRecords []appconfig.UploadRecord
	recordsMutex  sync.Mutex
}

// NewFileService 创建文件服务实例
func NewFileService() *FileService {
	return &FileService{}
}

// AddUploadRecord 添加上传记录
func (fs *FileService) AddUploadRecord(ipAddress string, filename string, filePath string) {
	fs.recordsMutex.Lock()
	defer fs.recordsMutex.Unlock()

	record := appconfig.UploadRecord{
		IPAddress:  ipAddress,
		UploadTime: utils.Now(),
		Filename:   filename,
		FilePath:   filePath,
	}

	// 将新记录添加到开头
	fs.uploadRecords = append([]appconfig.UploadRecord{record}, fs.uploadRecords...)

	// 只保留最近的100条记录
	if len(fs.uploadRecords) > 100 {
		fs.uploadRecords = fs.uploadRecords[:100]
	}
}

// GetRecentUploads 获取最近的上传记录
func (fs *FileService) GetRecentUploads(count int) []appconfig.UploadRecord {
	fs.recordsMutex.Lock()
	defer fs.recordsMutex.Unlock()

	if count <= 0 || count > len(fs.uploadRecords) {
		count = len(fs.uploadRecords)
	}

	// 返回副本以避免竞态条件
	result := make([]appconfig.UploadRecord, count)
	copy(result, fs.uploadRecords[:count])
	return result
}

// RenameFile 重命名文件
func (fs *FileService) RenameFile(oldPath, newName string) error {
	// 解析旧路径获取真实文件系统路径
	realPath, err := fs.resolvePath(oldPath)
	if err != nil {
		return err
	}

	// 检查文件是否存在
	info, err := os.Stat(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("文件不存在")
		}
		utils.Error("无法访问文件", utils.String("path", realPath), utils.Err(err))
		return fmt.Errorf("无法访问文件")
	}

	// 不能是目录
	if info.IsDir() {
		return fmt.Errorf("不支持重命名目录")
	}

	// 验证新文件名
	if err := fs.validateFileName(newName); err != nil {
		return err
	}

	// 获取父目录
	parentDir := filepath.Dir(realPath)
	newPath := filepath.Join(parentDir, newName)

	// 检查目标文件是否已存在
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("目标文件名已存在")
	}

	// 执行重命名
	if err := os.Rename(realPath, newPath); err != nil {
		utils.Error("重命名失败", utils.String("oldPath", realPath), utils.String("newPath", newPath), utils.Err(err))
		return fmt.Errorf("重命名失败")
	}

	utils.Info("文件重命名成功", utils.String("oldPath", realPath), utils.String("newPath", newPath))
	return nil
}

// MoveFile 移动或重命名文件
// 目标路径必须包含根目录名，如 "documents/newname.pdf" 或 "documents/subdir/newname.pdf"
func (fs *FileService) MoveFile(oldPath, newPath string) error {
	// 预处理：去除首尾空白
	newPath = strings.TrimSpace(newPath)

	if newPath == "" {
		return fmt.Errorf("目标路径不能为空")
	}

	// 路径不能以 / 结尾（必须指定文件名）
	if strings.HasSuffix(newPath, "/") {
		return fmt.Errorf("目标路径必须包含文件名，不能以 / 结尾")
	}

	// 检查路径是否包含 .. 防止越权
	if strings.Contains(newPath, "..") {
		return fmt.Errorf("目标路径无效，不能包含 ..")
	}

	// 验证目标路径必须以根目录名开头
	newPathParts := strings.SplitN(newPath, "/", 2)
	if len(newPathParts) < 1 || newPathParts[0] == "" {
		return fmt.Errorf("目标路径必须以根目录名开头")
	}
	rootName := newPathParts[0]
	if _, exists := appconfig.RootNames[rootName]; !exists {
		return fmt.Errorf("目标路径必须以根目录名开头，如 documents/、photos/ 等")
	}

	// 解析旧路径
	realOldPath, err := fs.resolvePath(oldPath)
	if err != nil {
		return err
	}

	// 解析新路径
	realNewPath, err := fs.resolvePath(newPath)
	if err != nil {
		return err
	}

	// 检查源文件是否存在
	info, err := os.Stat(realOldPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("源文件不存在")
		}
		utils.Error("无法访问源文件", utils.String("path", realOldPath), utils.Err(err))
		return fmt.Errorf("无法访问源文件")
	}

	// 不能是目录
	if info.IsDir() {
		return fmt.Errorf("不支持移动目录")
	}

	// 不能移动到自身
	if realOldPath == realNewPath {
		return fmt.Errorf("源文件和目标文件相同")
	}

	// 创建父目录（如果有子目录）
	parentDir := filepath.Dir(realNewPath)
	if parentDir != "." {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			utils.Error("无法创建目标目录", utils.String("path", parentDir), utils.Err(err))
			return fmt.Errorf("无法创建目标目录")
		}
	}

	// 检查目标文件是否已存在
	if _, err := os.Stat(realNewPath); err == nil {
		return fmt.Errorf("目标文件已存在")
	}

	// 执行移动
	if err := os.Rename(realOldPath, realNewPath); err != nil {
		utils.Error("移动文件失败", utils.String("oldPath", realOldPath), utils.String("newPath", realNewPath), utils.Err(err))
		return fmt.Errorf("移动文件失败")
	}

	utils.Info("文件移动成功", utils.String("oldPath", realOldPath), utils.String("newPath", realNewPath))
	return nil
}

// DeleteFile 删除文件
func (fs *FileService) DeleteFile(filePath string) error {
	// 解析路径
	realPath, err := fs.resolvePath(filePath)
	if err != nil {
		return err
	}

	// 检查文件是否存在
	info, err := os.Stat(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("文件不存在")
		}
		utils.Error("无法访问文件", utils.String("path", realPath), utils.Err(err))
		return fmt.Errorf("无法访问文件")
	}

	// 目录使用 RemoveAll 递归删除，文件使用 Remove
	if info.IsDir() {
		if err := os.RemoveAll(realPath); err != nil {
			utils.Error("删除目录失败", utils.String("path", realPath), utils.Err(err))
			return fmt.Errorf("删除目录失败")
		}
		utils.Info("目录删除成功", utils.String("path", realPath))
	} else {
		if err := os.Remove(realPath); err != nil {
			utils.Error("删除文件失败", utils.String("path", realPath), utils.Err(err))
			return fmt.Errorf("删除文件失败")
		}
		utils.Info("文件删除成功", utils.String("path", realPath))
	}
	return nil
}

// resolvePath 将相对路径解析为真实文件系统路径
func (fs *FileService) resolvePath(relativePath string) (string, error) {
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	relativePath = strings.Trim(relativePath, "/")

	if relativePath == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	// 防止路径遍历：禁止使用 ..
	if strings.Contains(relativePath, "..") {
		return "", fmt.Errorf("路径不能包含 ..")
	}

	parts := strings.SplitN(relativePath, "/", 2)
	rootName := parts[0]

	rootDir, exists := appconfig.RootNames[rootName]
	if !exists {
		return "", fmt.Errorf("根目录不存在: %s", rootName)
	}

	if len(parts) == 1 {
		return rootDir, nil
	}

	resolvedPath := filepath.Join(rootDir, parts[1])
	// 额外校验：确保解析后的路径仍以根目录开头
	absResolved, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("路径解析失败")
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("根目录解析失败")
	}
	if !strings.HasPrefix(absResolved, absRoot) {
		return "", fmt.Errorf("路径越界")
	}

	return resolvedPath, nil
}

// validateFileName 验证文件名合法性
func (fs *FileService) validateFileName(name string) error {
	if name == "" {
		return fmt.Errorf("文件名不能为空")
	}

	// 检查非法字符
	invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalidChars {
		if strings.Contains(name, char) {
			return fmt.Errorf("文件名包含非法字符: %s", char)
		}
	}

	// 检查保留名称
	if name == "." || name == ".." {
		return fmt.Errorf("文件名不能为保留名称")
	}

	return nil
}
