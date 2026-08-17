//go:build linux
// +build linux

package services

import (
	"os"
	"syscall"
)

func preAllocate(f *os.File, size int64) error {
	return syscall.Fallocate(int(f.Fd()), 0, 0, size)
}
