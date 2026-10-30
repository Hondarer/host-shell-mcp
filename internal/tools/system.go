package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// SudoRunInput is the input of sudo_run.
type SudoRunInput struct {
	Command    string `json:"command" jsonschema:"shell command line run via sudo sh -c"`
	User       string `json:"user,omitempty" jsonschema:"target user (default: root)"`
	Cwd        string `json:"cwd,omitempty" jsonschema:"working directory"`
	Stdin      string `json:"stdin,omitempty" jsonschema:"text written to command stdin"`
	Password   string `json:"password,omitempty" jsonschema:"sudo password provided by the user (passed to sudo only via askpass, never to the command or logs)"`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds"`
	IOEncoding
}

// AuditArgs redacts Stdin and Password for audit logging.
func (in SudoRunInput) AuditArgs() any {
	in.Stdin = redactedLen(in.Stdin)
	if in.Password != "" {
		in.Password = "<redacted>"
	}
	return in
}

// sudoRun runs a command via sudo: using sudo -n when Password is empty, or sudo -A with the
// built-in askpass helper (see Askpass) so the password is never exposed to the command's stdin.
func (d *Deps) sudoRun(ctx context.Context, req *mcp.CallToolRequest, in SudoRunInput) (*mcp.CallToolResult, proc.Result, error) {
	if in.Command == "" {
		return nil, proc.Result{}, errors.New("command is required")
	}
	argv := []string{"sudo", "-n"}
	var env map[string]string
	if in.Password != "" {
		var cleanup func()
		var err error
		if env, cleanup, err = withAskpass(in.Password); err != nil {
			return nil, proc.Result{}, fmt.Errorf("prepare the password for sudo: %w", err)
		}
		defer cleanup()
		argv = []string{"sudo", "-A"}
	}
	if in.User != "" {
		argv = append(argv, "-u", in.User)
	}
	argv = append(argv, "--", "sh", "-c", in.Command)
	output, stdin, err := in.resolve(in.Stdin, textenc.UTF8)
	if err != nil {
		return nil, proc.Result{}, err
	}
	tap, stop := watchOutput(newProgress(ctx, req))
	defer stop()
	res, err := proc.Run(ctx, d.procSpec(CommandSpec{Argv: argv, Cwd: in.Cwd, Env: env}), proc.Options{
		Stdin:     stdin,
		Output:    output,
		Timeout:   d.Cfg.ClampTimeout(in.TimeoutSec),
		MaxOutput: d.Cfg.MaxOutputBytes,
		OnOutput:  tap,
	})
	if in.Password != "" {
		// Whatever --redact says, the password never appears in the result.
		res.Stdout = strings.ReplaceAll(res.Stdout, in.Password, "[REDACTED]")
		res.Stderr = strings.ReplaceAll(res.Stderr, in.Password, "[REDACTED]")
	}
	return nil, res, err
}

// PathArgs passes the working directory to the policy check as a read target.
func (in SudoRunInput) PathArgs() (read, write []string) {
	return CommandSpec{Cwd: in.Cwd}.PathArgs()
}

// CommandArgs passes sudo and the shell sudo starts to the policy check.
func (in SudoRunInput) CommandArgs(string) (programs []string, text string) {
	return []string{"sudo", "sh"}, in.Command
}

// SysInfoOutput is the output of sys_info.
type SysInfoOutput struct {
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Hostname     string   `json:"hostname"`
	User         string   `json:"user"`
	UID          string   `json:"uid"`
	Home         string   `json:"home"`
	Cwd          string   `json:"cwd"`
	PathSep      string   `json:"path_separator"`
	DefaultShell string   `json:"default_shell"`
	ConsoleEnc   string   `json:"console_encoding" jsonschema:"default command I/O encoding (system for output_encoding/stdin_encoding)"`
	FileEnc      string   `json:"file_encoding" jsonschema:"default text file encoding (system for charset)"`
	NumCPU       int      `json:"num_cpu"`
	PID          int      `json:"pid"`
	Executable   string   `json:"executable"`
	Version      string   `json:"version" jsonschema:"host-shell version"`
	GoVersion    string   `json:"go_version"`
	ToolGroups   []string `json:"tool_groups" jsonschema:"enabled tool groups in this session (see help)"`
	Restrictions []string `json:"restrictions,omitempty" jsonschema:"active policy restrictions configured on this server"`
}

