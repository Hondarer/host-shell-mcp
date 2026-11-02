//go:build !windows

package proc

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
// including grandchildren) and configures parent-death termination. sharedConsole applies to
// Windows only.
func setupProcess(cmd *exec.Cmd, sharedConsole bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	SetParentDeathSignal(cmd.SysProcAttr)
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// signals lists the accepted signal names with their kill(1) names.
var signals = []struct {
	name string
	sig  syscall.Signal
	kill string
}{
	{"interrupt", syscall.SIGINT, "INT"},
	{"terminate", syscall.SIGTERM, "TERM"},
	{"hangup", syscall.SIGHUP, "HUP"},
	{"quit", syscall.SIGQUIT, "QUIT"},
	{"kill", syscall.SIGKILL, "KILL"},
	{"stop", syscall.SIGSTOP, "STOP"},
	{"continue", syscall.SIGCONT, "CONT"},
	{"usr1", syscall.SIGUSR1, "USR1"},
	{"usr2", syscall.SIGUSR2, "USR2"},
}

// signalNames lists the signal names accepted by Signal.
var signalNames = func() string {
	names := make([]string, len(signals))
	for i, s := range signals {
		names[i] = s.name
	}
	return strings.Join(names, ", ")
}()

func lookupSignal(name string) (syscall.Signal, string, error) {
	for _, s := range signals {
		if s.name == name {
			return s.sig, s.kill, nil
		}
	}
	return 0, "", fmt.Errorf("unknown signal %q (use one of: %s)", name, signalNames)
}

// Signal sends a named signal to pid.
func Signal(pid int, name string) error {
	sig, _, err := lookupSignal(name)
	if err != nil {
		return err
	}
	return syscall.Kill(pid, sig)
}

// SignalGroup sends a named signal to the process group led by pid.
func SignalGroup(pid int, name string) error {
	sig, _, err := lookupSignal(name)
	if err != nil {
		return err
	}
	return syscall.Kill(-pid, sig)
}
