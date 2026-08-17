package services

import (
	"fmt"
	"os"

	"fuzhan/internal/utils"
)

// PreAllocate 预分配文件空间
func PreAllocate(f *os.File, size int64) error {
	return preAllocate(f, size)
}

// CheckDiskSpace 检查指定路径所在分区的磁盘剩余空间是否足够
func CheckDiskSpace(path string, required int64) error {
	_, free, err := GetDiskSpace(path)
	if err != nil {
		return fmt.Errorf("无法获取磁盘空间信息: %w", err)
	}
	if free < required {
		return fmt.Errorf("磁盘空间不足（需要: %d 字节，可用: %d 字节）", required, free)
	}
	return nil
}

// GetDiskSpace 获取指定路径所在分区的总空间和可用空间
func GetDiskSpace(path string) (total, free int64, err error) {
	return getDiskSpace(path)
}

// ZeroOutChunk 清空已写入的分片数据，用于错误回滚
func ZeroOutChunk(f *os.File, offset int64, size int64) {
	if size <= 0 {
		return
	}
	const bufSize = 32 * 1024
	zeroBuf := make([]byte, bufSize)
	for written := int64(0); written < size; {
		chunkSize := size - written
		if chunkSize > bufSize {
			chunkSize = bufSize
		}
		if _, err := f.WriteAt(zeroBuf[:chunkSize], offset+written); err != nil {
			utils.Warn("清理分片数据失败", utils.Int64("offset", offset), utils.Int64("size", size), utils.Err(err))
			return
		}
		written += chunkSize
	}
}
