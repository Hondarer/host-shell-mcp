// Package audit records host-shell operations to daily JSON Lines files on the target.
// It never writes to standard output, which is reserved for MCP JSON-RPC.
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

// keepDays is the number of previous daily log files retained alongside today's file.
const keepDays = 5

const dateLayout = "2006-01-02"

// Redactor is implemented by tool input types that scrub sensitive fields (such as file contents or passwords) before logging.
type Redactor interface {
	AuditArgs() any
}

// Logger writes daily JSON Lines audit logs.
type Logger struct {
	l *slog.Logger
}

// Open creates a logger that appends to daily files derived from path (e.g., audit-2026-10-31.log
// in local time) and prunes files older than keepDays. An empty path disables logging.
//
// Each record opens, appends a single line, and closes the file so concurrent host-shell
// processes can share daily files without coordination or file renaming.
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
	// Verify writability upfront.
	f, err := os.OpenFile(w.name(now().Format(dateLayout)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	return &Logger{l: slog.New(slog.NewJSONHandler(w, nil))}, nil
}

// Close is a no-op retained for symmetry with Open; files are closed after each write.
func (a *Logger) Close() error { return nil }

// Event logs a lifecycle event such as server start or stop.
func (a *Logger) Event(msg string, attrs ...any) {
	a.l.Info(msg, attrs...)
}

// Tool logs a single tool invocation.
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

// dailyWriter appends each slog record to the current day's log file.
type dailyWriter struct {
	mu   sync.Mutex
	base string
	now  func() time.Time
	day  string // last write date, used to prune old files once per day
}

// name inserts day before the file extension of base.
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

// prune removes dated log files (<base>-YYYY-MM-DD<ext>) older than the keepDays most recent
// days before today. Removal errors are ignored (another process may have removed the file or
// hold it open briefly on Windows) and retried on the next day's prune.
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
