//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole  = kernel32.NewProc("AttachConsole")
	procFreeConsole    = kernel32.NewProc("FreeConsole")
	procSetConsoleCP   = kernel32.NewProc("SetConsoleCP")
	procSetConsoleOCP  = kernel32.NewProc("SetConsoleOutputCP")
	consoleOnce        sync.Once
	consoleOK          bool
	errConsoleStdio    = errors.New("standard input or output is a console")
	attachConsoleLimit = 2 * time.Second
)

const cpUTF8 = 65001

// UTF8Console reports whether host-shell has a console of its own, with input and output code
// pages 65001, for PowerShell to share (Spec.SharedConsole). The first call prepares the console;
// later calls return the same result. PowerShell reads stdin ($input) in the input code page of
// the console it starts in, and setting [Console]::InputEncoding in the script has no effect on
// $input, so the code page has to be set before PowerShell starts.
//
// Nothing is prepared when host-shell's stdin or stdout is a console (started in a terminal), so
// that the user's console keeps its code pages; PowerShell then gets a console of its own as before.
func UTF8Console() bool {
	consoleOnce.Do(func() { consoleOK = prepareConsole() == nil })
	return consoleOK
}

// prepareConsole leaves the current console, if any, and attaches to a new console without a
// window, which a helper cmd started with CREATE_NO_WINDOW owns until host-shell has attached.
// AllocConsole is not used: where Windows Terminal is the default terminal (the default on
// Windows 11) it opens a Windows Terminal window that cannot be hidden.
func prepareConsole() error {
	if isConsole(windows.STD_INPUT_HANDLE) || isConsole(windows.STD_OUTPUT_HANDLE) {
		return errConsoleStdio
	}
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	helper := exec.Command(comspec)
	// set /p waits for a line on stdin, so the helper lives until the pipe is closed.
	helper.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       fmt.Sprintf(`"%s" /d /c "set /p x="`, comspec),
		CreationFlags: createNoWindow,
	}
	in, err := helper.StdinPipe()
	if err != nil {
		return err
	}
	if err := helper.Start(); err != nil {
		return fmt.Errorf("start console helper: %w", err)
	}
	defer func() {
		in.Close()
		helper.Wait()
	}()
	procFreeConsole.Call()
	// The helper's console exists once the helper has initialized; retry until then.
	deadline := time.Now().Add(attachConsoleLimit)
	for {
		r, _, err := procAttachConsole.Call(uintptr(helper.Process.Pid))
		if r != 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("attach to the console helper: %w", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if r, _, err := procSetConsoleCP.Call(cpUTF8); r == 0 {
		return fmt.Errorf("set console input code page: %w", err)
	}
	if r, _, err := procSetConsoleOCP.Call(cpUTF8); r == 0 {
		return fmt.Errorf("set console output code page: %w", err)
	}
	return nil
}

// isConsole reports whether the standard handle std refers to a console.
func isConsole(std uint32) bool {
	h, err := windows.GetStdHandle(std)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return false
	}
	return isConsoleHandle(h)
}

func isConsoleHandle(h windows.Handle) bool {
	var mode uint32
	return windows.GetConsoleMode(h, &mode) == nil
}
