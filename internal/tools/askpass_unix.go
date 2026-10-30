//go:build !windows

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// askpassEnv holds the FIFO path used by sudo -A (see withAskpass).
const askpassEnv = "HOST_SHELL_ASKPASS"

// passwordTries is the number of password lines queued for sudo (up to 3 prompts).
const passwordTries = 3

// Askpass runs host-shell as the sudo -A askpass helper when invoked by sudo_run,
// reading one line from the HOST_SHELL_ASKPASS FIFO and writing it to stdout.
// If ran is true, the caller must exit with code.
//
// Because sudo closes descriptors above stderr before invoking the helper and env vars are visible
// to other processes, the password is buffered in a 0600 FIFO inside a 0700 directory for the duration
// of the command. Requiring a single non-flag argument (the prompt passed by sudo) prevents a nested
// host-shell process from acting as the helper when sudo preserves the environment.
func Askpass() (ran bool, code int) {
	path := os.Getenv(askpassEnv)
	if path == "" || len(os.Args) != 2 || strings.HasPrefix(os.Args[1], "-") {
		return false, 0
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "host-shell askpass:", err)
		return true, 1
	}
	defer f.Close()
	// Time out if sudo prompts more times than passwords were queued.
	f.SetReadDeadline(time.Now().Add(5 * time.Second))
	var line []byte
	b := make([]byte, 1)
	for {
		// Read one byte at a time so subsequent copies remain in the pipe for retries.
		if _, err := f.Read(b); err != nil {
			fmt.Fprintln(os.Stderr, "host-shell askpass:", err)
			return true, 1
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
	}
	os.Stdout.Write(append(line, '\n'))
	return true, 0
}

// withAskpass creates the sudo -A FIFO and returns the sudo environment and a cleanup function.
func withAskpass(password string) (env map[string]string, cleanup func(), err error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "host-shell-askpass-") // mode 0700
	if err != nil {
		return nil, nil, err
	}
	fifo := filepath.Join(dir, "password")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	// Open O_RDWR so opening does not block waiting for a reader and buffered bytes stay until read or closed.
	f, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	if _, err := f.WriteString(strings.Repeat(password+"\n", passwordTries)); err != nil {
		f.Close()
		os.RemoveAll(dir)
		return nil, nil, err
	}
	cleanup = func() {
		f.Close()
		os.RemoveAll(dir)
	}
	return map[string]string{"SUDO_ASKPASS": exe, askpassEnv: fifo}, cleanup, nil
}
