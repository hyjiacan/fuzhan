//go:build !windows

package resource

import "syscall"

// hiddenWindowAttr 非 Windows 平台无需隐藏窗口，返回 nil 使用默认进程属性。
func hiddenWindowAttr() *syscall.SysProcAttr {
	return nil
}
