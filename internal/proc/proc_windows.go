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
	"unsafe"

	"golang.org/x/sys/windows"

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

// KillChildrenOnExit puts host-shell into a new Job Object that ends every process in it when its
// last handle closes (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE). Processes host-shell starts, and their
// descendants, join the job automatically, so they all end when host-shell exits in any way,
// including being killed. The handle is not inheritable and is never closed: closing it is what
// exiting does. Breaking away from the job is not allowed.
func KillChildrenOnExit() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("set job object limits: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("assign host-shell to the job object: %w", err)
	}
	return nil
}

// setupProcess starts the process without a console window,
// and timeouts and stops end the whole process tree with taskkill /T.
// When host-shell exits, the Job Object of KillChildrenOnExit ends the processes.
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
