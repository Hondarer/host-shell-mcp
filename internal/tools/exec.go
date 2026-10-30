package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// CommandSpec is the input shared by command execution tools.
type CommandSpec struct {
	Command    string            `json:"command,omitempty" jsonschema:"shell command line (supports pipes, redirects, &&); set either command or argv"`
	Argv       []string          `json:"argv,omitempty" jsonschema:"program and arguments run directly without a shell, e.g. [\"git\", \"log\", \"-n\", \"5\"]"`
	Shell      string            `json:"shell,omitempty" jsonschema:"shell for command: sh, bash or pwsh on Linux; cmd, powershell or pwsh on Windows (default: default_shell in sys_info)"`
	Cwd        string            `json:"cwd,omitempty" jsonschema:"working directory (default: server startup directory; does not persist across calls)"`
	Env        map[string]string `json:"env,omitempty" jsonschema:"environment variables to set or override"`
	Stdin      string            `json:"stdin,omitempty" jsonschema:"text written to stdin before closing it (empty by default so prompts fail instead of hanging)"`
	TimeoutSec int               `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds; kills the process tree on expiry"`
	IOEncoding
}

// IOEncoding specifies the encoding of command input and output.
type IOEncoding struct {
	OutputEncoding string `json:"output_encoding,omitempty" jsonschema:"stdout/stderr encoding: auto (default: per-line UTF-8 if valid, else console code page), utf-8, system, or a charset name (shift_jis, euc-jp, utf-16le, ...)"`
	StdinEncoding  string `json:"stdin_encoding,omitempty" jsonschema:"stdin encoding: utf-8 (default; system for Windows PowerShell), system, or a charset name"`
}

// resolve parses the output encoding choice and encodes stdin (using def when StdinEncoding is empty).
func (e IOEncoding) resolve(stdin string, def textenc.Encoding) (textenc.Choice, string, error) {
	out, err := textenc.Parse(e.OutputEncoding, textenc.Console, true, textenc.UTF8)
	if err != nil {
		return textenc.Choice{}, "", fmt.Errorf("output_encoding: %w", err)
	}
	in, err := textenc.Parse(e.StdinEncoding, textenc.Console, false, def)
	if err != nil {
		return textenc.Choice{}, "", fmt.Errorf("stdin_encoding: %w", err)
	}
	b, err := in.Enc.Encode(stdin)
	if err != nil {
		return textenc.Choice{}, "", fmt.Errorf("stdin: %w (set stdin_encoding to an encoding the command reads, e.g. utf-8)", err)
	}
	return out, string(b), nil
}

// stdinDefault returns the default stdin encoding: console code page for non-PTY Windows PowerShell
// (which reads $input in the console code page), and UTF-8 otherwise.
func (d *Deps) stdinDefault(c CommandSpec, pty bool) textenc.Encoding {
	if runtime.GOOS != "windows" || pty || len(c.Argv) > 0 {
		return textenc.UTF8
	}
	switch orDefault(c.Shell, d.DefaultShell) {
	case "powershell", "pwsh":
		return textenc.Console()
	}
	return textenc.UTF8
}

// procSpec builds a proc.Spec with policy environment filtering applied.
func (d *Deps) procSpec(c CommandSpec) proc.Spec {
	spec := proc.Spec{Command: c.Command, Argv: c.Argv, Shell: c.Shell, Dir: c.Cwd, Env: c.Env}
	if d.Policy.FiltersEnv() {
		spec.EnvFilter = d.Policy.FilterEnv
	}
	return spec
}

// PathArgs returns Cwd as a read path for policy checks.
func (c CommandSpec) PathArgs() (read, write []string) {
	if c.Cwd != "" {
		read = []string{c.Cwd}
	}
	return read, nil
}

// CommandArgs returns the invoked program and command string for policy checks.
func (c CommandSpec) CommandArgs(defaultShell string) (programs []string, text string) {
	if len(c.Argv) > 0 {
		return []string{c.Argv[0]}, strings.Join(c.Argv, " ")
	}
	return []string{orDefault(c.Shell, defaultShell)}, c.Command
}

