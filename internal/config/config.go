// Package config は host-shell の実行時設定を定義します。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// Config は host-shell の実行時設定です。
type Config struct {
	// MaxOutputBytes は 1 つの出力 (標準出力、標準エラー、ファイル内容など) の上限です。
	// 超えた場合は先頭と末尾を残して切り詰めます。
	MaxOutputBytes int

	// DefaultTimeout は exec_run などでタイムアウトが省略されたときの値です。
	DefaultTimeout time.Duration

	// MaxTimeout はエージェントが指定できるタイムアウトの上限です。
	MaxTimeout time.Duration

	// WindowsShell は Windows で shell が省略されたときに使うシェル (cmd または powershell) です。
	// TODO(未決事項): 既定を cmd と PowerShell のどちらにするか。
	WindowsShell string

	// LogFile は操作ログ (JSON Lines) の出力先です。空文字の場合は記録しません。
	// TODO(未決事項): 保存先と保持期間 (ローテーション) を決める。
	LogFile string

	// Policy は利用者が指定した制限です。ゼロ値は制限なしです。
	Policy policy.Options
}

// Default は既定の設定を返します。
func Default() Config {
	return Config{
		MaxOutputBytes: 64 * 1024,
		DefaultTimeout: 60 * time.Second,
		MaxTimeout:     30 * time.Minute,
		WindowsShell:   "powershell",
		LogFile:        DefaultLogFile(),
	}
}

// Validate は設定値を検査します。
func (c Config) Validate() error {
	if c.MaxOutputBytes < 1024 {
		return fmt.Errorf("max-output must be at least 1024 bytes: %d", c.MaxOutputBytes)
	}
	if c.DefaultTimeout <= 0 || c.MaxTimeout < c.DefaultTimeout {
		return fmt.Errorf("invalid timeout: default=%s max=%s", c.DefaultTimeout, c.MaxTimeout)
	}
	switch c.WindowsShell {
	case "cmd", "powershell", "pwsh":
	default:
		return fmt.Errorf("windows-shell must be cmd, powershell or pwsh: %q", c.WindowsShell)
	}
	return nil
}

// ClampTimeout はエージェントが指定した秒数を既定値と上限で補正します。
func (c Config) ClampTimeout(sec int) time.Duration {
	if sec <= 0 {
		return c.DefaultTimeout
	}
	d := time.Duration(sec) * time.Second
	if d > c.MaxTimeout {
		return c.MaxTimeout
	}
	return d
}

// DefaultLogFile は OS ごとの操作ログの既定の保存先を返します。
//
//   - Linux: $XDG_STATE_HOME/host-shell/audit.log (既定は ~/.local/state/host-shell/audit.log)
//   - Windows: %LOCALAPPDATA%\host-shell\audit.log
func DefaultLogFile() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "host-shell", "audit.log")
		}
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "host-shell", "audit.log")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "host-shell", "audit.log")
	}
	return ""
}
