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

// setupProcess places the child in a new process group (so cancellation kills the entire group,
// including grandchildren) and configures parent-death termination.
func setupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	SetParentDeathSignal(cmd.SysProcAttr)
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

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

// SignalNames lists the signal names accepted by Signal.
const SignalNames = "interrupt, terminate, hangup, quit, kill, stop, continue, usr1, usr2"

// Signal sends a named signal to pid, or to the process group -pid when group is true.
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
