// Package config defines host-shell's runtime settings.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// Config holds host-shell's runtime settings.
type Config struct {
	// MaxOutputBytes caps a single output (stdout, stderr, file content and so on).
	// Longer output is truncated, keeping its head and tail.
	MaxOutputBytes int

	// DefaultTimeout is used when exec_run and similar tools omit the timeout.
	DefaultTimeout time.Duration

	// MaxTimeout is the largest timeout the agent may specify.
	MaxTimeout time.Duration

	// WindowsShell is the shell used on Windows when shell is omitted (cmd or powershell).
	// TODO(open question): whether the default should be cmd or PowerShell.
	WindowsShell string

	// LogFile is where the audit log (JSON Lines) is written. An empty string records nothing.
	// TODO(open question): decide the location and retention (rotation).
	LogFile string

	// Policy holds the restrictions given by the user. The zero value means no restriction.
	Policy policy.Options
}

// Default returns the default settings.
func Default() Config {
	return Config{
		MaxOutputBytes: 64 * 1024,
		DefaultTimeout: 60 * time.Second,
		MaxTimeout:     30 * time.Minute,
		WindowsShell:   "powershell",
		LogFile:        DefaultLogFile(),
	}
}

// Validate checks the settings.
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

// ClampTimeout applies the default and the maximum to the seconds the agent specified.
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

// DefaultLogFile returns the default audit log location for the OS.
//
//   - Linux: $XDG_STATE_HOME/host-shell/audit.log (default ~/.local/state/host-shell/audit.log)
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
