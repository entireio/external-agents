//go:build !windows

package vibe

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid names a running process. A process we may
// not signal still exists.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
