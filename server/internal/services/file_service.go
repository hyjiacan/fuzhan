package services

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/storage"
	"fuzhan/internal/utils"
)

// storageContext 内部使用的上下文
var fsStorageContext = context.Background()

// FileService 文件服务（提供文件/目录的 CRUD 操作）
type FileService struct {
	uploadRecords []appconfig.UploadRecord
	recordsMutex  sync.Mutex
	// publicStorages 公开多根目录的存储后端映射（rootName → Storage）
	publicStorages map[string]storage.Storage
}

// NewFileService 创建文件服务实例
func NewFileService(publicStorages map[string]storage.Storage) *FileService {
	return &FileService{publicStorages: publicStorages}
}

// storageFor 获取指定公开根目录对应的存储后端；未注册时回退物理根目录语义
func (fs *FileService) storageFor(rootName string) (storage.Storage, bool) {
	if fs.publicStorages != nil {
		if st, ok := fs.publicStorages[rootName]; ok {
			return st, true
		}
	}
	rootPath, exists := appconfig.RootNames[rootName]
	if !exists {
		return nil, false
	}
	return storage.NewLocalStorage(rootPath), true
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
	// 解析旧路径获取根目录与存储内部逻辑子路径
	rootName, relPath, err := fs.resolvePath(oldPath)
	if err != nil {
		return err
	}
	st, ok := fs.storageFor(rootName)
	if !ok {
		return fmt.Errorf("根目录不存在: %s", rootName)
	}

	// 检查文件是否存在（根级路径不能为空，若旧路径指向根目录则报错）
	if relPath == "" {
		return fmt.Errorf("文件不存在")
	}
	info, err := st.Stat(fsStorageContext, relPath)
	if err != nil {
		if storage.IsNotExist(err) {
			return fmt.Errorf("文件不存在")
		}
		utils.Error("无法访问文件", utils.String("path", oldPath), utils.Err(err))
		return fmt.Errorf("无法访问文件")
	}

	// 不能是目录
	if info.IsDir {
		return fmt.Errorf("不支持重命名目录")
	}

	// 验证新文件名
	if err := fs.validateFileName(newName); err != nil {
		return err
	}

	// 构造新逻辑路径（同父目录，仅替换文件名）
	newRel := newName
	if idx := strings.LastIndex(relPath, "/"); idx >= 0 {
		newRel = relPath[:idx+1] + newName
	}

	// 检查目标文件是否已存在
	if _, err := st.Stat(fsStorageContext, newRel); err == nil {
		return fmt.Errorf("目标文件名已存在")
	}

	// 执行重命名（对象存储后端内部为复制+删除）
	if err := st.Rename(fsStorageContext, relPath, newRel); err != nil {
		utils.Error("重命名失败", utils.String("old", relPath), utils.String("new", newRel), utils.Err(err))
		return fmt.Errorf("重命名失败")
	}

	utils.Info("文件重命名成功", utils.String("old", oldPath), utils.String("new", newRel))
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

	// 目标目录不允许上传时，禁止移动/复制文件进入该目录。取目标根目录名进行检查。
	newPathParts := strings.SplitN(newPath, "/", 2)
	destRootName := newPathParts[0]
	if destRootName != "" {
		if !appconfig.PublicDirUploadAllowed(destRootName) {
			return fmt.Errorf("目标目录不允许上传文件，无法移动/复制到该目录")
		}
	}

	// 解析旧路径与新路径（均校验根目录存在）
	oldRoot, oldRel, err := fs.resolvePath(oldPath)
	if err != nil {
		return err
	}
	newRoot, newRel, err := fs.resolvePath(newPath)
	if err != nil {
		return err
	}

	// 源与目标根目录一致，取源后端（目标与源同根）；否则分别取各自后端
	srcSt, ok := fs.storageFor(oldRoot)
	if !ok {
		return fmt.Errorf("根目录不存在: %s", oldRoot)
	}
	dstSt, ok := fs.storageFor(newRoot)
	if !ok {
		return fmt.Errorf("根目录不存在: %s", newRoot)
	}

	// 检查源文件是否存在
	if oldRel == "" {
		return fmt.Errorf("源文件不存在")
	}
	info, err := srcSt.Stat(fsStorageContext, oldRel)
	if err != nil {
		if storage.IsNotExist(err) {
			return fmt.Errorf("源文件不存在")
		}
		utils.Error("无法访问源文件", utils.String("path", oldPath), utils.Err(err))
		return fmt.Errorf("无法访问源文件")
	}

	// 不能是目录
	if info.IsDir {
		return fmt.Errorf("不支持移动目录")
	}

	// 不能移动到自身（同根且路径相同）
	if oldRoot == newRoot && oldRel == newRel {
		return fmt.Errorf("源文件和目标文件相同")
	}

	// 创建目标父目录（仅同根时可通过 Rename 语义直接完成；跨根需分别处理，
	// 目标父目录由最终后端在 Rename 前的复制流程中调用 MkdirAll 确保）
	parentDir := ""
	if idx := strings.LastIndex(newRel, "/"); idx >= 0 {
		parentDir = newRel[:idx]
	}
	if parentDir != "" {
		if dstSt != srcSt {
			if err := dstSt.MkdirAll(fsStorageContext, parentDir); err != nil {
				utils.Error("无法创建目标目录", utils.String("path", parentDir), utils.Err(err))
				return fmt.Errorf("无法创建目标目录")
			}
		}
	}

	// 检查目标文件是否已存在
	if _, err := dstSt.Stat(fsStorageContext, newRel); err == nil {
		return fmt.Errorf("目标文件已存在")
	}

	// 同根：直接重命名/移动；跨根：复制到目标后端后再删除源
	if oldRoot == newRoot {
		if err := dstSt.Rename(fsStorageContext, oldRel, newRel); err != nil {
			utils.Error("移动文件失败", utils.String("old", oldPath), utils.String("new", newPath), utils.Err(err))
			return fmt.Errorf("移动文件失败")
		}
	} else {
		if _, err := dstSt.Stat(fsStorageContext, parentDir); storage.IsNotExist(err) && parentDir != "" {
			if err := dstSt.MkdirAll(fsStorageContext, parentDir); err != nil {
				utils.Error("无法创建目标目录", utils.String("path", parentDir), utils.Err(err))
				return fmt.Errorf("无法创建目标目录")
			}
		}
		if err := fs.copyCrossRoot(srcSt, dstSt, oldRel, newRel); err != nil {
			utils.Error("移动文件失败（跨根复制）", utils.String("old", oldPath), utils.String("new", newPath), utils.Err(err))
			return fmt.Errorf("移动文件失败")
		}
	}

	utils.Info("文件移动成功", utils.String("old", oldPath), utils.String("new", newPath))
	return nil
}

