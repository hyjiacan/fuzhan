package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeebo/xxh3"
)

// PathSecurityError 路径安全错误
type PathSecurityError struct {
	TargetPath string
	RootPath   string
	Message    string
}

func (e *PathSecurityError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("路径越权: %s 不在允许范围 %s 内", e.TargetPath, e.RootPath)
}

// PathValidator 路径验证器，提供更便捷的路径安全检查
type PathValidator struct {
	rootName string
	rootPath string
	absRoot  string
}

// NewPathValidator 创建路径验证器
// rootName: 根目录名称
// rootPath: 根目录实际路径
func NewPathValidator(rootName, rootPath string) *PathValidator {
	absRoot, _ := filepath.Abs(rootPath)
	return &PathValidator{
		rootName: rootName,
		rootPath: rootPath,
		absRoot:  absRoot,
	}
}

// NewPathValidatorWithRoots 从 rootNames map 创建路径验证器
func NewPathValidatorWithRoots(rootName string, rootNames map[string]string) *PathValidator {
	if rootPath, ok := rootNames[rootName]; ok {
		return NewPathValidator(rootName, rootPath)
	}
	return nil
}

// RootName 返回根目录名称
func (v *PathValidator) RootName() string {
	return v.rootName
}

// RootPath 返回根目录路径
func (v *PathValidator) RootPath() string {
	return v.rootPath
}

// Validate 验证路径是否在范围内（推荐方式）
// 返回 nil 表示安全，返回 error 表示越权
func (v *PathValidator) Validate(targetPath string) error {
	if v == nil || v.absRoot == "" {
		return &PathSecurityError{
			TargetPath: targetPath,
			RootPath:   "",
			Message:    "路径验证器未正确初始化",
		}
	}

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return &PathSecurityError{
			TargetPath: targetPath,
			RootPath:   v.absRoot,
			Message:    "无法解析路径",
		}
	}

	rel, err := filepath.Rel(v.absRoot, absTarget)
	if err != nil || strings.HasPrefix(rel, "..") {
		Warn("检测到路径越权尝试", String("target_path", targetPath), String("root_path", v.absRoot))
		return &PathSecurityError{
			TargetPath: targetPath,
			RootPath:   v.absRoot,
		}
	}

	return nil
}

// MustValidate 验证路径，失败时 panic
// 适用于初始化阶段等确定不会越权的场景
func (v *PathValidator) MustValidate(targetPath string) {
	if err := v.Validate(targetPath); err != nil {
		panic(err)
	}
}

// ValidateAndRespond 验证路径，越权时自动发送错误响应并返回 false
// 返回 true 表示安全，false 表示越权（已发送响应）
func (v *PathValidator) ValidateAndRespond(targetPath string, w http.ResponseWriter) bool {
	if err := v.Validate(targetPath); err != nil {
		EncodeResponse(w, nil, "路径越权访问", http.StatusForbidden)
		return false
	}
	return true
}

// ValidateSubPath 验证子路径（相对于根目录）
// subPath 应为不含根目录前缀的相对路径
func (v *PathValidator) ValidateSubPath(subPath string) error {
	if v == nil || v.absRoot == "" {
		return errors.New("路径验证器未正确初始化")
	}

	fullPath := filepath.Join(v.rootPath, subPath)
	return v.Validate(fullPath)
}

// ValidateSubPathAndRespond 验证子路径，越权时自动发送错误响应
func (v *PathValidator) ValidateSubPathAndRespond(subPath string, w http.ResponseWriter) bool {
	if err := v.ValidateSubPath(subPath); err != nil {
		EncodeResponse(w, nil, "路径越权访问", http.StatusForbidden)
		return false
	}
	return true
}

// Join 构建安全的完整路径
// 如果路径越权，会返回根目录路径
func (v *PathValidator) Join(parts ...string) string {
	fullPath := filepath.Join(v.rootPath, filepath.Join(parts...))
	if err := v.Validate(fullPath); err != nil {
		return v.rootPath
	}
	return fullPath
}

