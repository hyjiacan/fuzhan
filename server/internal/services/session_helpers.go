package services

import (
	"fmt"
	"os"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// classifyUploadFileError 将打开上传文件时的系统错误归类为可读提示
func classifyUploadFileError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "virus") || strings.Contains(l, "potentially unwanted") ||
		strings.Contains(l, "malware") || strings.Contains(l, "threat"):
		return "文件被安全软件拦截（疑似病毒或潜在有害软件），请将该目录加入安全软件信任/排除列表后重试"
	case strings.Contains(l, "being used by another process") || strings.Contains(l, "sharing violation") ||
		strings.Contains(l, "0x80070020"):
		return "文件正被其他进程占用，请关闭相关程序后重试"
	case strings.Contains(l, "access is denied") || strings.Contains(l, "0x80070005") ||
		strings.Contains(l, "permission denied"):
		return "无权限写入该目录，请检查目录权限"
	case strings.Contains(l, "disk full") || strings.Contains(l, "no space") || strings.Contains(l, "0x80070070"):
		return "磁盘空间不足，请清理后重试"
	default:
		return msg
	}
}

// ensureUploadingFile 创建上传文件并预分配空间
func (s *UploadSessionService) ensureUploadingFile(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		utils.Warn("创建上传文件失败", utils.String("path", path), utils.Err(err))
		return fmt.Errorf("无法创建上传文件")
	}
	defer file.Close()

	if err := preAllocate(file, size); err != nil {
		os.Remove(path)
		return err
	}

	return nil
}

// zeroOutChunk 清空已写入的分片数据，用于错误回滚
func (s *UploadSessionService) zeroOutChunk(f *os.File, offset int64, size int64) {
	ZeroOutChunk(f, offset, size)
}

// checkDiskSpace 检查磁盘剩余空间是否足够
func (s *UploadSessionService) checkDiskSpace(required int64, rootName string) error {
	checkPath := ""
	if rootName != "" {
		if rootPath, ok := appconfig.RootNames[rootName]; ok {
			checkPath = rootPath
		}
	}
	if checkPath == "" {
		// 无根目录时跳过磁盘空间检查
		return nil
	}
	return CheckDiskSpace(checkPath, required)
}
