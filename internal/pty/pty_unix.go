//go:build !windows

package pty

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"

	cpty "github.com/creack/pty"

	"github.com/hondarer/host-shell-mcp/internal/proc"
)

// Terminal represents a process attached to a pseudo-terminal.
type Terminal struct {
	cmd  *exec.Cmd
	f    *os.File
	once sync.Once
}

func start(_ context.Context, cmd *exec.Cmd, cols, rows int) (*Terminal, error) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if !slices.ContainsFunc(cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "TERM=") }) {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	// Start a new session with the PTY as its controlling terminal; as session leader, the child
	// also leads its process group, preserving proc's group-kill cancellation.
	attr := &syscall.SysProcAttr{Setsid: true, Setctty: true}
	proc.SetParentDeathSignal(attr)
	f, err := cpty.StartWithAttrs(cmd, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}, attr)
	if err != nil {
		return nil, err
	}
	return &Terminal{cmd: cmd, f: f}, nil
}

// Pid returns the process ID.
func (t *Terminal) Pid() int { return t.cmd.Process.Pid }

// Read reads terminal output, mapping Linux's EIO (returned when all child fds close) to io.EOF.
func (t *Terminal) Read(p []byte) (int, error) {
	n, err := t.f.Read(p)
	if err != nil && (errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)) {
		err = errEOF
	}
	return n, err
}

// Write sends input to the terminal.
func (t *Terminal) Write(p []byte) (int, error) { return t.f.Write(p) }

// Resize changes the terminal size.
func (t *Terminal) Resize(cols, rows int) error {
	return cpty.Setsize(t.f, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Wait waits for the process to end and returns its exit code.
func (t *Terminal) Wait() (int, error) {
	err := t.cmd.Wait()
	if t.cmd.ProcessState == nil {
		return -1, err
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		err = nil
	}
	return t.cmd.ProcessState.ExitCode(), err
}

// Close closes the master PTY descriptor, unblocking any concurrent Read.
func (t *Terminal) Close() error {
	var err error
	t.once.Do(func() { err = t.f.Close() })
	return err
}

// EOF is the terminal end-of-input byte (Ctrl-D).
const EOF = "\x04"