// copyCrossRoot 跨存储后端复制文件（用于跨根目录移动）。
func (fs *FileService) copyCrossRoot(srcSt, dstSt storage.Storage, srcRel, dstRel string) error {
	src, err := srcSt.Open(fsStorageContext, srcRel)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := dstSt.OpenWriter(fsStorageContext, dstRel)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

// DeleteFile 删除文件或目录
func (fs *FileService) DeleteFile(filePath string) error {
	// 解析路径
	rootName, relPath, err := fs.resolvePath(filePath)
	if err != nil {
		return err
	}
	st, ok := fs.storageFor(rootName)
	if !ok {
		return fmt.Errorf("根目录不存在: %s", rootName)
	}

	// 根级路径不可删除整个根目录
	if relPath == "" {
		return fmt.Errorf("文件不存在")
	}

	// 检查目标是否存在
	info, err := st.Stat(fsStorageContext, relPath)
	if err != nil {
		if storage.IsNotExist(err) {
			return fmt.Errorf("文件不存在")
		}
		utils.Error("无法访问文件", utils.String("path", filePath), utils.Err(err))
		return fmt.Errorf("无法访问文件")
	}

	// 目录使用 RemoveAll 递归删除，文件使用 Remove
	if info.IsDir {
		if err := st.RemoveAll(fsStorageContext, relPath); err != nil {
			utils.Error("删除目录失败", utils.String("path", filePath), utils.Err(err))
			return fmt.Errorf("删除目录失败")
		}
		utils.Info("目录删除成功", utils.String("path", filePath))
	} else {
		if err := st.Remove(fsStorageContext, relPath); err != nil {
			utils.Error("删除文件失败", utils.String("path", filePath), utils.Err(err))
			return fmt.Errorf("删除文件失败")
		}
		utils.Info("文件删除成功", utils.String("path", filePath))
	}
	return nil
}

// resolvePath 将相对路径解析为存储内部逻辑子路径（rootName 之外的相对路径）。
// 返回的 relativ 是不含根目录前缀的逻辑路径（如 "子目录/xx.pdf"），供 Storage 后端使用；
// 当路径仅为根目录名时返回空字符串表示根级。
func (fs *FileService) resolvePath(relativePath string) (rootName, relativ string, err error) {
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	relativePath = strings.Trim(relativePath, "/")

	if relativePath == "" {
		return "", "", fmt.Errorf("路径不能为空")
	}

	// 防止路径遍历：禁止使用 ..
	if strings.Contains(relativePath, "..") {
		return "", "", fmt.Errorf("路径不能包含 ..")
	}

	parts := strings.SplitN(relativePath, "/", 2)
	rootName = parts[0]

	if _, exists := appconfig.RootNames[rootName]; !exists {
		return "", "", fmt.Errorf("根目录不存在: %s", rootName)
	}

	if len(parts) == 1 {
		return rootName, "", nil
	}
	return rootName, parts[1], nil
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