func (d *Deps) sysInfo(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, SysInfoOutput, error) {
	out := SysInfoOutput{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		PathSep:      string(os.PathSeparator),
		DefaultShell: d.DefaultShell,
		ConsoleEnc:   textenc.Console().Name,
		FileEnc:      textenc.File().Name,
		NumCPU:       runtime.NumCPU(),
		PID:          os.Getpid(),
		Version:      version.Version,
		GoVersion:    runtime.Version(),
	}
	out.Hostname, _ = os.Hostname()
	out.Cwd, _ = os.Getwd()
	out.Home, _ = os.UserHomeDir()
	out.Executable, _ = os.Executable()
	if u, err := user.Current(); err == nil {
		out.User, out.UID = u.Username, u.Uid
	}
	out.ToolGroups = d.enabledGroups()
	for _, kv := range d.Policy.Summary() {
		out.Restrictions = append(out.Restrictions, kv[0]+": "+kv[1])
	}
	return nil, out, nil
}

// enabledGroups returns the names of the groups with exposed tools.
func (d *Deps) enabledGroups() []string {
	var out []string
	for _, g := range Groups {
		if d.groupEnabled(g[0]) {
			out = append(out, g[0])
		}
	}
	return out
}

// SysProcessesInput is the input of sys_processes.
type SysProcessesInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"case-insensitive name or command line substring"`
	PID    int    `json:"pid,omitempty" jsonschema:"root pid (returns this process and its descendants)"`
	Max    int    `json:"max,omitempty" jsonschema:"max processes to return (default 500)"`
}

// Process describes one running process. Fields unavailable on the OS or inaccessible due to permissions are omitted.
type Process struct {
	PID         int    `json:"pid"`
	PPID        int    `json:"ppid"`
	Name        string `json:"name"`
	CommandLine string `json:"command_line,omitempty"`
	User        string `json:"user,omitempty"`
	State       string `json:"state,omitempty" jsonschema:"Linux: R running, S sleeping, D uninterruptible sleep, Z zombie, T stopped, ..."`
	StartTime   string `json:"start_time,omitempty" jsonschema:"RFC 3339"`
	MemoryBytes uint64 `json:"memory_bytes,omitempty" jsonschema:"resident set size (working set on Windows)"`
}

// SysProcessesOutput is the output of sys_processes.
type SysProcessesOutput struct {
	Processes []Process `json:"processes"`
	Total     int       `json:"total" jsonschema:"matching processes before max limit"`
	Truncated bool      `json:"truncated,omitempty" jsonschema:"stopped at max"`
}

// sysProcesses lists processes from /proc on Linux and the Windows API on Windows, ordered by pid.
func (d *Deps) sysProcesses(_ context.Context, _ *mcp.CallToolRequest, in SysProcessesInput) (*mcp.CallToolResult, SysProcessesOutput, error) {
	all, err := listProcesses()
	if err != nil {
		return nil, SysProcessesOutput{}, err
	}
	slices.SortFunc(all, func(a, b Process) int { return a.PID - b.PID })
	if in.PID > 0 {
		keep := map[int]bool{in.PID: true}
		for changed := true; changed; { // parents come before children only by chance, so repeat
			changed = false
			for _, p := range all {
				if !keep[p.PID] && keep[p.PPID] && p.PID != p.PPID {
					keep[p.PID], changed = true, true
				}
			}
		}
		all = slices.DeleteFunc(all, func(p Process) bool { return !keep[p.PID] })
	}
	if in.Filter != "" {
		f := strings.ToLower(in.Filter)
		all = slices.DeleteFunc(all, func(p Process) bool {
			return !strings.Contains(strings.ToLower(p.Name), f) && !strings.Contains(strings.ToLower(p.CommandLine), f)
		})
	}
	out := SysProcessesOutput{Total: len(all)}
	if max := orDefaultInt(in.Max, 500); len(all) > max {
		all, out.Truncated = all[:max], true
	}
	out.Processes = all
	return nil, out, nil
}
