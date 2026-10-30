//go:build !windows

package proc

// UTF8Console reports whether host-shell has a UTF-8 console for PowerShell to share. Only Windows
// needs one; elsewhere PowerShell reads stdin as UTF-8.
func UTF8Console() bool { return false }
