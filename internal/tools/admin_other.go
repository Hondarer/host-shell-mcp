//go:build !linux && !windows

package tools

import (
	"context"
	"errors"
	"regexp"

	"github.com/hondarer/host-shell-mcp/internal/proc"
)

var errAdminUnsupported = errors.New("this tool is supported on Linux and Windows; use exec_run with the OS commands instead")

func killProcess(_ context.Context, _ *Deps, in ProcKillInput, sig string) ([]int, error) {
	if in.Sudo {
		return nil, errAdminUnsupported
	}
	return []int{in.PID}, proc.Signal(in.PID, sig)
}

func listServices(context.Context, *Deps, bool) ([]Service, error) { return nil, errAdminUnsupported }

func controlService(context.Context, *Deps, SvcControlInput, string) (SvcControlOutput, error) {
	return SvcControlOutput{}, errAdminUnsupported
}

func queryLog(context.Context, *Deps, LogQueryInput, *regexp.Regexp) ([]LogEntry, error) {
	return nil, errAdminUnsupported
}

func listDisks(bool) ([]Disk, error) { return nil, errAdminUnsupported }
