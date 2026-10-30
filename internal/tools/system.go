package tools

import (
	"context"
	"errors"
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

// SudoRunInput は sudo_run の入力です。
type SudoRunInput struct {
	Command    string `json:"command" jsonschema:"shell command line run as sh -c under sudo"`
	User       string `json:"user,omitempty" jsonschema:"target user (default: root)"`
	Cwd        string `json:"cwd,omitempty" jsonschema:"working directory"`
	Stdin      string `json:"stdin,omitempty" jsonschema:"text passed to standard input of the command"`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds"`
	IOEncoding
}

// AuditArgs は標準入力の内容をログに残さないようにします。
func (in SudoRunInput) AuditArgs() any {
	in.Stdin = redactedLen(in.Stdin)
	return in
}

// sudoRun は sudo -n (対話なし) でコマンドを実行します。
// TODO(未決事項): パスワードを標準入力から渡す sudo -S に対応するか。
// 対応する場合は、パスワードがツールの引数や操作ログに残らない受け渡し方法を設計する。
func (d *Deps) sudoRun(ctx context.Context, _ *mcp.CallToolRequest, in SudoRunInput) (*mcp.CallToolResult, proc.Result, error) {
	if in.Command == "" {
		return nil, proc.Result{}, errors.New("command is required")
	}
	argv := []string{"sudo", "-n"}
	if in.User != "" {
		argv = append(argv, "-u", in.User)
	}
	argv = append(argv, "--", "sh", "-c", in.Command)
	output, stdin, err := in.resolve(in.Stdin)
	if err != nil {
		return nil, proc.Result{}, err
	}
	res, err := proc.Run(ctx, proc.Spec{Argv: argv, Dir: in.Cwd}, proc.Options{
		Stdin:     stdin,
		Output:    output,
		Timeout:   d.Cfg.ClampTimeout(in.TimeoutSec),
		MaxOutput: d.Cfg.MaxOutputBytes,
	})
	return nil, res, err
}

// SysInfoOutput は sys_info の出力です。
type SysInfoOutput struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Hostname     string `json:"hostname"`
	User         string `json:"user"`
	UID          string `json:"uid"`
	Home         string `json:"home"`
	Cwd          string `json:"cwd"`
	PathSep      string `json:"path_separator"`
	DefaultShell string `json:"default_shell"`
	ConsoleEnc   string `json:"console_encoding" jsonschema:"encoding used for command input/output by default (system for output_encoding/stdin_encoding)"`
	FileEnc      string `json:"file_encoding" jsonschema:"encoding used for text files by default (system for charset)"`
	NumCPU       int    `json:"num_cpu"`
	PID          int    `json:"pid"`
	Executable   string `json:"executable"`
	Version      string `json:"version" jsonschema:"host-shell version"`
	GoVersion    string `json:"go_version"`
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
	return nil, out, nil
}

// SysProcessesInput は sys_processes の入力です。
type SysProcessesInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"case-insensitive substring; only matching lines (plus the header) are returned"`
}

// SysProcessesOutput は sys_processes の出力です。
type SysProcessesOutput struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// sysProcesses は OS 標準のコマンドでプロセス一覧を取得します。
// TODO: /proc や Windows API から直接取得し、構造化した一覧を返す。
func (d *Deps) sysProcesses(ctx context.Context, _ *mcp.CallToolRequest, in SysProcessesInput) (*mcp.CallToolResult, SysProcessesOutput, error) {
	argv := []string{"ps", "-eo", "pid,ppid,user,stat,etime,args"}
	if runtime.GOOS == "windows" {
		argv = []string{"tasklist", "/fo", "csv"}
	}
	// フィルターの前に切り詰めないよう、ここでは大きめの上限で取得する。
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
