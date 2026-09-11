//go:build darwin || linux || freebsd || openbsd || netbsd

package sqlite

import (
	"fmt"
	"os"
	"syscall"
)

func filesystemFreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func allocatedFileBytes(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("allocated size is unavailable")
	}
	return stat.Blocks * 512, nil
}
