// Package pty は子プロセスを疑似端末 (Linux の pty、Windows の ConPTY) につないで起動します。
//
// 端末を前提にするプログラム (パスワードの入力、REPL、全画面の表示、色付きの出力など) を、
// 標準入出力のパイプではなく端末として動かすために使います。
package pty

import (
	"context"
	"io"
	"os/exec"
)

// errEOF は端末の出力の終わりです。OS ごとの終わり方の違いを io.EOF にそろえます。
var errEOF = io.EOF

// 端末の大きさの既定です。
const (
	DefaultCols = 120
	DefaultRows = 40
)

// Start は cmd を新しい疑似端末につないで起動します。
// cmd は proc.Command で作ったものを渡します。標準入出力は設定しません。
// ctx が終了すると、プロセス ツリーを終了させます。
func Start(ctx context.Context, cmd *exec.Cmd, cols, rows int) (*Terminal, error) {
	if cols <= 0 {
		cols = DefaultCols
	}
	if rows <= 0 {
		rows = DefaultRows
	}
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	return start(ctx, cmd, cols, rows)
}
