//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package sqlite

import "fmt"

func filesystemFreeBytes(string) (int64, error) {
	return 0, fmt.Errorf("filesystem availability is unsupported on this platform")
}

func allocatedFileBytes(string) (int64, error) {
	return 0, fmt.Errorf("allocated file size is unsupported on this platform")
}
