//go:build windows

package proc

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

func shellCommand(ctx context.Context, sh, line string, interactive bool) (*exec.Cmd, error) {
	switch sh {
	case "cmd":
		comspec := comSpec()
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

// setupProcess starts the process in a console of its own without a window (CREATE_NO_WINDOW), or,
// with sharedConsole, in host-shell's console (UTF8Console), and kills the entire process tree
// (SignalGroup) on cancellation.
func setupProcess(cmd *exec.Cmd, sharedConsole bool) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !sharedConsole {
		cmd.SysProcAttr.CreationFlags |= createNoWindow
	}
	cmd.Cancel = func() error {
		_ = SignalGroup(cmd.Process.Pid, "kill")
		return cmd.Process.Kill()
	}
}

// Signal terminates pid; only "kill" is supported on Windows.
func Signal(pid int, name string) error {
	if name != "kill" {
		return fmt.Errorf("signal %q is not supported on Windows (only kill)", name)
	}
	_, err := terminate(uint32(pid))
	return err
}

// SignalGroup terminates pid and its descendants; only "kill" is supported on Windows.
//
// It calls TerminateProcess directly instead of taskkill: taskkill relies on WMI, which a standard user
// connected over SSH (a network logon) may not reach, while terminating the user's own processes is
// allowed. Descendants come from Toolhelp snapshots; a child counts only if it started after its parent,
// so a parent ID reused by an unrelated process is not followed. A few passes catch children started
// while the tree is being terminated.
func SignalGroup(pid int, name string) error {
	if name != "kill" {
		return fmt.Errorf("signal %q is not supported on Windows (only kill)", name)
	}
	root := uint32(pid)
	rootCreated, err := terminate(root)
	if err != nil && !errors.Is(err, errNoProcess) {
		return err
	}
	killed := map[uint32]uint64{root: rootCreated}
	for pass := 0; pass < 3; pass++ {
		found, serr := killDescendants(killed)
		if serr != nil {
			return serr
		}
		if !found {
			break
		}
	}
	if errors.Is(err, errNoProcess) && len(killed) == 1 {
		return err
	}
	return nil
}

var errNoProcess = errors.New("no such process")

// terminate kills pid and returns its creation time (100 ns units since 1601).
func terminate(pid uint32) (uint64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return 0, fmt.Errorf("process %d: %w", pid, errNoProcess)
		}
		return 0, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	created := creationTime(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		// A process that is already exiting refuses termination with access denied.
		var code uint32
		if windows.GetExitCodeProcess(h, &code) == nil && code != stillActive {
			return created, nil
		}
		return created, fmt.Errorf("terminate process %d: %w", pid, err)
	}
	return created, nil
}

const stillActive = 259

func creationTime(h windows.Handle) uint64 {
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return 0
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
}

// killDescendants terminates processes whose parent is in killed and that started after that parent,
// adding them to killed. It reports whether it found any.
func killDescendants(killed map[uint32]uint64) (bool, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID != e.ParentProcessID {
			children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
		}
	}
	found := false
	queue := make([]uint32, 0, len(killed))
	for pid := range killed {
		queue = append(queue, pid)
	}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if _, done := killed[child]; done {
				continue
			}
			h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, child)
			if err != nil {
				continue // exited, or not ours to terminate
			}
			created := creationTime(h)
			if created != 0 && created >= killed[parent] {
				windows.TerminateProcess(h, 1)
				killed[child] = created
				queue = append(queue, child)
				found = true
			}
			windows.CloseHandle(h)
		}
	}
	return found, nil
}

// comSpec returns the command interpreter cmd.exe is started as.
func comSpec() string { return cmp.Or(os.Getenv("ComSpec"), "cmd.exe") }
