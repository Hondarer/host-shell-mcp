package tools

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerAdmin(s *mcp.Server, d *Deps) {
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "sys_processes",
		Title: "List processes",
		Description: "List running processes with pid, ppid, name, command line, user, state, start time and memory, ordered by pid. " +
			"filter keeps those whose name or command line contains a substring; pid keeps one process and its descendants. Use proc_kill to end one.",
		Annotations: readOnly(),
	}, d.sysProcesses)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "proc_kill",
		Title: "Signal or kill process",
		Description: "Send a signal to a process by pid (Linux: terminate by default, or kill, interrupt, hangup, ...; Windows: kill). " +
			"tree includes all its descendants. On Linux sudo sends it as root via sudo -n. For jobs started by host-shell use job_stop instead.",
		Annotations: destructive(),
	}, d.procKill)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "svc_list",
		Title: "List services",
		Description: "List services with state and start mode: systemd units on Linux (user for --user units), Windows services on Windows. " +
			"Filter by name/description substring and state (running, stopped, failed).",
		Annotations: readOnly(),
	}, d.svcList)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "svc_control",
		Title: "Control service",
		Description: "Show or change one service: status (details and recent log lines), start, stop, restart, reload (Linux), enable, disable. " +
			"Linux uses systemctl (sudo for system units when not root, user for --user units); Windows uses the Service Control Manager (needs an elevated session for most changes). " +
			"Returns the status after the action.",
		Annotations: destructive(),
	}, d.svcControl)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "log_query",
		Title: "Query system logs",
		Description: "Read the system log as structured entries: the systemd journal on Linux (unit, priority), the Windows Event Log on Windows (log, provider, level, event_id). " +
			"since accepts durations like 30m, 2h, 1d or a timestamp; grep filters messages by regular expression. Newest lines last. " +
			"For plain log files use fs_read (negative offset for the tail) or search_grep.",
		Annotations: readOnly(),
	}, d.logQuery)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:        "sys_disks",
		Title:       "Disk usage",
		Description: "List mounted file systems (Linux) or drives (Windows) with type, total, used and free space. all includes pseudo file systems.",
		Annotations: readOnly(),
	}, d.sysDisks)

	if d.onOS("windows") {
		add(s, d, groupAdmin, &mcp.Tool{
			Name:  "reg_read",
			Title: "Read registry",
			Description: "Read a Windows registry key: its subkeys and values, or one value with value. Keys like HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion " +
				"(HKLM, HKCU, HKCR, HKU, HKCC). view 32 or 64 selects the WOW64 view.",
			Annotations: readOnly(),
		}, d.regRead)
		add(s, d, groupAdmin, &mcp.Tool{
			Name:  "reg_write",
			Title: "Write registry",
			Description: "Create or set a Windows registry value (and create the key if missing). type: REG_SZ (default), REG_EXPAND_SZ, REG_MULTI_SZ (data is an array of strings), " +
				"REG_DWORD, REG_QWORD (data is a number), REG_BINARY (data is base64). Omit value to only create the key.",
			Annotations: destructive(),
		}, d.regWrite)
		add(s, d, groupAdmin, &mcp.Tool{
			Name:        "reg_delete",
			Title:       "Delete registry",
			Description: "Delete a Windows registry value (with value) or key (recursive deletes a key with all subkeys).",
			Annotations: destructive(),
		}, d.regDelete)
	}
}

// ---- proc_kill ----

// ProcKillInput is the input of proc_kill.
type ProcKillInput struct {
	PID    int    `json:"pid" jsonschema:"process id (see sys_processes)"`
	Signal string `json:"signal,omitempty" jsonschema:"Linux: terminate (default), kill, interrupt, hangup, quit, stop, continue, usr1, usr2. Windows: kill (default, the only one)"`
	Tree   bool   `json:"tree,omitempty" jsonschema:"also signal all descendant processes"`
	Sudo   bool   `json:"sudo,omitempty" jsonschema:"Linux: send the signal as root with sudo -n (for processes of other users. If sudo needs a password, use sudo_run with password instead)"`
}

// ProcKillOutput is the output of proc_kill.
type ProcKillOutput struct {
	Signaled []int `json:"signaled" jsonschema:"process ids the signal was sent to"`
	Exited   bool  `json:"exited" jsonschema:"the process was gone within 3 seconds"`
}

