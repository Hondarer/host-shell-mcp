//go:build windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Terminal represents a process attached to a Windows ConPTY pseudo-console.
type Terminal struct {
	hpc     windows.Handle
	process windows.Handle
	pid     int
	in      *os.File // ConPTY input pipe (write side)
	out     *os.File // ConPTY output pipe (read side)

	closeOnce sync.Once
	exited    chan struct{}
	lastRead  atomic.Int64 // UnixNano timestamp of last output read
}

// Output settle timeouts after process exit; ConPTY renders asynchronously, so closing it
// immediately upon process exit can drop trailing output.
const (
	settleIdle = 300 * time.Millisecond
	settleMax  = 2 * time.Second
)

// start calls CreateProcess directly because os/exec cannot pass PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE (golang/go#62708).
func start(ctx context.Context, cmd *exec.Cmd, cols, rows int) (*Terminal, error) {
	ptyIn, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, ptyOut, err := os.Pipe()
	if err != nil {
		ptyIn.Close()
		inW.Close()
		return nil, err
	}
	var hpc windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)},
		windows.Handle(ptyIn.Fd()), windows.Handle(ptyOut.Fd()), 0, &hpc)
	// ConPTY duplicates the pipe handles it needs.
	ptyIn.Close()
	ptyOut.Close()
	if err != nil {
		inW.Close()
		outR.Close()
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	t := &Terminal{hpc: hpc, in: inW, out: outR, exited: make(chan struct{})}
	if err := t.spawn(cmd); err != nil {
		windows.ClosePseudoConsole(hpc)
		inW.Close()
		outR.Close()
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			t.kill()
		case <-t.exited:
		}
	}()
	return t, nil
}

func (t *Terminal) spawn(cmd *exec.Cmd) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE expects the HPCON value itself, not a pointer to it.
	hpc := *(*unsafe.Pointer)(unsafe.Pointer(&t.hpc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hpc, unsafe.Sizeof(t.hpc)); err != nil {
		return err
	}
	cmdline := windows.ComposeCommandLine(cmd.Args)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		cmdline = cmd.SysProcAttr.CmdLine
	}
	app, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return err
	}
	line, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return err
	}
	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return err
		}
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// Pass STARTF_USESTDHANDLES with null handles so the child does not inherit host-shell's
	// stdio (the MCP transport) and bypass the pseudo-console.
	si.Flags = windows.STARTF_USESTDHANDLES
	pi := &windows.ProcessInformation{}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(app, line, nil, nil, false, flags, envBlock(env), dir, &si.StartupInfo, pi); err != nil {
		return fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	windows.CloseHandle(pi.Thread)
	t.process = pi.Process
	t.pid = int(pi.ProcessId)
	return nil
}

// envBlock encodes env as a UTF-16 block of NUL-terminated strings terminated by an extra NUL.
func envBlock(env []string) *uint16 {
	var b []uint16
	for _, kv := range env {
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0]
}

// Pid returns the process ID.
func (t *Terminal) Pid() int { return t.pid }

// Read reads terminal output. It returns io.EOF when the terminal is closed.
func (t *Terminal) Read(p []byte) (int, error) {
	n, err := t.out.Read(p)
	if n > 0 {
		t.lastRead.Store(time.Now().UnixNano())
	}
	if err != nil && (errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed)) {
		err = errEOF
	}
	return n, err
}

// Write sends input to the terminal.
func (t *Terminal) Write(p []byte) (int, error) { return t.in.Write(p) }

// Resize changes the terminal size.
func (t *Terminal) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(t.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Wait waits for the process to end and returns its exit code.
func (t *Terminal) Wait() (int, error) {
	defer close(t.exited)
	if _, err := windows.WaitForSingleObject(t.process, windows.INFINITE); err != nil {
		return -1, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(t.process, &code); err != nil {
		return -1, err
	}
	// Close ConPTY once output settles, which flushes buffered rendering and closes the output pipe.
	go func() {
		deadline := time.Now().Add(settleMax)
		start := time.Now().UnixNano()
		for time.Now().Before(deadline) {
			last := max(t.lastRead.Load(), start)
			if time.Since(time.Unix(0, last)) >= settleIdle {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.closeConsole()
	}()
	return int(code), nil
}

func (t *Terminal) closeConsole() {
	t.closeOnce.Do(func() {
		windows.ClosePseudoConsole(t.hpc)
		t.in.Close()
	})
}

// Close closes the pseudo-console and pipes, unblocking any concurrent Read.
func (t *Terminal) Close() error {
	t.closeConsole()
	windows.CloseHandle(t.process)
	return t.out.Close()
}

func (t *Terminal) kill() {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(t.pid)).Run()
	_ = windows.TerminateProcess(t.process, 1)
}

// EOF is the terminal end-of-input sequence on Windows (Ctrl-Z followed by CR).
const EOF = "\x1a\r"
