//go:build !linux && !windows

package proc

import "syscall"

// Alive reports whether the process exists.
func Alive(pid int) bool { return syscall.Kill(pid, 0) == nil }
