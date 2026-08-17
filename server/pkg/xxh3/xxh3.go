// Package xxh3 提供 xxh3 64位哈希计算
//
// 使用策略:
//   - 小文件 (≤64MB): 一次性读取内存计算
//   - 大文件 (>64MB): 64KB 分块流式计算, 内存占用 ≤1MB
package xxh3

import (
    "bufio"
    "fmt"
    "io"
    "os"

    "github.com/zeebo/xxh3"
)

const (
    // SmallFileThreshold 小文件阈值, ≤此值一次性读取
    SmallFileThreshold = 64 * 1024 * 1024 // 64MB

    // ReadChunkSize 流式读取分块大小
    ReadChunkSize = 64 * 1024 // 64KB
)

// ComputeFileHash 计算文件的 xxh3 64位哈希值
// 根据文件大小自动选择策略:
//   - ≤64MB: 一次性读取全量内存计算
//   - >64MB: 分块流式计算
//
// 返回 16 进制小写字符串 (如 "a1b2c3d4e5f6g7h8")
func ComputeFileHash(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil {
        return "", fmt.Errorf("打开文件失败: %w", err)
    }
    defer f.Close()

    fi, err := f.Stat()
    if err != nil {
        return "", fmt.Errorf("获取文件信息失败: %w", err)
    }

    // 小文件: 一次性读取
    if fi.Size() <= SmallFileThreshold {
        data, err := io.ReadAll(f)
        if err != nil {
            return "", fmt.Errorf("读取文件失败: %w", err)
        }
        return HashBytes(data), nil
    }

    // 大文件: 流式计算
    return HashReader(f)
}

// HashBytes 计算字节数据的 xxh3 64位哈希值
func HashBytes(data []byte) string {
    hash := xxh3.Hash(data)
    return fmt.Sprintf("%016x", hash)
}

// HashReader 从 io.Reader 流式计算 xxh3 64位哈希值
// 以 64KB 分块读取, 适合大文件或网络流
func HashReader(r io.Reader) (string, error) {
    hasher := xxh3.New()
    buf := make([]byte, ReadChunkSize)
    reader := bufio.NewReaderSize(r, ReadChunkSize)

    for {
        n, err := reader.Read(buf)
        if n > 0 {
            if _, werr := hasher.Write(buf[:n]); werr != nil {
                return "", fmt.Errorf("哈希计算写入失败: %w", werr)
            }
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            return "", fmt.Errorf("读取数据失败: %w", err)
        }
    }

    hash := hasher.Sum64()
    return fmt.Sprintf("%016x", hash), nil
}

// HashString 计算字符串的 xxh3 64位哈希值
func HashString(s string) string {
    hash := xxh3.HashString(s)
    return fmt.Sprintf("%016x", hash)
}
