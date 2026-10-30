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
		// Pass the raw command line via SysProcAttr.CmdLine so cmd.exe's own quoting rules apply.
		// Built-in cmd commands write in the OEM code page, which textenc converts to UTF-8.
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

// KillChildrenOnExit assigns host-shell to a Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so
// all child processes and their descendants terminate when host-shell exits or is killed.
// The uninheritable job handle is intentionally kept open for the lifetime of the process.
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

// setupProcess hides the console window and kills the entire process tree via taskkill /T on cancellation.
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

// SignalNames lists the signal names accepted by Signal on Windows.
const SignalNames = "kill"

// Signal terminates pid (and its descendants when group is true) via taskkill; only "kill" is supported on Windows.
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
