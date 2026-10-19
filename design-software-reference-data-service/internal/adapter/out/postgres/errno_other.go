//go:build !windows

package postgres

import "syscall"

func classifyPlatformConnectionErrno(syscall.Errno) (category, errnoClass string, ok bool) {
	return "", "UNKNOWN", false
}
