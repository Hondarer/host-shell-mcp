package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/limit"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// SudoRunInput is the input of sudo_run.
type SudoRunInput struct {
	Command    string `json:"command" jsonschema:"shell command line run as sh -c under sudo"`
	User       string `json:"user,omitempty" jsonschema:"target user (default: root)"`
	Cwd        string `json:"cwd,omitempty" jsonschema:"working directory"`
	Stdin      string `json:"stdin,omitempty" jsonschema:"text passed to standard input of the command"`
	Password   string `json:"password,omitempty" jsonschema:"the user's sudo password, only when the user gave it to you and sudo asks for one. It is handed to sudo only if sudo asks, never to the command, and is not logged"`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds"`
	IOEncoding
}

// AuditArgs keeps the content of standard input and the password out of the log.
func (in SudoRunInput) AuditArgs() any {
	in.Stdin = redactedLen(in.Stdin)
	if in.Password != "" {
		in.Password = "<redacted>"
	}
	return in
}

// sudoRun runs a command with sudo. Without a password it uses sudo -n (non-interactive).
// With one it uses sudo -A, with host-shell itself as the askpass helper (see Askpass), so the
// password reaches sudo only when sudo asks for it and never the command's standard input.
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
	ConsoleEnc   string   `json:"console_encoding" jsonschema:"encoding used for command input/output by default (system for output_encoding/stdin_encoding)"`
	FileEnc      string   `json:"file_encoding" jsonschema:"encoding used for text files by default (system for charset)"`
	NumCPU       int      `json:"num_cpu"`
	PID          int      `json:"pid"`
	Executable   string   `json:"executable"`
	Version      string   `json:"version" jsonschema:"host-shell version"`
	GoVersion    string   `json:"go_version"`
	ToolGroups   []string `json:"tool_groups" jsonschema:"tool groups available in this session (see help)"`
	Restrictions []string `json:"restrictions,omitempty" jsonschema:"restrictions set by the administrator of this host-shell; operations outside them fail"`
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
	Filter string `json:"filter,omitempty" jsonschema:"case-insensitive substring; only matching lines (plus the header) are returned"`
}

// SysProcessesOutput is the output of sys_processes.
type SysProcessesOutput struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// sysProcesses lists processes with the OS's standard commands.
// TODO: read them directly from /proc or the Windows API and return a structured list.
func (d *Deps) sysProcesses(ctx context.Context, _ *mcp.CallToolRequest, in SysProcessesInput) (*mcp.CallToolResult, SysProcessesOutput, error) {
	argv := []string{"ps", "-eo", "pid,ppid,user,stat,etime,args"}
	if runtime.GOOS == "windows" {
		argv = []string{"tasklist", "/fo", "csv"}
	}
	// Fetch with a generous limit here so that nothing is truncated before filtering.
	res, err := proc.Run(ctx, proc.Spec{Argv: argv}, proc.Options{
		Output:    textenc.Choice{Auto: true, Enc: textenc.Console()},
		Timeout:   d.Cfg.DefaultTimeout,
		MaxOutput: 16 << 20,
	})
	if err != nil {
		return nil, SysProcessesOutput{}, err
	}
	if res.ExitCode != 0 {
		return nil, SysProcessesOutput{}, errors.New(strings.TrimSpace(res.Stderr))
	}
	text := res.Stdout
	if in.Filter != "" {
		text = filterLines(text, in.Filter)
	}
	var out SysProcessesOutput
	out.Text, out.Truncated = limit.Text(text, d.Cfg.MaxOutputBytes)
	return nil, out, nil
}

func filterLines(text, sub string) string {
	lines := strings.SplitAfter(text, "\n")
	sub = strings.ToLower(sub)
	var sb strings.Builder
	for i, l := range lines {
		if i == 0 || strings.Contains(strings.ToLower(l), sub) {
			sb.WriteString(l)
		}
	}
	return sb.String()
}
