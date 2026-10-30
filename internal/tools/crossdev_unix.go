//go:build !windows

package tools

import (
	"errors"
	"syscall"
)

// isCrossDevice reports whether err is a cross-device rename error (EXDEV).
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
