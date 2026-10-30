//go:build windows

package proc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

const createNoWindow = 0x08000000

func shellCommand(ctx context.Context, sh, line string, interactive bool) (*exec.Cmd, error) {
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
		return exec.CommandContext(ctx, "powershell.exe", powershellArgs(line, interactive)...), nil
	case "pwsh":
		return exec.CommandContext(ctx, "pwsh.exe", powershellArgs(line, interactive)...), nil
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

// SignalNames は Signal で使える名前です。Windows ではプロセスの強制終了だけです。
const SignalNames = "kill"

// Signal はプロセスを終了させます。Windows ではシグナルがないため、kill だけに対応します。
// group の場合は、子孫のプロセスもまとめて終了させます。
func Signal(pid int, name string, group bool) error {
	if name != "kill" {
		return fmt.Errorf("signal %q is not supported on Windows (only kill)", name)
	}
	args := []string{"/F", "/PID", strconv.Itoa(pid)}
	if group {
		args = append([]string{"/T"}, args...)
	}
	out, err := exec.Command("taskkill", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill: %v: %s", err, strings.TrimSpace(textenc.Console().Decode(out)))
	}
	return nil
}
