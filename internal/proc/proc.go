// Package proc spawns and manages child processes across Linux and Windows.
//
// Child processes must never inherit host-shell's own stdin or stdout, which carry MCP JSON-RPC.
package proc

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/hondarer/host-shell-mcp/internal/limit"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// Spec describes a command to start. Exactly one of Command or Argv must be set.
type Spec struct {
	// Command is a command line passed to Shell.
	Command string
	// Argv is a program and arguments executed directly without a shell.
	Argv []string
	// Shell interprets Command (empty uses the OS default).
	Shell string
	// Dir is the working directory.
	Dir string
	// Env adds or overrides environment variables.
	Env map[string]string
	// EnvFilter filters inherited environment variables before applying Env (nil means no filtering).
	EnvFilter func([]string) []string
	// Interactive indicates an interactive terminal session (omits PowerShell's -NonInteractive).
	Interactive bool
}

// waitDelay bounds how long to wait for pipes held open by grandchildren after process exit.
const waitDelay = 2 * time.Second

// Command builds an exec.Cmd from spec. The caller sets up standard input and output.
func Command(ctx context.Context, spec Spec, defaultShell string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch {
	case len(spec.Argv) > 0 && spec.Command != "":
		return nil, errors.New("command and argv are mutually exclusive")
	case len(spec.Argv) > 0:
		cmd = exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	case spec.Command != "":
		sh := spec.Shell
		if sh == "" {
			sh = defaultShell
		}
		var err error
		if cmd, err = shellCommand(ctx, sh, spec.Command, spec.Interactive); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("either command or argv is required")
	}
	cmd.Dir = spec.Dir
	if spec.EnvFilter != nil {
		cmd.Env = spec.EnvFilter(os.Environ())
	}
	if len(spec.Env) > 0 {
		base := cmd.Env
		if base == nil {
			base = os.Environ()
		}
		cmd.Env = mergeEnv(base, spec.Env)
	}
	setupProcess(cmd)
	cmd.WaitDelay = waitDelay
	return cmd, nil
}

// Result is the result of Run.
type Result struct {
	ExitCode        int    `json:"exit_code" jsonschema:"process exit code (-1 if killed or unknown)"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
	StdoutEncoding  string `json:"stdout_encoding,omitempty" jsonschema:"set when stdout was converted to UTF-8 from this encoding"`
	StderrEncoding  string `json:"stderr_encoding,omitempty" jsonschema:"set when stderr was converted to UTF-8 from this encoding"`
	TimedOut        bool   `json:"timed_out,omitempty"`
	Cancelled       bool   `json:"cancelled,omitempty" jsonschema:"the client cancelled the request and the process tree was killed"`
	DurationMs      int64  `json:"duration_ms"`
}

// Options configures Run.
type Options struct {
	DefaultShell string
	// Stdin is pre-encoded standard input.
	Stdin string
	// Output is the stdout/stderr encoding (zero value means UTF-8 without conversion).
	Output    textenc.Choice
	Timeout   time.Duration
	MaxOutput int
	// OnOutput receives raw stdout/stderr bytes before encoding conversion (used for progress notifications).
	OnOutput func([]byte)
}

// Run executes spec to completion and returns its captured output and exit status.
// Non-zero exit codes are reported in Result.ExitCode; an error is returned only if the process fails to start.
func Run(parent context.Context, spec Spec, opt Options) (Result, error) {
	ctx, cancel := context.WithTimeout(parent, opt.Timeout)
	defer cancel()

	cmd, err := Command(ctx, spec, opt.DefaultShell)
	if err != nil {
		return Result{}, err
	}
	stdoutBuf, stderrBuf := limit.NewBuffer(opt.MaxOutput), limit.NewBuffer(opt.MaxOutput)
	stdout, stderr := textenc.NewWriter(stdoutBuf, opt.Output), textenc.NewWriter(stderrBuf, opt.Output)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if opt.OnOutput != nil {
		tap := writerFunc(func(p []byte) (int, error) { opt.OnOutput(p); return len(p), nil })
		cmd.Stdout, cmd.Stderr = io.MultiWriter(stdout, tap), io.MultiWriter(stderr, tap)
	}
	if opt.Stdin != "" {
		cmd.Stdin = strings.NewReader(opt.Stdin)
	}

	start := time.Now()
	runErr := cmd.Run()
	stdout.Close()
	stderr.Close()
	res := Result{
		Stdout:          stdoutBuf.String(),
		Stderr:          stderrBuf.String(),
		StdoutTruncated: stdoutBuf.Truncated(),
		StderrTruncated: stderrBuf.Truncated(),
		StdoutEncoding:  stdout.Converted(),
		StderrEncoding:  stderr.Converted(),
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil,
		Cancelled:       errors.Is(parent.Err(), context.Canceled),
		DurationMs:      time.Since(start).Milliseconds(),
	}
	if cmd.ProcessState == nil {
		return res, runErr
	}
	res.ExitCode = cmd.ProcessState.ExitCode()
	return res, nil
}

// DefaultShell returns the shell used on this OS when shell is omitted.
func DefaultShell(windowsShell string) string {
	if runtime.GOOS == "windows" {
		return windowsShell
	}
	return "sh"
}

// powershellArgs passes script, base64-encoded, to a short loader given with -Command, which avoids
// cmd/SSH quoting issues. -EncodedCommand is not used: Windows PowerShell 5.1 then writes errors,
// warnings and progress to stderr as CLIXML, even with -OutputFormat Text. With -Command, errors are
// plain text and warnings go to stdout. The loader pipes $input into the script and dot-sources it,
// so the script sees stdin and runs in the global scope as with -EncodedCommand.
//
// Before script, it hides progress, turns off PowerShell 7's ANSI colors and sets
// [Console]::OutputEncoding to UTF-8. (Setting the input encoding there has no effect, as PowerShell
// initializes $input before running the script; see UTF8Console.) Afterward, it maps the last
// statement's status to an sh-compatible exit code: 0 if $? is true, $LASTEXITCODE if non-zero, or 1
// (scripts that call exit N or throw a terminating error exit earlier).
func powershellArgs(script string, interactive bool) []string {
	script = "$ProgressPreference = 'SilentlyContinue'\n" +
		"if ($PSStyle) { $PSStyle.OutputRendering = 'PlainText' }\n" +
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" +
		script +
		"\n$hostShellOk = $?; if ($hostShellOk) { exit 0 }; if ($LASTEXITCODE) { exit $LASTEXITCODE }; exit 1"
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	loader := "$input | . ([ScriptBlock]::Create([Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('" +
		base64.StdEncoding.EncodeToString(b) + "'))))"
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-OutputFormat", "Text", "-Command", loader}
	if interactive {
		args = slices.Delete(args, 1, 2)
	}
	return args
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func mergeEnv(base []string, add map[string]string) []string {
	out := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !hasKey(add, k) {
			out = append(out, kv)
		}
	}
	for k, v := range add {
		out = append(out, k+"="+v)
	}
	return out
}

func hasKey(m map[string]string, key string) bool {
	for k := range m {
		// Windows environment variable names are case-insensitive.
		if k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key)) {
			return true
		}
	}
	return false
}
