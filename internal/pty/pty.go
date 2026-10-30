// Package pty starts child processes attached to a pseudo terminal (pty on Linux, ConPTY on Windows).
//
// It runs programs that expect a terminal (password prompts, REPLs, full-screen displays,
// colored output and so on) on a terminal instead of standard I/O pipes.
package pty

import (
	"context"
	"io"
	"os/exec"
)

// errEOF is the end of terminal output. It turns the OS-specific ways of ending into io.EOF.
var errEOF = io.EOF

// Default terminal size.
const (
	DefaultCols = 120
	DefaultRows = 40
)

// Start starts cmd attached to a new pseudo terminal.
// Pass a cmd built with proc.Command, without standard input or output set.
// When ctx ends, the process tree is ended.
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
