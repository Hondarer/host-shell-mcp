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
		// cmd has its own quoting rules, so pass the command line as is instead of using Go's argument escaping.
		// Internal commands of cmd write in the OEM code page; the tools convert that to UTF-8 (textenc, output_encoding auto).
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

// setupProcess starts the process without a console window,
// and timeouts and stops end the whole process tree with taskkill /T.
// TODO: use a Job Object so that descendants surely end when host-shell exits.
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

// SignalNames are the names Signal accepts. On Windows, only killing the process.
const SignalNames = "kill"

// Signal ends a process. Windows has no signals, so only kill is supported.
// With group, descendant processes are ended too.
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
