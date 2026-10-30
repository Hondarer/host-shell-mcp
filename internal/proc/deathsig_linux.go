package proc

import "syscall"

// SetParentDeathSignal makes the child get SIGKILL when host-shell exits, however it exits
// (including SIGKILL), so commands do not outlive a host-shell that was killed. Only the direct
// child gets the signal; grandchildren that keep running are reparented and may remain.
//
// The kernel ties the signal to the OS thread that started the child, not to the process.
// host-shell never locks goroutines to OS threads (runtime.LockOSThread), so the Go runtime keeps
// its threads alive and the signal is sent only when the whole process exits.
func SetParentDeathSignal(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}

// KillChildrenOnExit does nothing on Linux: SetParentDeathSignal handles each child.
func KillChildrenOnExit() error { return nil }
