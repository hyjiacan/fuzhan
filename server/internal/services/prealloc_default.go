//go:build !linux && !windows
// +build !linux,!windows

package services

import "os"

func preAllocate(f *os.File, size int64) error {
	return f.Truncate(size)
}
