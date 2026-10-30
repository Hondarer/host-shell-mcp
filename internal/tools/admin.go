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
		Name:  "proc_list",
		Title: "List processes",
		Description: "List running processes with pid, ppid, name, command line, user, state, start time and memory, ordered by pid. " +
			"filter matches name or command line by substring; pid keeps one process and its descendants. Use proc_kill to end a process.",
		Annotations: readOnly(),
	}, d.procList)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "proc_kill",
		Title: "Signal or kill process",
		Description: "Send a signal to a process by pid (Linux: terminate by default, kill, interrupt, hangup, ...; Windows: kill). " +
			"tree includes all descendants; sudo sends as root via sudo -n on Linux. Use job_stop for jobs started by host-shell.",
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
		Description: "Query or control a service: status (details and recent log lines), start, stop, restart, reload (Linux), enable, disable. " +
			"Uses systemctl on Linux (sudo for system units when not root, user for --user units) and Service Control Manager on Windows (needs elevation for most changes). " +
			"Returns post-action status.",
		Annotations: destructive(),
	}, d.svcControl)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:  "log_query",
		Title: "Query system logs",
		Description: "Read structured system log entries: systemd journal on Linux (unit, priority), Windows Event Log on Windows (log, unit as provider, priority, event_id). " +
			"since accepts durations (30m, 2h, 1d) or timestamps; grep filters messages by regex. Oldest first. " +
			"For plain log files use fs_read or search_grep.",
		Annotations: readOnly(),
	}, d.logQuery)
	add(s, d, groupAdmin, &mcp.Tool{
		Name:        "disk_list",
		Title:       "Disk usage",
		Description: "List mounted file systems (Linux) or drives (Windows) with type, total, used and free space. all includes pseudo file systems.",
		Annotations: readOnly(),
	}, d.diskList)

	if d.onOS("windows") {
		add(s, d, groupAdmin, &mcp.Tool{
			Name:  "reg_read",
			Title: "Read registry",
			Description: "Read a Windows registry key (subkeys and values, or one value via value). Keys use roots HKLM, HKCU, HKCR, HKU, HKCC " +
				"(e.g. HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion). view 32 or 64 selects the WOW64 view.",
			Annotations: readOnly(),
		}, d.regRead)
		add(s, d, groupAdmin, &mcp.Tool{
			Name:  "reg_write",
			Title: "Write registry",
			Description: "Create or set a Windows registry value (creating the key if missing), or omit value to only create the key. " +
				"type: REG_SZ (default), REG_EXPAND_SZ, REG_MULTI_SZ (string array), REG_DWORD, REG_QWORD (number), REG_BINARY (base64).",
			Annotations: destructive(),
		}, d.regWrite)
		add(s, d, groupAdmin, &mcp.Tool{
			Name:        "reg_delete",
			Title:       "Delete registry",
			Description: "Delete a Windows registry value (with value) or key (recursive deletes subkeys).",
			Annotations: destructive(),
		}, d.regDelete)
	}
}

// ---- proc_kill ----

// ProcKillInput is the input of proc_kill.
type ProcKillInput struct {
	PID    int    `json:"pid" jsonschema:"process id (see proc_list)"`
	Signal string `json:"signal,omitempty" jsonschema:"Linux: terminate (default), kill, interrupt, hangup, quit, stop, continue, usr1, usr2. Windows: kill (default)"`
	Tree   bool   `json:"tree,omitempty" jsonschema:"also signal all descendant processes"`
	Sudo   bool   `json:"sudo,omitempty" jsonschema:"Linux: send as root via sudo -n (for other users' processes; if sudo needs a password, use exec_sudo)"`
}