// AuditArgs redacts Stdin for audit logging.
func (c CommandSpec) AuditArgs() any {
	c.Stdin = redactedLen(c.Stdin)
	return c
}

func (d *Deps) execRun(ctx context.Context, req *mcp.CallToolRequest, in CommandSpec) (*mcp.CallToolResult, proc.Result, error) {
	output, stdin, err := in.resolve(in.Stdin, d.stdinDefault(in, false))
	if err != nil {
		return nil, proc.Result{}, err
	}
	tap, stop := watchOutput(newProgress(ctx, req))
	defer stop()
	res, err := proc.Run(ctx, d.procSpec(in), proc.Options{
		DefaultShell: d.DefaultShell,
		Stdin:        stdin,
		Output:       output,
		Timeout:      d.Cfg.ClampTimeout(in.TimeoutSec),
		MaxOutput:    d.Cfg.MaxOutputBytes,
		OnOutput:     tap,
	})
	return nil, res, err
}

// JobStartInput is the input of job_start.
type JobStartInput struct {
	CommandSpec
	StdinOpen bool `json:"stdin_open,omitempty" jsonschema:"keep stdin open for job_write (stdin, if set, is written first)"`
	PTY       bool `json:"pty,omitempty" jsonschema:"run in a pseudo terminal (Linux pty, Windows ConPTY) for interactive/TTY programs (prompts, REPLs, ssh, top); stdin stays open and combined output has ANSI escapes stripped by job_read unless raw is set"`
	Cols      int  `json:"cols,omitempty" jsonschema:"pty: terminal width (default 120)"`
	Rows      int  `json:"rows,omitempty" jsonschema:"pty: terminal height (default 40)"`
}

// AuditArgs redacts Stdin for audit logging.
func (in JobStartInput) AuditArgs() any {
	in.Stdin = redactedLen(in.Stdin)
	return in
}

func (d *Deps) jobStart(_ context.Context, _ *mcp.CallToolRequest, in JobStartInput) (*mcp.CallToolResult, jobs.Status, error) {
	// Run without a time limit when timeout_sec is omitted.
	var timeout time.Duration
	if in.TimeoutSec > 0 {
		timeout = time.Duration(in.TimeoutSec) * time.Second
	}
	def := d.stdinDefault(in.CommandSpec, in.PTY)
	output, stdin, err := in.resolve(in.Stdin, def)
	if err != nil {
		return nil, jobs.Status{}, err
	}
	stdinEnc, err := textenc.Parse(in.StdinEncoding, textenc.Console, false, def)
	if err != nil {
		return nil, jobs.Status{}, fmt.Errorf("stdin_encoding: %w", err)
	}
	j, err := d.Jobs.Start(d.procSpec(in.CommandSpec), jobs.StartOptions{
		Stdin:         stdin,
		KeepStdin:     in.StdinOpen,
		StdinEncoding: stdinEnc.Enc,
		Output:        output,
		Timeout:       timeout,
		PTY:           in.PTY,
		Cols:          in.Cols,
		Rows:          in.Rows,
	})
	if err != nil {
		return nil, jobs.Status{}, err
	}
	return nil, j.Status(), nil
}

// JobWriteInput is the input of job_write.
type JobWriteInput struct {
	ID    string `json:"id" jsonschema:"job id from job_start"`
	Input string `json:"input,omitempty" jsonschema:"text to write to stdin (include trailing \\n or \\r to submit a line; in pty jobs use \\u0003 for Ctrl-C, \\u001b for Esc, \\u001b[A for Up)"`
	Close bool   `json:"close,omitempty" jsonschema:"close stdin after writing (in pty jobs, sends Ctrl-D on Linux or Ctrl-Z Enter on Windows)"`
	Cols  int    `json:"cols,omitempty" jsonschema:"pty: new terminal width (requires rows)"`
	Rows  int    `json:"rows,omitempty" jsonschema:"pty: new terminal height (requires cols)"`
}

// AuditArgs redacts Input for audit logging.
func (in JobWriteInput) AuditArgs() any {
	in.Input = redactedLen(in.Input)
	return in
}