// IsPathOutOfScope 兼容旧接口，内部使用 PathValidator
func IsPathOutOfScope(targetPath, rootName string, rootNames map[string]string, w http.ResponseWriter) bool {
	validator := NewPathValidatorWithRoots(rootName, rootNames)
	if validator == nil {
		Warn("根目录不存在", String("root_name", rootName))
		EncodeResponse(w, nil, "指定的目录不存在", http.StatusBadRequest)
		return true
	}
	if err := validator.Validate(targetPath); err != nil {
		EncodeResponse(w, nil, "路径越权访问", http.StatusForbidden)
		return true
	}
	return false
}

// CreateDirectory 创建目录
func CreateDirectory(path string, w http.ResponseWriter) bool {
	if err := os.MkdirAll(path, 0755); err != nil {
		Error("创建目录失败", String("path", path), Err(err))
		return false
	}
	return true
}

// CalculateDirectorySize 递归计算目录大小
func CalculateDirectorySize(path string) (int64, error) {
	// 检查目录是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// 目录不存在，返回0大小而不报错
		return 0, nil
	}

	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// CalculateTempStorageSize 计算临时文件存储总大小
func CalculateTempStorageSize(tempPath string) (int64, error) {
	filesPath := filepath.Join(tempPath, "files")
	return CalculateDirectorySize(filesPath)
}

// CalculateUserTempStorageSize 计算特定用户的临时文件存储大小
func CalculateUserTempStorageSize(tempPath, userID string) (int64, error) {
	userPath := filepath.Join(tempPath, "users", userID)
	return CalculateDirectorySize(userPath)
}

// CopyFile 复制文件
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// GenerateUUID 生成UUID
func GenerateUUID() (string, error) {
	// 生成16字节的随机数据
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		return "", err
	}

	// 设置版本号和变体
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant is 10

	return hex.EncodeToString(uuid), nil
}

// GenerateAccessCode 生成访问码
func GenerateAccessCode() (string, error) {
	// 生成16字节的随机数据
	randomBytes := make([]byte, 16)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}

	// 转换为32位十六进制字符串并取前8位
	hexString := hex.EncodeToString(randomBytes)
	return strings.ToUpper(hexString[:8]), nil
}

// IsValidUserID 验证用户ID是否有效
func IsValidUserID(userID string) bool {
	// UUID应该是32个十六进制字符
	if len(userID) != 32 {
		return false
	}

	// 检查是否都是十六进制字符
	for _, c := range userID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}

	return true
}

// IsValidAccessCode 验证访问码是否有效
func IsValidAccessCode(code string) bool {
	// 访问码应该是8个十六进制字符
	if len(code) != 8 {
		return false
	}

	// 检查是否都是十六进制字符
	for _, c := range code {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'F') {
			return false
		}
	}

	return true
}

// GetFileReferencePath 获取文件引用存储路径
func GetFileReferencePath(configTempPath string) string {
	return filepath.Join(configTempPath, "references")
}

// GetPersistentFilesPath 获取持久文件存储根路径
func GetPersistentFilesPath(configTempPath string) string {
	return filepath.Join(configTempPath, "files")
}

// GetFileStoragePath 获取文件实际存储路径
func GetFileStoragePath(configTempPath string, fileHash string) string {
	return filepath.Join(GetPersistentFilesPath(configTempPath), fileHash)
}

// GetUserPath 获取用户目录路径
func GetUserPath(configTempPath string, userID string) string {
	return filepath.Join(configTempPath, "users", userID)
}

// GetUserFileReferencePath 获取用户文件引用路径
func GetUserFileReferencePath(configTempPath string, userID, accessCode string) string {
	return filepath.Join(GetUserPath(configTempPath, userID), accessCode)
}

// GenerateStableFileIdentifier 生成稳定的文件标识（基于用户ID和文件名）
func GenerateStableFileIdentifier(userID, filename string) string {
	combined := userID + "|#|" + filename
	hash := xxh3.HashString(combined)
	return fmt.Sprintf("%016x", hash)
}
