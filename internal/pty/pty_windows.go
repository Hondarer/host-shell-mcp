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
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Terminal は疑似端末 (ConPTY) につないだプロセスです。
type Terminal struct {
	hpc     windows.Handle
	process windows.Handle
	pid     int
	in      *os.File // 端末への入力 (こちらが書く側)
	out     *os.File // 端末の出力 (こちらが読む側)

	closeOnce sync.Once
	exited    chan struct{}
}

// os/exec の Cmd は疑似端末の属性を渡せないため (golang/go#62708)、CreateProcess を直接呼ぶ。
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
	// 端末側の端は ConPTY が複製して持つため、ここで閉じる。
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
	// 属性の値は HPCON そのもの (ポインターの指す先ではない) を渡す。
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

// envBlock は環境変数を CreateProcess に渡す形 (NUL 区切り、最後に NUL を 2 つ) にします。
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

// Pid はプロセスの識別子を返します。
func (t *Terminal) Pid() int { return t.pid }

// Read は端末の出力を読みます。端末を閉じると io.EOF を返します。
func (t *Terminal) Read(p []byte) (int, error) {
	n, err := t.out.Read(p)
	if err != nil && (errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed)) {
		err = errEOF
	}
	return n, err
}

// Write は端末に入力を送ります。
func (t *Terminal) Write(p []byte) (int, error) { return t.in.Write(p) }

// Resize は端末の大きさを変えます。
func (t *Terminal) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(t.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Wait はプロセスの終了を待ち、終了コードを返します。
func (t *Terminal) Wait() (int, error) {
	defer close(t.exited)
	if _, err := windows.WaitForSingleObject(t.process, windows.INFINITE); err != nil {
		return -1, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(t.process, &code); err != nil {
		return -1, err
	}
	// ConPTY を閉じると、残りの出力を送り終えてから出力のパイプが閉じられる。
	go t.closeConsole()
	return int(code), nil
}

func (t *Terminal) closeConsole() {
	t.closeOnce.Do(func() {
		windows.ClosePseudoConsole(t.hpc)
		t.in.Close()
	})
}

// Close は端末を閉じます。読み取り中の Read は終わります。
func (t *Terminal) Close() error {
	t.closeConsole()
	windows.CloseHandle(t.process)
	return t.out.Close()
}

// kill はプロセス ツリーを終了させます。
func (t *Terminal) kill() {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(t.pid)).Run()
	_ = windows.TerminateProcess(t.process, 1)
}

// EOF は入力の終わりを伝えるための、端末に送る文字です (Ctrl-Z と Enter)。
const EOF = "\x1a\r"
