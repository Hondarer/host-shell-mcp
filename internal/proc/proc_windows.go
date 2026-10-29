//go:build windows

package proc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

func shellCommand(ctx context.Context, sh, line string) (*exec.Cmd, error) {
	switch sh {
	case "cmd":
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		cmd := exec.CommandContext(ctx, comspec)
		// cmd は独自の引用符規則を持つため、Go の引数エスケープを使わずにコマンド行をそのまま渡す。
		// TODO: cmd の内部コマンドの出力は OEM コード ページ (日本語環境では CP932) になるため、UTF-8 への変換を検討する。
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CmdLine: fmt.Sprintf(`"%s" /d /s /c "%s"`, comspec, line),
		}
		return cmd, nil
	case "powershell":
		return exec.CommandContext(ctx, "powershell.exe", powershellArgs(line)...), nil
	case "pwsh":
		return exec.CommandContext(ctx, "pwsh.exe", powershellArgs(line)...), nil
	default:
		return nil, fmt.Errorf("shell %q is not supported on this OS (use cmd, powershell or pwsh)", sh)
	}
}

// setupProcess はコンソール ウィンドウを作らずに起動し、
// タイムアウトや停止のときに taskkill /T でプロセス ツリー全体を終了させます。
// TODO: Job Object を使い、host-shell の終了時に子孫プロセスも確実に終了させる。
func setupProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	cmd.Cancel = func() error {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return cmd.Process.Kill()
	}
}
