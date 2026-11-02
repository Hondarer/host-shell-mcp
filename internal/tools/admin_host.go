//go:build linux || windows

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// Helpers for the administrative tools that have Linux and Windows implementations.

// runTool runs an administrative command, prefixed with sudo -n when sudo is set (sudo applies on Linux only).
// It goes through procSpec so the policy environment filter and proxy environment apply.
func (d *Deps) runTool(ctx context.Context, argv []string, sudo bool) (proc.Result, error) {
	if sudo && os.Geteuid() != 0 {
		argv = append([]string{"sudo", "-n", "--"}, argv...)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return proc.Result{}, fmt.Errorf("%s is not available on this host: %w", argv[0], err)
	}
	spec := d.procSpec(CommandSpec{Argv: argv})
	return proc.Run(ctx, spec, proc.Options{
		Output:    textenc.Choice{Auto: true, Enc: textenc.Console()},
		Timeout:   d.Cfg.DefaultTimeout,
		MaxOutput: 32 << 20,
	})
}

// parseSince parses a relative duration ("30m", "2h", "1d") or timestamp.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if dur, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-dur), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q (use 30m, 2h, 1d or 2026-10-30 09:00)", s)
}
