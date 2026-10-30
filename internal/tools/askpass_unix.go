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

// askpassEnv names the named pipe that holds the password for sudo -A (see withAskpass).
const askpassEnv = "HOST_SHELL_ASKPASS"

// passwordTries is how many copies of the password are queued: sudo asks up to three times.
const passwordTries = 3

// Askpass runs host-shell as the askpass helper of sudo -A when sudo started it for sudo_run:
// it reads one line from the named pipe in HOST_SHELL_ASKPASS and prints it for sudo.
// It reports whether it ran; the caller then exits with the returned code.
//
// sudo closes every descriptor above stderr before it starts the helper, so an inherited pipe
// cannot carry the password, and an environment variable would expose it to other processes.
// The named pipe lives in a directory only the user can open, and holds the password only until
// the command ends. sudo starts the helper with the prompt as its single argument; requiring that
// keeps a host-shell started by the command itself (when sudo keeps the environment) from acting
// as the helper.
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
	// If sudo asks more often than passwords were queued, fail instead of waiting forever.
	f.SetReadDeadline(time.Now().Add(5 * time.Second))
	var line []byte
	b := make([]byte, 1)
	for {
		// One byte at a time, so the next copy stays in the pipe for the next prompt.
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

// withAskpass prepares the named pipe for sudo -A and returns the environment for sudo.
// cleanup removes the pipe and its directory; call it after the command ends.
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
	// Opening for reading and writing does not wait for a reader. The copies stay in the pipe's
	// buffer until the helper reads them, or are dropped with the pipe.
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
