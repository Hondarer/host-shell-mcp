package proc

import "syscall"

// SetParentDeathSignal configures attr to send SIGKILL to the direct child when host-shell exits
// (including on SIGKILL); reparented grandchildren may survive.
//
// Linux binds Pdeathsig to the creating OS thread rather than the process. Because host-shell
// never calls runtime.LockOSThread, the Go runtime keeps its OS threads alive until process exit.
func SetParentDeathSignal(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}

// KillChildrenOnExit is a no-op on Linux; SetParentDeathSignal configures each child instead.
func KillChildrenOnExit() error { return nil }
