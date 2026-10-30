// Package proc starts child processes, hiding the differences between Windows and Linux.
//
// It is essential not to give child processes host-shell's own standard input (the MCP channel)
// as their standard input, nor host-shell's own standard output as their standard output.
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

// Spec specifies the command to start. Set exactly one of Command and Argv.
type Spec struct {
	// Command is the command line passed to the shell.
	Command string
	// Argv is the argument list started without a shell. Argv[0] is the program.
	Argv []string
	// Shell interprets Command. Empty means the OS default.
	Shell string
	// Dir is the working directory.
	Dir string
	// Env adds to or overrides the inherited environment variables.
	Env map[string]string
	// EnvFilter narrows the environment before it is passed to the child (user restrictions). nil means no filtering.
	EnvFilter func([]string) []string
	// Interactive means the command is used interactively on a terminal. PowerShell then gets no -NonInteractive.
	Interactive bool
}

// waitDelay is how long to wait when grandchildren still hold the pipes after the process exits.
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

// Options control how Run behaves.
type Options struct {
	DefaultShell string
	// Stdin is the bytes passed to standard input. The caller has already converted the encoding.
	Stdin string
	// Output is the encoding of standard output and standard error. The zero value is UTF-8 (no conversion).
	Output    textenc.Choice
	Timeout   time.Duration
	MaxOutput int
	// OnOutput receives the bytes written to standard output and standard error (before conversion). Used for progress notifications.
	OnOutput func([]byte)
}

// Run runs the command, waits for it to end and returns the result.
// A non-zero exit code is not an error; an error is returned only when the command could not start.
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

// powershellArgs passes the script with -EncodedCommand to avoid differences in quote handling.
//
// Before the script, output is set to UTF-8. (Setting [Console]::InputEncoding there has no effect on
// $input, which PowerShell sets up before the script runs; the tools encode stdin for PowerShell in
// the console code page instead.) After the script, the exit code is taken from the
// last statement, as with sh -c: 0 when it succeeded ($? is true), otherwise $LASTEXITCODE when a
// native command set one, otherwise 1. A script that ends with exit N, or stops on a terminating
// error, never reaches that part.
func powershellArgs(script string, interactive bool) []string {
	script = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" +
		script +
		"\n$hostShellOk = $?; if ($hostShellOk) { exit 0 }; if ($LASTEXITCODE) { exit $LASTEXITCODE }; exit 1"
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)}
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
