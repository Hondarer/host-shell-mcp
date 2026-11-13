//go:build !windows

package sshpass

import (
	"os/exec"
	"syscall"
)

// execTool replaces the wrapper with the client, so signals and the exit status reach the caller directly.
func execTool(path string, args, env []string) (int, error) {
	return 0, syscall.Exec(path, append([]string{path}, args...), env)
}

// Detach starts cmd in a session of its own, without a controlling terminal. OpenSSH before 8.4
// reads passwords from /dev/tty when it can open it, and uses SSH_ASKPASS only when it cannot.
func Detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
