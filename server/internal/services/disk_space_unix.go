//go:build linux || darwin
// +build linux darwin

package services

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

func getDiskSpace(path string) (total, free int64, err error) {
	if path == "" {
		return 0, 0, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, 0, err
	}

	var stat unix.Statfs_t
	err = unix.Statfs(absPath, &stat)
	if err != nil {
		parent := filepath.Dir(absPath)
		if parent != absPath {
			return getDiskSpace(parent)
		}
		return 0, 0, err
	}

	total = int64(stat.Blocks) * int64(stat.Bsize)
	free = int64(stat.Bfree) * int64(stat.Bsize)
	return total, free, nil
}
