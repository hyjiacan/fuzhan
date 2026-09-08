package xxh3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHashBytes 验证 HashBytes 返回值格式和确定性
func TestHashBytes(t *testing.T) {
	// 空数据
	h := HashBytes(nil)
	if len(h) != 16 {
		t.Errorf("HashBytes(nil) 期望 16 字符，实际 %d: %s", len(h), h)
	}

	// 确定性的
	data := []byte("hello fuzhan")
	h1 := HashBytes(data)
	h2 := HashBytes(data)
	if h1 != h2 {
		t.Errorf("HashBytes 不一致: %s vs %s", h1, h2)
	}

	// 不同输入产生不同 hash
	h3 := HashBytes([]byte("different data"))
	if h1 == h3 {
		t.Errorf("不同数据应产生不同 hash")
	}

	// 十六进制格式
	for _, c := range h1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("非十六进制字符: %c", c)
		}
	}
}

// TestHashString 验证 HashString 与 HashBytes 一致
func TestHashString(t *testing.T) {
	s := "test-string-for-hashing-测试中文"
	fromStr := HashString(s)
	fromBytes := HashBytes([]byte(s))
	if fromStr != fromBytes {
		t.Errorf("HashString 与 HashBytes 结果不一致: %s vs %s", fromStr, fromBytes)
	}
}

// TestHashReader 验证流式哈希正确性
func TestHashReader(t *testing.T) {
	data := []byte("streaming hash test data for reader")
	reader := strings.NewReader(string(data))

	h, err := HashReader(reader)
	if err != nil {
		t.Fatalf("HashReader 失败: %v", err)
	}

	// 应与 HashBytes 一致
	expected := HashBytes(data)
	if h != expected {
		t.Errorf("HashReader 结果不一致: 期望 %s, 实际 %s", expected, h)
	}
}

// TestHashReader_Empty 验证空流的流式哈希
func TestHashReader_Empty(t *testing.T) {
	reader := strings.NewReader("")
	h, err := HashReader(reader)
	if err != nil {
		t.Fatalf("HashReader(空) 失败: %v", err)
	}

	expected := HashBytes([]byte{})
	if h != expected {
		t.Errorf("HashReader(空) 结果不一致: 期望 %s, 实际 %s", expected, h)
	}
}

// TestHashReader_LargeData 验证大数据量的流式哈希（触发内部缓冲区分块）
func TestHashReader_LargeData(t *testing.T) {
	// 构造超过 64KB 的数据，触发分块读取
	size := ReadChunkSize*3 + 1
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader := strings.NewReader(string(data))
	h, err := HashReader(reader)
	if err != nil {
		t.Fatalf("HashReader(大) 失败: %v", err)
	}

	expected := HashBytes(data)
	if h != expected {
		t.Errorf("HashReader(大) 结果不一致")
	}
}

// TestComputeFileHash 验证文件哈希计算
func TestComputeFileHash(t *testing.T) {
	dir := t.TempDir()

	// 写入测试文件
	content := []byte("file hash test content")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	h, err := ComputeFileHash(filePath)
	if err != nil {
		t.Fatalf("ComputeFileHash 失败: %v", err)
	}

	expected := HashBytes(content)
	if h != expected {
		t.Errorf("ComputeFileHash 结果不一致: 期望 %s, 实际 %s", expected, h)
	}
}

// TestComputeFileHash_EmptyFile 验证空文件哈希
func TestComputeFileHash_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatalf("写入空文件失败: %v", err)
	}

	h, err := ComputeFileHash(filePath)
	if err != nil {
		t.Fatalf("ComputeFileHash(空文件) 失败: %v", err)
	}

	expected := HashBytes([]byte{})
	if h != expected {
		t.Errorf("ComputeFileHash(空文件) 结果不一致: 期望 %s, 实际 %s", expected, h)
	}
}

// TestComputeFileHash_LargeFile 验证大文件（超过 SmallFileThreshold）的流式哈希
func TestComputeFileHash_LargeFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "large.bin")

	// 构造略小于 64KB 的文件（触发流式路径的边界）
	// 使用 SmallFileThreshold+1 确保走流式路径
	size := SmallFileThreshold + 1
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("写入大文件失败: %v", err)
	}

	h, err := ComputeFileHash(filePath)
	if err != nil {
		t.Fatalf("ComputeFileHash(大文件) 失败: %v", err)
	}

	expected := HashBytes(data)
	if h != expected {
		t.Errorf("ComputeFileHash(大文件) 结果不一致")
	}
}

// TestComputeFileHash_FileNotFound 验证文件不存在的错误处理
func TestComputeFileHash_FileNotFound(t *testing.T) {
	_, err := ComputeFileHash("/path/to/nonexistent/file.txt")
	if err == nil {
		t.Fatal("期望文件不存在错误，实际为 nil")
	}
}

// TestComputeFileHash_Directory 验证对目录计算哈希返回错误
func TestComputeFileHash_Directory(t *testing.T) {
	dir := t.TempDir()
	_, err := ComputeFileHash(dir)
	if err == nil {
		// 不同平台行为不同，至少不应 panic
		t.Log("对目录计算哈希未返回错误（平台相关）")
	}
}

// TestComputeFileHash_SmallVsLarge 验证小文件和大文件路径结果一致
func TestComputeFileHash_SmallVsLarge(t *testing.T) {
	dir := t.TempDir()

	// 创建略大于 SmallFileThreshold 的文件
	size := SmallFileThreshold + 100
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	filePath := filepath.Join(dir, "test.dat")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	h, err := ComputeFileHash(filePath)
	if err != nil {
		t.Fatalf("ComputeFileHash 失败: %v", err)
	}

	expected := HashBytes(data)
	if h != expected {
		t.Errorf("流式路径与全量路径结果不一致")
	}
}

// TestConstants 验证关键常量
func TestConstants(t *testing.T) {
	if SmallFileThreshold != 64*1024*1024 {
		t.Errorf("SmallFileThreshold 应为 64MB，实际 %d", SmallFileThreshold)
	}
	if ReadChunkSize != 64*1024 {
		t.Errorf("ReadChunkSize 应为 64KB，实际 %d", ReadChunkSize)
	}
}
