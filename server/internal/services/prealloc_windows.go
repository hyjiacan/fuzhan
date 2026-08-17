//go:build windows
// +build windows

package services

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func preAllocate(f *os.File, size int64) error {
	if err := f.Truncate(size); err != nil {
		return err
	}

	type fileAllocationInfo struct {
		AllocationSize int64
	}

	info := fileAllocationInfo{AllocationSize: size}
	return windows.SetFileInformationByHandle(
		windows.Handle(f.Fd()),
		windows.FileAllocationInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
}
