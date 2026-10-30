//go:build !windows

package proc

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, sh, line string, interactive bool) (*exec.Cmd, error) {
	switch sh {
	case "sh", "bash":
		return exec.CommandContext(ctx, sh, "-c", line), nil
	case "pwsh":
		return exec.CommandContext(ctx, "pwsh", powershellArgs(line, interactive)...), nil
	default:
		return nil, fmt.Errorf("shell %q is not supported on this OS (use sh, bash or pwsh)", sh)
	}
}

// setupProcess starts the child in a new process group,
// so that timeouts and stops end the whole group (including grandchildren).
func setupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// signals are the signal names Signal can send.
var signals = map[string]syscall.Signal{
	"interrupt": syscall.SIGINT,
	"terminate": syscall.SIGTERM,
	"hangup":    syscall.SIGHUP,
	"quit":      syscall.SIGQUIT,
	"kill":      syscall.SIGKILL,
	"stop":      syscall.SIGSTOP,
	"continue":  syscall.SIGCONT,
	"usr1":      syscall.SIGUSR1,
	"usr2":      syscall.SIGUSR2,
}

// SignalNames are the names Signal accepts.
const SignalNames = "interrupt, terminate, hangup, quit, kill, stop, continue, usr1, usr2"

// Signal sends a signal to a process. With group, it is sent to the whole process group led by pid.
func Signal(pid int, name string, group bool) error {
	sig, ok := signals[name]
	if !ok {
		return fmt.Errorf("unknown signal %q (use one of: %s)", name, SignalNames)
	}
	if group {
		return syscall.Kill(-pid, sig)
	}
	return syscall.Kill(pid, sig)
}
