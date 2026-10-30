//go:build !windows

package tools

import (
	"errors"
	"syscall"
)

// isCrossDevice は rename がファイル システムをまたぐために失敗したかを返します。
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
