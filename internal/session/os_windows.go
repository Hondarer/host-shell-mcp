package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/hondarer/host-shell-mcp/internal/ownerdir"
)

// defaultDir is %LOCALAPPDATA%\host-shell\sessions.
func defaultDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		if base, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "host-shell", "sessions")
	return dir, ownerdir.Ensure(dir)
}

// isTerminal reports whether f is a console.
func isTerminal(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}

// startDetached starts the command from newCmd without a window, in a process group of its own,
// so that Ctrl+C in the shell that ran session start --detach does not reach it. It also leaves
// the job object of that shell when the job allows it, because closing the job would otherwise
// end the session.
func startDetached(newCmd func() *exec.Cmd) (*exec.Cmd, error) {
	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW)
	cmd := newCmd()
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags | windows.CREATE_BREAKAWAY_FROM_JOB}
	if err := cmd.Start(); err == nil {
		return cmd, nil
	}
	// The job does not permit breakaway.
	cmd = newCmd()
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	return cmd, cmd.Start()
}
