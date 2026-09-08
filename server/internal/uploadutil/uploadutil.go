// Package uploadutil 提供上传相关的共享工具函数
package uploadutil

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"fuzhan/internal/services"
	"github.com/zeebo/xxh3"
)

// WriteChunkToUploading 写入分片数据到上传文件，执行 xxh3 校验
func WriteChunkToUploading(
	uploadingPath string, fileSize int64,
	chunkIndex int, chunkSize int64, totalChunks int,
	chunkFileSize int64, srcFile io.Reader, checksum string,
) (int64, error) {
	chunkData, readErr := io.ReadAll(srcFile)
	if readErr != nil {
		return 0, fmt.Errorf("读取分片数据失败: %w", readErr)
	}
	written := int64(len(chunkData))
	offset := int64(chunkIndex) * chunkSize
	if checksum != "" && len(chunkData) > 0 {
		actualHash := xxh3.Hash(chunkData)
		expectedHash, parseErr := strconv.ParseUint(checksum, 16, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("无效的xxh3校验值: %s", checksum)
		}
		if expectedHash != actualHash {
			return 0, fmt.Errorf("xxh3校验失败: 期望值=%016X, 实际值=%016X", expectedHash, actualHash)
		}
	}
	var f *os.File
	var openErr error
	if offset == 0 {
		f, openErr = os.OpenFile(uploadingPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
		if openErr == nil {
			if preErr := services.PreAllocate(f, fileSize); preErr != nil {
				f.Close()
				os.Remove(uploadingPath)
				f = nil
				return 0, fmt.Errorf("预分配文件空间失败: %w", preErr)
			}
		} else if os.IsExist(openErr) {
			f, openErr = os.OpenFile(uploadingPath, os.O_RDWR, 0644)
			if openErr != nil {
				return 0, fmt.Errorf("打开已存在的上传文件失败: %w", openErr)
			}
		} else {
			return 0, fmt.Errorf("创建上传文件失败: %w", openErr)
		}
	} else {
		f, openErr = os.OpenFile(uploadingPath, os.O_RDWR, 0644)
		if openErr != nil {
			return 0, fmt.Errorf("打开上传文件失败: %w", openErr)
		}
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	if _, writeErr := f.WriteAt(chunkData, offset); writeErr != nil {
		services.ZeroOutChunk(f, offset, written)
		return 0, fmt.Errorf("写入分片数据失败: %w", writeErr)
	}
	return written, nil
}
