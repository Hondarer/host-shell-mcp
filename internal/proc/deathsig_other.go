//go:build !linux && !windows

package proc

import "syscall"

// SetParentDeathSignal is a no-op on platforms without a parent-death signal
// (where host-shell runs only as the local bridge and spawns no agent commands).
func SetParentDeathSignal(*syscall.SysProcAttr) {}

// KillChildrenOnExit is a no-op on this platform.
func KillChildrenOnExit() error { return nil }