// JobWriteOutput is the output of job_write.
type JobWriteOutput struct {
	Status       jobs.Status `json:"status"`
	BytesWritten int         `json:"bytes_written"`
}

func (d *Deps) jobWrite(_ context.Context, _ *mcp.CallToolRequest, in JobWriteInput) (*mcp.CallToolResult, JobWriteOutput, error) {
	j, err := d.Jobs.Get(in.ID)
	if err != nil {
		return nil, JobWriteOutput{}, err
	}
	resized := in.Cols > 0 || in.Rows > 0
	if resized {
		if in.Cols <= 0 || in.Rows <= 0 {
			return nil, JobWriteOutput{}, errors.New("set both cols and rows to resize")
		}
		if err := j.Resize(in.Cols, in.Rows); err != nil {
			return nil, JobWriteOutput{}, err
		}
	}
	var data []byte
	if in.Input != "" {
		if data, err = j.StdinEncoding().Encode(in.Input); err != nil {
			return nil, JobWriteOutput{}, fmt.Errorf("input: %w", err)
		}
		if err := j.Write(string(data), 10*time.Second); err != nil {
			return nil, JobWriteOutput{}, err
		}
	}
	if in.Close {
		if err := j.CloseStdin(); err != nil {
			return nil, JobWriteOutput{}, err
		}
	} else if in.Input == "" && !resized {
		return nil, JobWriteOutput{}, errors.New("input, close or cols/rows is required")
	}
	return nil, JobWriteOutput{Status: j.Status(), BytesWritten: len(data)}, nil
}

// JobReadInput is the input of job_read.
type JobReadInput struct {
	ID       string `json:"id" jsonschema:"job id from job_start"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"byte offset to read from (0 for start, next_offset from prior call, or negative for last -offset bytes)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"max bytes to return (default and cap: server output limit)"`
	WaitSec  int    `json:"wait_sec,omitempty" jsonschema:"seconds to wait (max 300) for new output, until match, idle_ms, or job exit"`
	Until    string `json:"until,omitempty" jsonschema:"with wait_sec: wait until output after offset matches this regex (e.g. \\$ $ or Listening on)"`
	IdleMs   int    `json:"idle_ms,omitempty" jsonschema:"with wait_sec: return once new output arrives and then stays quiet for this many ms"`
	Raw      bool   `json:"raw,omitempty" jsonschema:"pty jobs: keep ANSI escape sequences and carriage returns instead of stripping them"`
	Screen   bool   `json:"screen,omitempty" jsonschema:"pty jobs: return current terminal screen (cols x rows) and cursor instead of stream (for top, vi, less; ignores offset/max_bytes, honors wait_sec/until/idle_ms)"`
}

// JobReadOutput is the output of job_read.
type JobReadOutput struct {
	Status     jobs.Status `json:"status"`
	Matched    bool        `json:"matched,omitempty" jsonschema:"until regex matched"`
	Output     string      `json:"output" jsonschema:"interleaved stdout and stderr"`
	NextOffset int64       `json:"next_offset" jsonschema:"offset for the next incremental read"`
	More       bool        `json:"more,omitempty" jsonschema:"more output is available beyond next_offset"`
	Dropped    int64       `json:"dropped,omitempty" jsonschema:"bytes overwritten in the ring buffer before being read"`
	CursorRow  *int        `json:"cursor_row,omitempty" jsonschema:"screen: 0-based cursor row"`
	CursorCol  *int        `json:"cursor_col,omitempty" jsonschema:"screen: 0-based cursor column"`
}