func (d *Deps) procKill(ctx context.Context, _ *mcp.CallToolRequest, in ProcKillInput) (*mcp.CallToolResult, ProcKillOutput, error) {
	if in.PID <= 0 {
		return nil, ProcKillOutput{}, errors.New("pid is required")
	}
	sig := in.Signal
	if sig == "" {
		sig = "terminate"
		if runtime.GOOS == "windows" {
			sig = "kill"
		}
	}
	pids, err := killProcess(ctx, d, in.PID, sig, in.Tree, in.Sudo)
	if err != nil {
		return nil, ProcKillOutput{}, err
	}
	out := ProcKillOutput{Signaled: pids}
	for range 30 {
		if !processAlive(in.PID) {
			out.Exited = true
			break
		}
		select {
		case <-ctx.Done():
			return nil, out, nil
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, out, nil
}

// ---- svc_list / svc_control ----

// SvcListInput is the input of svc_list.
type SvcListInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"case-insensitive substring of the name or description"`
	State  string `json:"state,omitempty" jsonschema:"running, stopped, failed or all (default)"`
	User   bool   `json:"user,omitempty" jsonschema:"Linux: list the user's systemd units (systemctl --user)"`
}

// Service is the state of a service.
type Service struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	State       string `json:"state" jsonschema:"running, stopped, failed, starting, stopping or the native state"`
	SubState    string `json:"sub_state,omitempty" jsonschema:"Linux: systemd sub-state such as running, exited, dead"`
	StartMode   string `json:"start_mode,omitempty" jsonschema:"Linux: enabled, disabled, static, masked...; Windows: auto, delayed-auto, manual, disabled"`
	PID         int    `json:"pid,omitempty"`
}

// SvcListOutput is the output of svc_list.
type SvcListOutput struct {
	Services  []Service `json:"services"`
	Truncated bool      `json:"truncated,omitempty"`
}

func (d *Deps) svcList(ctx context.Context, _ *mcp.CallToolRequest, in SvcListInput) (*mcp.CallToolResult, SvcListOutput, error) {
	all, err := listServices(ctx, d, in.User)
	if err != nil {
		return nil, SvcListOutput{}, err
	}
	out := SvcListOutput{Services: []Service{}}
	filter := strings.ToLower(in.Filter)
	for _, s := range all {
		if filter != "" && !strings.Contains(strings.ToLower(s.Name+" "+s.Description), filter) {
			continue
		}
		if st := orDefault(in.State, "all"); st != "all" && s.State != st {
			continue
		}
		if len(out.Services) >= 2000 {
			out.Truncated = true
			break
		}
		out.Services = append(out.Services, s)
	}
	return nil, out, nil
}

// SvcControlInput is the input of svc_control.
type SvcControlInput struct {
	Name    string `json:"name" jsonschema:"service name: systemd unit (nginx or nginx.service) or Windows service name (not the display name)"`
	Action  string `json:"action,omitempty" jsonschema:"status (default), start, stop, restart, reload (Linux), enable, disable"`
	User    bool   `json:"user,omitempty" jsonschema:"Linux: a user unit (systemctl --user)"`
	Sudo    bool   `json:"sudo,omitempty" jsonschema:"Linux: run systemctl with sudo -n (needed for system units when host-shell is not root. If sudo needs a password, use sudo_run with password instead)"`
	WaitSec int    `json:"wait_sec,omitempty" jsonschema:"Windows: seconds to wait for start/stop to complete (default 30)"`
}

// SvcControlOutput is the output of svc_control.
type SvcControlOutput struct {
	Service    Service           `json:"service"`
	Properties map[string]string `json:"properties,omitempty" jsonschema:"details such as the executable path, account and main pid"`
	StatusText string            `json:"status_text,omitempty" jsonschema:"Linux: output of systemctl status including recent log lines"`
	Output     string            `json:"output,omitempty" jsonschema:"output of the control command"`
}

func (d *Deps) svcControl(ctx context.Context, _ *mcp.CallToolRequest, in SvcControlInput) (*mcp.CallToolResult, SvcControlOutput, error) {
	if in.Name == "" {
		return nil, SvcControlOutput{}, errors.New("name is required")
	}
	action := orDefault(in.Action, "status")
	switch action {
	case "status", "start", "stop", "restart", "reload", "enable", "disable":
	default:
		return nil, SvcControlOutput{}, fmt.Errorf("unknown action %q", in.Action)
	}
	if d.Policy.ReadOnly && action != "status" {
		return nil, SvcControlOutput{}, errors.New("policy: read-only mode allows only action status")
	}
	out, err := controlService(ctx, d, in, action)
	if err != nil {
		return nil, SvcControlOutput{}, err
	}
	return nil, out, nil
}

// ---- log_query ----

// LogQueryInput is the input of log_query.
type LogQueryInput struct {
	Unit     string `json:"unit,omitempty" jsonschema:"Linux: systemd unit (e.g. nginx); Windows: event provider (source) name"`
	Log      string `json:"log,omitempty" jsonschema:"Windows: event log name: System (default), Application, Security, Setup or a channel such as Microsoft-Windows-PowerShell/Operational. Linux: kernel for kernel messages only"`
	Since    string `json:"since,omitempty" jsonschema:"only entries newer than this: a duration ago (30m, 2h, 1d) or a timestamp (2026-10-30 09:00, RFC3339)"`
	Until    string `json:"until,omitempty" jsonschema:"only entries older than this (same formats as since)"`
	Priority string `json:"priority,omitempty" jsonschema:"minimum severity: Linux emerg, alert, crit, err, warning, notice, info, debug; Windows critical, error, warning, information, verbose"`
	EventID  int    `json:"event_id,omitempty" jsonschema:"Windows: only this event id"`
	Grep     string `json:"grep,omitempty" jsonschema:"regular expression the message must match (case-insensitive)"`
	Lines    int    `json:"lines,omitempty" jsonschema:"maximum entries, newest kept (default 100, max 2000)"`
	User     bool   `json:"user,omitempty" jsonschema:"Linux: the user's journal (journalctl --user)"`
	Sudo     bool   `json:"sudo,omitempty" jsonschema:"Linux: run journalctl with sudo -n (to read the system journal without journal group membership. If sudo needs a password, use sudo_run with password instead)"`
}

// LogEntry is one log entry.
type LogEntry struct {
	Time     string `json:"time"`
	Priority string `json:"priority"`
	Source   string `json:"source,omitempty" jsonschema:"unit or syslog identifier (Linux), provider (Windows)"`
	PID      int    `json:"pid,omitempty"`
	EventID  int    `json:"event_id,omitempty"`
	Message  string `json:"message"`
}

// LogQueryOutput is the output of log_query.
type LogQueryOutput struct {
	Entries   []LogEntry `json:"entries"`
	Truncated bool       `json:"truncated,omitempty" jsonschema:"messages were shortened to fit the output limit"`
}

func (d *Deps) logQuery(ctx context.Context, _ *mcp.CallToolRequest, in LogQueryInput) (*mcp.CallToolResult, LogQueryOutput, error) {
	in.Lines = min(orDefaultInt(in.Lines, 100), 2000)
	entries, err := queryLog(ctx, d, in)
	if err != nil {
		return nil, LogQueryOutput{}, err
	}
	out := LogQueryOutput{Entries: entries}
	if out.Entries == nil {
		out.Entries = []LogEntry{}
	}
	// Beyond the output limit, drop the oldest entries.
	total := 0
	for i := len(out.Entries) - 1; i >= 0; i-- {
		total += len(out.Entries[i].Message) + 100
		if total > d.Cfg.MaxOutputBytes {
			out.Entries = out.Entries[i+1:]
			out.Truncated = true
			break
		}
	}
	return nil, out, nil
}

// parseSince parses an elapsed time such as "30m", "2h" or "1d", or a timestamp.
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

// ---- sys_disks ----

// SysDisksInput is the input of sys_disks.
type SysDisksInput struct {
	All bool `json:"all,omitempty" jsonschema:"Linux: include pseudo file systems (proc, sysfs, cgroup, ...)"`
}

// Disk is the usage of a file system.
type Disk struct {
	Mount       string  `json:"mount"`
	Device      string  `json:"device,omitempty"`
	Type        string  `json:"type,omitempty"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes" jsonschema:"space available to the current user"`
	UsedPercent float64 `json:"used_percent"`
	Total       string  `json:"total" jsonschema:"human-readable total"`
	Free        string  `json:"free" jsonschema:"human-readable free space"`
}

// SysDisksOutput is the output of sys_disks.
type SysDisksOutput struct {
	Disks []Disk `json:"disks"`
}

func (d *Deps) sysDisks(_ context.Context, _ *mcp.CallToolRequest, in SysDisksInput) (*mcp.CallToolResult, SysDisksOutput, error) {
	disks, err := listDisks(in.All)
	if err != nil {
		return nil, SysDisksOutput{}, err
	}
	for i := range disks {
		dk := &disks[i]
		if dk.TotalBytes > 0 {
			dk.UsedPercent = float64(int(float64(dk.UsedBytes)/float64(dk.TotalBytes)*1000+0.5)) / 10
		}
		dk.Total, dk.Free = humanBytes(dk.TotalBytes), humanBytes(dk.FreeBytes)
	}
	if disks == nil {
		disks = []Disk{}
	}
	return nil, SysDisksOutput{Disks: disks}, nil
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
