// Package audit records every operation host-shell accepts in a log file on the target.
//
// Standard output carries MCP JSON-RPC only, so this package never writes to it.
package audit

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// keepDays is how many files of earlier days are kept besides the current day's file.
const keepDays = 5

// dateLayout is the date put into the file names.
const dateLayout = "2006-01-02"

// Redactor is implemented by input types that replace the arguments written to the log.
// It keeps values such as written content and passwords out of the log.
type Redactor interface {
	AuditArgs() any
}

// Logger writes the audit log as JSON Lines, one file per day.
type Logger struct {
	l *slog.Logger
}

// Open creates a logger that appends to a file per day named after path: audit.log becomes
// audit-2026-10-31.log, by the local date of each record. Files of all but the last keepDays
// earlier days are removed. An empty path records nothing.
//
// Each record opens the file, appends one line with a single write and closes it again, so the
// host-shell processes of several SSH sessions can share the files without renaming anything.
func Open(path string) (*Logger, error) {
	return open(path, time.Now)
}

func open(path string, now func() time.Time) (*Logger, error) {
	if path == "" {
		return &Logger{l: slog.New(slog.DiscardHandler)}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w := &dailyWriter{base: path, now: now}
	// Fail early when the file cannot be written.
	f, err := os.OpenFile(w.name(now().Format(dateLayout)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	return &Logger{l: slog.New(slog.NewJSONHandler(w, nil))}, nil
}

// Close is kept for symmetry with Open; no file stays open between records.
func (a *Logger) Close() error { return nil }

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

// dailyWriter appends each write (one record from slog) to the file of the current day.
type dailyWriter struct {
	mu   sync.Mutex
	base string
	now  func() time.Time
	day  string // the day this process last wrote, to prune old files once per day
}

// name returns the file name for day: the date goes before the extension of base.
func (w *dailyWriter) name(day string) string {
	ext := filepath.Ext(w.base)
	return strings.TrimSuffix(w.base, ext) + "-" + day + ext
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	day := w.now().Format(dateLayout)
	if day != w.day {
		w.day = day
		w.prune(day)
	}
	f, err := os.OpenFile(w.name(day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := f.Write(p)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// prune removes the files of all but the keepDays most recent days other than today.
// Only names of the form <base>-YYYY-MM-DD<ext> with a valid date are touched. Errors are ignored:
// another host-shell may have removed the file already, or (on Windows) have it open for a moment;
// the next day's prune tries again.
func (w *dailyWriter) prune(today string) {
	ext := filepath.Ext(w.base)
	prefix := filepath.Base(strings.TrimSuffix(w.base, ext)) + "-"
	entries, err := os.ReadDir(filepath.Dir(w.base))
	if err != nil {
		return
	}
	var days []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, ext) || len(n) != len(prefix)+len(dateLayout)+len(ext) {
			continue
		}
		day := n[len(prefix) : len(prefix)+len(dateLayout)]
		if _, err := time.Parse(dateLayout, day); err != nil || day == today {
			continue
		}
		days = append(days, day)
	}
	slices.Sort(days)
	slices.Reverse(days)
	for _, day := range days[min(keepDays, len(days)):] {
		os.Remove(w.name(day))
	}
}
