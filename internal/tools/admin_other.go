//go:build !linux && !windows

package tools

import (
	"context"
	"errors"
	"syscall"

	"github.com/hondarer/host-shell-mcp/internal/proc"
)

var errAdminUnsupported = errors.New("this tool is supported on Linux and Windows; use exec_run with the OS commands instead")

func killProcess(_ context.Context, _ *Deps, pid int, sig string, _, sudo bool) ([]int, error) {
	if sudo {
		return nil, errAdminUnsupported
	}
	return []int{pid}, proc.Signal(pid, sig, false)
}

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func listServices(context.Context, *Deps, bool) ([]Service, error) { return nil, errAdminUnsupported }

func controlService(context.Context, *Deps, SvcControlInput, string) (SvcControlOutput, error) {
	return SvcControlOutput{}, errAdminUnsupported
}

func queryLog(context.Context, *Deps, LogQueryInput) ([]LogEntry, error) {
	return nil, errAdminUnsupported
}

func listDisks(bool) ([]Disk, error) { return nil, errAdminUnsupported }
