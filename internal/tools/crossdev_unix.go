//go:build !windows

package tools

import (
	"errors"
	"syscall"
)

// isCrossDevice reports whether rename failed because it crosses file systems.
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
