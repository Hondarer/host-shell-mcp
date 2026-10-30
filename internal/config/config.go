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
	// MaxOutputBytes caps individual output streams (stdout, stderr, file content, etc.);
	// longer output is middle-truncated, keeping its head and tail.
	MaxOutputBytes int

	// DefaultTimeout is used when command execution tools omit a timeout.
	DefaultTimeout time.Duration

	// MaxTimeout caps client-requested timeouts.
	MaxTimeout time.Duration

	// WindowsShell is the default Windows shell (cmd, powershell, or pwsh).
	// It defaults to powershell, which produces UTF-8 output and accepts -EncodedCommand scripts
	// immune to cmd and SSH quoting; cmd remains available via --windows-shell or per-call shell="cmd".
	WindowsShell string

	// LogFile is the base path for daily JSON Lines audit logs (e.g., audit-2026-10-31.log; see package audit).
	// An empty string disables audit logging.
	LogFile string

	// Policy configures access and output restrictions; the zero value imposes none.
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

// ClampTimeout resolves a client-requested timeout in seconds against DefaultTimeout and MaxTimeout.
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

// DefaultLogFile returns the OS-specific default audit log path:
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
