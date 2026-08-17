package pathpolicy

import (
    "os"
    "path/filepath"
    "strings"
)

// PathPolicy 判断操作是否允许
type PathPolicy interface {
    CanCreate(name string) bool
    CanModify(name string) bool
    CanDelete(name string) bool
}

// PublicPathPolicy public/ 下可创建不可修改删除
type PublicPathPolicy struct{}

func (PublicPathPolicy) CanCreate(name string) bool {
    return true
}

func (PublicPathPolicy) CanModify(name string) bool {
    return false
}

func (PublicPathPolicy) CanDelete(name string) bool {
    return false
}

// PrivatePathPolicy private/ 下完全控制
type PrivatePathPolicy struct{}

func (PrivatePathPolicy) CanCreate(name string) bool {
    return true
}

func (PrivatePathPolicy) CanModify(name string) bool {
    return true
}

func (PrivatePathPolicy) CanDelete(name string) bool {
    return true
}

// SanitizePath Layer 1 输入归一化：Clean + 拒绝对路径/空字节/..前缀
// 返回 cleaned path；如果路径不安全，返回空字符串
func SanitizePath(path string) string {
    // 拒绝空路径
    if path == "" {
        return ""
    }
    // 拒绝空字节注入
    if strings.ContainsRune(path, 0) {
        return ""
    }
    // 拒绝绝对路径
    if filepath.IsAbs(path) {
        return ""
    }
    cleaned := filepath.Clean(path)
    // 拒绝 .. 前缀（尝试跳出根目录）
    if strings.HasPrefix(cleaned, "..") || cleaned == ".." {
        return ""
    }
    return cleaned
}

// ValidatePath Layer 4 OS 层验证：解析 symlink 确认目标在 root 内
// 如果 path 不存在（如新建文件场景），检查父目录
func ValidatePath(path, root string) error {
    resolved, err := filepath.EvalSymlinks(path)
    if err != nil {
        if os.IsNotExist(err) {
            // 文件不存在则检查父目录
            parent := filepath.Dir(path)
            resolvedParent, err := filepath.EvalSymlinks(parent)
            if err != nil {
                return os.ErrPermission
            }
            if !strings.HasPrefix(resolvedParent, root) {
                return os.ErrPermission
            }
            return nil
        }
        return os.ErrPermission
    }
    if !strings.HasPrefix(resolved, root) {
        return os.ErrPermission
    }
    return nil
}
