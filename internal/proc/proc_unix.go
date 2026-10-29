//go:build !windows

package proc

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, sh, line string) (*exec.Cmd, error) {
	switch sh {
	case "sh", "bash":
		return exec.CommandContext(ctx, sh, "-c", line), nil
	case "pwsh":
		return exec.CommandContext(ctx, "pwsh", powershellArgs(line)...), nil
	default:
		return nil, fmt.Errorf("shell %q is not supported on this OS (use sh, bash or pwsh)", sh)
	}
}

// setupProcess は子プロセスを新しいプロセス グループで起動し、
// タイムアウトや停止のときにグループ全体 (孫プロセスを含む) を終了させます。
func setupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
