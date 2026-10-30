//go:build !windows

package pty

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"

	cpty "github.com/creack/pty"
)

// Terminal は疑似端末につないだプロセスです。
type Terminal struct {
	cmd  *exec.Cmd
	f    *os.File
	once sync.Once
}

func start(_ context.Context, cmd *exec.Cmd, cols, rows int) (*Terminal, error) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if !slices.ContainsFunc(cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "TERM=") }) {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	// 新しいセッションを作り、端末を制御端末にする。セッションのリーダーはプロセス グループのリーダーでもあるため、
	// proc が設定した停止の処理 (グループ全体への SIGKILL) はそのまま使える。
	f, err := cpty.StartWithAttrs(cmd, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)},
		&syscall.SysProcAttr{Setsid: true, Setctty: true})
	if err != nil {
		return nil, err
	}
	return &Terminal{cmd: cmd, f: f}, nil
}

// Pid はプロセスの識別子を返します。
func (t *Terminal) Pid() int { return t.cmd.Process.Pid }

// Read は端末の出力を読みます。すべての書き手が端末を閉じると io.EOF を返します。
func (t *Terminal) Read(p []byte) (int, error) {
	n, err := t.f.Read(p)
	if err != nil && (errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)) {
		// Linux では、端末の子側がすべて閉じられると EIO になる。
		err = errEOF
	}
	return n, err
}

// Write は端末に入力を送ります。
func (t *Terminal) Write(p []byte) (int, error) { return t.f.Write(p) }

// Resize は端末の大きさを変えます。
func (t *Terminal) Resize(cols, rows int) error {
	return cpty.Setsize(t.f, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Wait はプロセスの終了を待ち、終了コードを返します。
func (t *Terminal) Wait() (int, error) {
	err := t.cmd.Wait()
	if t.cmd.ProcessState == nil {
		return -1, err
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		err = nil
	}
	return t.cmd.ProcessState.ExitCode(), err
}

// Close は端末を閉じます。読み取り中の Read は終わります。
func (t *Terminal) Close() error {
	var err error
	t.once.Do(func() { err = t.f.Close() })
	return err
}

// EOF は入力の終わりを伝えるための、端末に送る文字です (Ctrl-D)。
const EOF = "\x04"
