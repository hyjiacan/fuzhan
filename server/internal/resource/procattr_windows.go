//go:build windows

package resource

import "syscall"

// hiddenWindowAttr 在 Windows 上隐藏子进程控制台窗口，避免探测网卡速率时闪出黑框。
func hiddenWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
