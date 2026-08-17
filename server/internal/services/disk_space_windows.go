//go:build windows
// +build windows

package services

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func getDiskSpace(path string) (total, free int64, err error) {
	if path == "" {
		return 0, 0, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, 0, err
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(absPath),
		&freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		parent := filepath.Dir(absPath)
		if parent != absPath {
			return getDiskSpace(parent)
		}
		return 0, 0, err
	}

	return int64(totalNumberOfBytes), int64(freeBytesAvailable), nil
}
