//go:build !windows

package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/hondarer/host-shell-mcp/internal/ownerdir"
)

// defaultDir is $XDG_RUNTIME_DIR/host-shell/sessions, or host-shell-UID/sessions in the
// temporary directory. macOS uses /tmp, because its per-user temporary directory is too long
// for socket paths.
func defaultDir() (string, error) {
	var base string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		base = filepath.Join(xdg, "host-shell")
	} else {
		tmp := os.TempDir()
		if runtime.GOOS == "darwin" {
			tmp = "/tmp"
		}
		base = filepath.Join(tmp, fmt.Sprintf("host-shell-%d", os.Getuid()))
	}
	dir := filepath.Join(base, "sessions")
	for _, d := range []string{base, dir} {
		if err := ownerdir.Ensure(d); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	return err == nil
}

// startDetached starts the command from newCmd in a session of its own, without a controlling
// terminal, so that it outlives the shell that ran session start --detach.
func startDetached(newCmd func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := newCmd()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, cmd.Start()
}