// ProcKillOutput is the output of proc_kill.
type ProcKillOutput struct {
	Signaled []int `json:"signaled" jsonschema:"process ids signaled"`
	Exited   bool  `json:"exited" jsonschema:"process exited within 3 seconds"`
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
	Filter string `json:"filter,omitempty" jsonschema:"case-insensitive name or description substring"`
	State  string `json:"state,omitempty" jsonschema:"running, stopped, failed or all (default)"`
	User   bool   `json:"user,omitempty" jsonschema:"Linux: list user systemd units (systemctl --user)"`
}

// Service is the state of a service.
type Service struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	State       string `json:"state" jsonschema:"running, stopped, failed, starting, stopping or native state"`
	SubState    string `json:"sub_state,omitempty" jsonschema:"Linux: systemd sub-state (running, exited, dead, ...)"`
	StartMode   string `json:"start_mode,omitempty" jsonschema:"Linux: enabled, disabled, static, masked, ...; Windows: auto, delayed-auto, manual, disabled"`
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
	Name    string `json:"name" jsonschema:"service name: systemd unit (nginx or nginx.service) or Windows service name (not display name)"`
	Action  string `json:"action,omitempty" jsonschema:"status (default), start, stop, restart, reload (Linux), enable, disable"`
	User    bool   `json:"user,omitempty" jsonschema:"Linux: user unit (systemctl --user)"`
	Sudo    bool   `json:"sudo,omitempty" jsonschema:"Linux: run systemctl via sudo -n (needed for system units when not root; if sudo needs a password, use exec_sudo)"`
	WaitSec int    `json:"wait_sec,omitempty" jsonschema:"Windows: seconds to wait for start/stop (default 30)"`
}

// SvcControlOutput is the output of svc_control.
type SvcControlOutput struct {
	Service    Service           `json:"service"`
	Properties map[string]string `json:"properties,omitempty" jsonschema:"unit or service properties (executable path, account, main pid, ...)"`
	StatusText string            `json:"status_text,omitempty" jsonschema:"Linux: systemctl status output with recent log lines"`
	Output     string            `json:"output,omitempty" jsonschema:"control command output"`
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
	Log      string `json:"log,omitempty" jsonschema:"Windows: event log channel: System (default), Application, Security, Setup or a full channel name. Linux: kernel for kernel messages only"`
	Since    string `json:"since,omitempty" jsonschema:"entries newer than a relative duration (30m, 2h, 1d) or timestamp (2026-10-30 09:00, RFC3339)"`
	Until    string `json:"until,omitempty" jsonschema:"entries older than this (same formats as since)"`
	Priority string `json:"priority,omitempty" jsonschema:"minimum severity: Linux emerg, alert, crit, err, warning, notice, info, debug; Windows critical, error, warning, information, verbose"`
	EventID  int    `json:"event_id,omitempty" jsonschema:"Windows: filter by event id"`
	Grep     string `json:"grep,omitempty" jsonschema:"case-insensitive regex to match message"`
	Lines    int    `json:"lines,omitempty" jsonschema:"max entries, newest kept (default 100, max 2000)"`
	User     bool   `json:"user,omitempty" jsonschema:"Linux: user journal (journalctl --user)"`
	Sudo     bool   `json:"sudo,omitempty" jsonschema:"Linux: run journalctl via sudo -n (to read system journal without journal group; if sudo needs a password, use exec_sudo)"`
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
	Truncated bool       `json:"truncated,omitempty" jsonschema:"oldest entries were dropped to fit the output limit"`
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
	// Drop oldest entries when exceeding MaxOutputBytes.
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

// ---- disk_list ----

// DiskListInput is the input of disk_list.
type DiskListInput struct {
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

// DiskListOutput is the output of disk_list.
type DiskListOutput struct {
	Disks []Disk `json:"disks"`
}

func (d *Deps) diskList(_ context.Context, _ *mcp.CallToolRequest, in DiskListInput) (*mcp.CallToolResult, DiskListOutput, error) {
	disks, err := listDisks(in.All)
	if err != nil {
		return nil, DiskListOutput{}, err
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
	return nil, DiskListOutput{Disks: disks}, nil
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
