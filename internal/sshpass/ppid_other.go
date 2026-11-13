//go:build !windows

package sshpass

import (
	"os"
	"syscall"
)

// sshProcessID identifies the ssh process that started the helper: OpenSSH forks and executes the
// helper, so it is the parent.
func sshProcessID() int { return os.Getppid() }

// stopSSH terminates the ssh process pid, so that it does not go on to send an empty password
// (which the server would count as another failure). It does nothing unless pid is ssh.
func stopSSH(pid int) bool {
	if pid <= 1 || processName(pid) != "ssh" {
		return false
	}
	return syscall.Kill(pid, syscall.SIGTERM) == nil
}
