// Package pty runs child processes attached to a pseudo-terminal (POSIX pty on Linux, ConPTY on Windows)
// for interactive prompts, REPLs, full-screen programs, and colorized output.
package pty

import (
	"context"
	"io"
	"os/exec"
)

// errEOF normalizes OS-specific terminal hangup errors to io.EOF.
var errEOF = io.EOF

// Default terminal dimensions.
const (
	DefaultCols = 120
	DefaultRows = 40
)

// Start attaches cmd (built with proc.Command, without stdin/stdout set) to a new pseudo-terminal
// of size cols x rows, terminating the process tree when ctx is cancelled.
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
