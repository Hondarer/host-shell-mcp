package tools

import (
	"errors"
	"syscall"
)

// errorNotSameDevice is Windows ERROR_NOT_SAME_DEVICE.
const errorNotSameDevice = syscall.Errno(17)

// isCrossDevice reports whether err is a cross-drive rename error.
func isCrossDevice(err error) bool { return errors.Is(err, errorNotSameDevice) }