func (d *Deps) jobRead(ctx context.Context, _ *mcp.CallToolRequest, in JobReadInput) (*mcp.CallToolResult, JobReadOutput, error) {
	j, err := d.Jobs.Get(in.ID)
	if err != nil {
		return nil, JobReadOutput{}, err
	}
	max := in.MaxBytes
	if max <= 0 || max > d.Cfg.MaxOutputBytes {
		max = d.Cfg.MaxOutputBytes
	}
	offset := in.Offset
	if offset < 0 {
		offset = max64(0, j.Status().Output+offset)
	}
	var until *regexp.Regexp
	if in.Until != "" {
		if until, err = regexp.Compile(in.Until); err != nil {
			return nil, JobReadOutput{}, fmt.Errorf("until: %w", err)
		}
	}
	if in.Screen && !j.PTY() {
		return nil, JobReadOutput{}, errors.New("screen needs a pty job (job_start with pty)")
	}
	strip := j.PTY() && !in.Raw
	out := JobReadOutput{}
	if in.WaitSec > 0 {
		out.Matched = waitForOutput(ctx, j, offset, time.Duration(min(in.WaitSec, 300))*time.Second, until, in.IdleMs, strip)
	}
	if in.Screen {
		text, row, col, err := j.Screen()
		if err != nil {
			return nil, JobReadOutput{}, err
		}
		st := j.Status()
		out.Status, out.Output, out.NextOffset, out.CursorRow, out.CursorCol = st, text, st.Output, &row, &col
		return nil, out, nil
	}
	data, next, dropped := j.Read(offset, max)
	if strip {
		var pending int
		data, pending = stripTerminal(data)
		next -= int64(pending)
	}
	st := j.Status()
	out.Status, out.Output, out.NextOffset, out.More, out.Dropped = st, data, next, next < st.Output, dropped
	return nil, out, nil
}

// waitForOutput waits for new output after offset, a regex match (until), an idle gap after new output (idleMs), or job exit.
func waitForOutput(ctx context.Context, j *jobs.Job, offset int64, wait time.Duration, until *regexp.Regexp, idleMs int, strip bool) bool {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	idle := time.Duration(idleMs) * time.Millisecond
	lastSize, lastChange := offset, time.Now()
	for {
		size := j.Status().Output
		if size != lastSize {
			lastSize, lastChange = size, time.Now()
		}
		switch {
		case until != nil:
			if size > offset {
				data, _, _ := j.Read(offset, 1<<20)
				if strip {
					data, _ = stripTerminal(data)
				}
				if until.MatchString(data) {
					return true
				}
			}
		case idle > 0:
			if size > offset && time.Since(lastChange) >= idle {
				return false
			}
		default:
			if size > offset {
				return false
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-j.Done():
			// Check once more against the output that arrived before the end.
			if until != nil {
				data, _, _ := j.Read(offset, 1<<20)
				if strip {
					data, _ = stripTerminal(data)
				}
				return until.MatchString(data)
			}
			return false
		case <-tick.C:
		}
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// JobIDInput is the input of tools that take only a job ID.
type JobIDInput struct {
	ID string `json:"id" jsonschema:"job id from job_start"`
}

// JobStopInput is the input of job_stop.
type JobStopInput struct {
	ID      string `json:"id" jsonschema:"job id from job_start"`
	Signal  string `json:"signal,omitempty" jsonschema:"kill (default: kill process tree), or Linux process-group signal (interrupt, terminate, hangup, quit, stop, continue, usr1, usr2); in pty jobs interrupt sends Ctrl-C (including Windows)"`
	WaitSec int    `json:"wait_sec,omitempty" jsonschema:"seconds to wait for exit (default 5)"`
}

func (d *Deps) jobStop(ctx context.Context, _ *mcp.CallToolRequest, in JobStopInput) (*mcp.CallToolResult, jobs.Status, error) {
	j, err := d.Jobs.Get(in.ID)
	if err != nil {
		return nil, jobs.Status{}, err
	}
	wait := time.Duration(orDefaultInt(in.WaitSec, 5)) * time.Second
	if sig := orDefault(in.Signal, "kill"); sig == "kill" {
		j.Stop(wait)
	} else {
		if err := j.Signal(sig); err != nil {
			return nil, jobs.Status{}, err
		}
		select {
		case <-j.Done():
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return nil, j.Status(), nil
}

// JobListOutput is the output of job_list.
type JobListOutput struct {
	Jobs []jobs.Status `json:"jobs"`
}

func (d *Deps) jobList(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, JobListOutput, error) {
	return nil, JobListOutput{Jobs: d.Jobs.List()}, nil
}
