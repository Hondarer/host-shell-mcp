package tools

import (
	"errors"
	"syscall"
)

// errorNotSameDevice is Windows' ERROR_NOT_SAME_DEVICE.
const errorNotSameDevice = syscall.Errno(17)

// isCrossDevice reports whether rename failed because it crosses drives.
func isCrossDevice(err error) bool { return errors.Is(err, errorNotSameDevice) }
