//go:build !linux && !windows

package proc

import "syscall"

// SetParentDeathSignal does nothing on this OS, which has no parent death signal.
// host-shell runs here only as the local bridge, which starts no commands for the agent.
func SetParentDeathSignal(*syscall.SysProcAttr) {}

// KillChildrenOnExit does nothing on this OS.
func KillChildrenOnExit() error { return nil }
