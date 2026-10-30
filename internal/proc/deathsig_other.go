//go:build !windows

package proc

// KillChildrenOnExit does nothing on this OS.
func KillChildrenOnExit() error { return nil }
