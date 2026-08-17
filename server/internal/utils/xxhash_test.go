package utils

import (
    "fmt"
    "testing"

    "github.com/zeebo/xxh3"
)

const defaultSeed uint64 = 0

// xxh3Hash 计算字节切片的 xxh3 哈希（使用默认 seed）
func xxh3Hash(data []byte) uint64 {
    return xxh3.Hash(data)
}

// TestXXH3HashConsistency 测试 xxh3 哈希一致性
// 前后端必须使用相同的编码方式：UTF-8 字节数组
func TestXXH3HashConsistency(t *testing.T) {
    testCases := []struct {
        name  string
        input string
    }{
        {"empty string", ""},
        {"hello", "hello"},
        {"test data", "test data"},
        {"large file content", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."},
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Go: 直接对 UTF-8 字符串字节计算哈希
            data := []byte(tc.input)
            hash := xxh3Hash(data)
            result := fmt.Sprintf("%016x", hash)
            t.Logf("Input: %q -> Hash: %s (hex: %x)", tc.input, result, hash)
        })
    }
}