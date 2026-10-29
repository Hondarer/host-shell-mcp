// Package audit は host-shell が受け付けたすべての操作を接続先のログ ファイルに記録します。
//
// 標準出力は MCP の JSON-RPC 専用のため、ここでは決して標準出力に書きません。
package audit

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Redactor はログに残す引数を差し替えたい入力型が実装します。
// 書き込み内容やパスワードなど、ログに残すべきでない値を除くために使います。
type Redactor interface {
	AuditArgs() any
}

// Logger は操作ログを JSON Lines で書き出します。
type Logger struct {
	l *slog.Logger
	c io.Closer
}

// Open は path にログを追記するロガーを作ります。path が空の場合は何も記録しません。
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

// Close はログ ファイルを閉じます。
func (a *Logger) Close() error {
	if a.c == nil {
		return nil
	}
	return a.c.Close()
}

// Event はサーバーの起動、停止などの出来事を記録します。
func (a *Logger) Event(msg string, attrs ...any) {
	a.l.Info(msg, attrs...)
}

// Tool はツールの呼び出しを 1 行で記録します。
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
