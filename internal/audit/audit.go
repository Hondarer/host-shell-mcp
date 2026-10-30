// Package audit records every operation host-shell accepts in a log file on the target.
//
// Standard output carries MCP JSON-RPC only, so this package never writes to it.
package audit

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Redactor is implemented by input types that replace the arguments written to the log.
// It keeps values such as written content and passwords out of the log.
type Redactor interface {
	AuditArgs() any
}

// Logger writes the audit log as JSON Lines.
type Logger struct {
	l *slog.Logger
	c io.Closer
}

// Open creates a logger that appends to path. An empty path records nothing.
func Open(path string) (*Logger, error) {
	if path == "" {
		return &Logger{l: slog.New(slog.DiscardHandler)}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{l: slog.New(slog.NewJSONHandler(f, nil)), c: f}, nil
}

// Close closes the log file.
func (a *Logger) Close() error {
	if a.c == nil {
		return nil
	}
	return a.c.Close()
}

// Event records an event such as server start or stop.
func (a *Logger) Event(msg string, attrs ...any) {
	a.l.Info(msg, attrs...)
}

// Tool records one tool call on one line.
func (a *Logger) Tool(name string, args any, d time.Duration, isError bool, err error) {
	if r, ok := args.(Redactor); ok {
		args = r.AuditArgs()
	}
	attrs := []any{
		slog.String("tool", name),
		slog.Any("args", args),
		slog.Int64("duration_ms", d.Milliseconds()),
		slog.Bool("is_error", isError || err != nil),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	a.l.Info("tool_call", attrs...)
}
