package tools

import (
	"errors"
	"syscall"
)

// errorNotSameDevice は Windows の ERROR_NOT_SAME_DEVICE です。
const errorNotSameDevice = syscall.Errno(17)

// isCrossDevice は rename がドライブをまたぐために失敗したかを返します。
func isCrossDevice(err error) bool { return errors.Is(err, errorNotSameDevice) }
