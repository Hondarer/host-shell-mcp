package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// CommandSpec is the input shared by the command execution tools.
type CommandSpec struct {
	Command    string            `json:"command,omitempty" jsonschema:"shell command line, interpreted by the selected shell (pipes, redirects, && work). Exactly one of command or argv is required"`
	Argv       []string          `json:"argv,omitempty" jsonschema:"program and arguments to run without a shell, e.g. [\"git\", \"log\", \"-n\", \"5\"]. No quoting or globbing is applied"`
	Shell      string            `json:"shell,omitempty" jsonschema:"shell for command: sh, bash or pwsh on Linux; cmd, powershell or pwsh on Windows. Defaults to the host default shown in sys_info"`
	Cwd        string            `json:"cwd,omitempty" jsonschema:"working directory (default: the directory host-shell was started in). Each call starts fresh; cd in one call does not carry over"`
	Env        map[string]string `json:"env,omitempty" jsonschema:"environment variables to add or override"`
	Stdin      string            `json:"stdin,omitempty" jsonschema:"text passed to standard input, which is then closed. Without it the command gets an empty stdin, so interactive prompts fail instead of hanging"`
	TimeoutSec int               `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds; the process tree is killed when it expires"`
	IOEncoding
}

// IOEncoding specifies the encoding of command input and output.
type IOEncoding struct {
	OutputEncoding string `json:"output_encoding,omitempty" jsonschema:"encoding of stdout/stderr: auto (default: UTF-8 if valid, otherwise the host console code page, decided per line), utf-8 (no conversion), system, or a charset name such as shift_jis, euc-jp, utf-16le"`
	StdinEncoding  string `json:"stdin_encoding,omitempty" jsonschema:"encoding to convert stdin into: utf-8 (default), system, or a charset name"`
}

// resolve returns how to convert the output and the converted standard input.
func (e IOEncoding) resolve(stdin string) (textenc.Choice, string, error) {
	out, err := textenc.Parse(e.OutputEncoding, textenc.Console, true, textenc.UTF8)
	if err != nil {
		return textenc.Choice{}, "", fmt.Errorf("output_encoding: %w", err)
	}
	in, err := textenc.Parse(e.StdinEncoding, textenc.Console, false, textenc.UTF8)
	if err != nil {
		return textenc.Choice{}, "", fmt.Errorf("stdin_encoding: %w", err)
	}
	b, err := in.Enc.Encode(stdin)
	if err != nil {
		return textenc.Choice{}, "", fmt.Errorf("stdin: %w", err)
	}
	return out, string(b), nil
}

// procSpec builds the start specification with the user's environment restrictions applied.
func (d *Deps) procSpec(c CommandSpec) proc.Spec {
	spec := proc.Spec{Command: c.Command, Argv: c.Argv, Shell: c.Shell, Dir: c.Cwd, Env: c.Env}
	if d.Policy.FiltersEnv() {
		spec.EnvFilter = d.Policy.FilterEnv
	}
	return spec
}

// PathArgs passes the working directory to the policy check as a read target.
func (c CommandSpec) PathArgs() (read, write []string) {
	if c.Cwd != "" {
		read = []string{c.Cwd}
	}
	return read, nil
}

// CommandArgs passes the programs started and the whole command line to the policy check.
func (c CommandSpec) CommandArgs(defaultShell string) (programs []string, text string) {
	if len(c.Argv) > 0 {
		return []string{c.Argv[0]}, strings.Join(c.Argv, " ")
	}
	return []string{orDefault(c.Shell, defaultShell)}, c.Command
}

// AuditArgs keeps the content of standard input out of the log.
func (c CommandSpec) AuditArgs() any {
	c.Stdin = redactedLen(c.Stdin)
	return c
}

func (d *Deps) execRun(ctx context.Context, req *mcp.CallToolRequest, in CommandSpec) (*mcp.CallToolResult, proc.Result, error) {
	output, stdin, err := in.resolve(in.Stdin)
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
	StdinOpen bool `json:"stdin_open,omitempty" jsonschema:"keep standard input open so that input can be sent later with job_write (for REPLs and programs that prompt). stdin, if given, is sent first"`
	PTY       bool `json:"pty,omitempty" jsonschema:"run in a pseudo terminal (Linux pty, Windows ConPTY) for programs that need a terminal: password prompts, REPLs, ssh, top, programs that change behaviour when not on a terminal. Input is always open; stdout and stderr are one stream with terminal escape sequences (job_read strips them unless raw)"`
	Cols      int  `json:"cols,omitempty" jsonschema:"pty: terminal width (default 120)"`
	Rows      int  `json:"rows,omitempty" jsonschema:"pty: terminal height (default 40)"`
}

// AuditArgs keeps the content of standard input out of the log.
func (in JobStartInput) AuditArgs() any {
	in.Stdin = redactedLen(in.Stdin)
	return in
}

func (d *Deps) jobStart(_ context.Context, _ *mcp.CallToolRequest, in JobStartInput) (*mcp.CallToolResult, jobs.Status, error) {
	// Without timeout_sec, a job runs without a time limit.
	var timeout time.Duration
	if in.TimeoutSec > 0 {
		timeout = time.Duration(in.TimeoutSec) * time.Second
	}
	output, stdin, err := in.resolve(in.Stdin)
	if err != nil {
		return nil, jobs.Status{}, err
	}
	stdinEnc, err := textenc.Parse(in.StdinEncoding, textenc.Console, false, textenc.UTF8)
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
	ID    string `json:"id" jsonschema:"job id returned by job_start"`
	Input string `json:"input,omitempty" jsonschema:"text to send to standard input. Include a trailing newline to submit a line, e.g. \"yes\\n\"; it is not added automatically. In a pty job send \\r for Enter if a program expects it, \\u0003 for Ctrl-C, \\u001b for Esc, \\u001b[A for the up arrow"`
	Close bool   `json:"close,omitempty" jsonschema:"close standard input after sending input (signals end of input, like Ctrl-D). In a pty job this sends Ctrl-D (Linux) or Ctrl-Z Enter (Windows)"`
	Cols  int    `json:"cols,omitempty" jsonschema:"pty: resize the terminal to this width (with rows)"`
	Rows  int    `json:"rows,omitempty" jsonschema:"pty: resize the terminal to this height (with cols)"`
}

// AuditArgs keeps the input out of the log.
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
	ID       string `json:"id" jsonschema:"job id returned by job_start"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"byte offset to read from: 0 for the beginning, then next_offset of the previous call. A negative value reads the last -offset bytes"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes to return (default and cap: the server output limit)"`
	WaitSec  int    `json:"wait_sec,omitempty" jsonschema:"wait up to this many seconds (max 300) for new output beyond offset (or for until/idle_ms), returning early when the job exits. Use this instead of polling rapidly"`
	Until    string `json:"until,omitempty" jsonschema:"with wait_sec: keep waiting until the output after offset matches this regular expression (e.g. a prompt like \\$ $ or >>> $, or a line like Listening on). matched tells whether it did"`
	IdleMs   int    `json:"idle_ms,omitempty" jsonschema:"with wait_sec: return once new output has arrived and then nothing more came for this many milliseconds (the program is waiting or done printing). Good after sending a command to a REPL or shell"`
	Raw      bool   `json:"raw,omitempty" jsonschema:"pty jobs: return terminal escape sequences and carriage returns as they are. By default they are removed so the output reads as plain text"`
}

// JobReadOutput is the output of job_read.
type JobReadOutput struct {
	Status     jobs.Status `json:"status"`
	Matched    bool        `json:"matched,omitempty" jsonschema:"until matched the output"`
	Output     string      `json:"output" jsonschema:"stdout and stderr interleaved"`
	NextOffset int64       `json:"next_offset" jsonschema:"pass as offset to read only new output next time"`
	More       bool        `json:"more,omitempty" jsonschema:"more output is already available beyond next_offset"`
	Dropped    int64       `json:"dropped,omitempty" jsonschema:"bytes discarded from the buffer before they were read (the job produced output faster than it was read)"`
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
	strip := j.PTY() && !in.Raw
	out := JobReadOutput{}
	if in.WaitSec > 0 {
		out.Matched = waitForOutput(ctx, j, offset, time.Duration(min(in.WaitSec, 300))*time.Second, until, in.IdleMs, strip)
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

// waitForOutput waits until output after offset arrives or the job ends.
// With until, it waits until the output matches it and reports whether it matched.
// With idleMs, it returns once no new output has arrived for that long after some output.
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
	ID string `json:"id" jsonschema:"job id returned by job_start"`
}

// JobStopInput is the input of job_stop.
type JobStopInput struct {
	ID      string `json:"id" jsonschema:"job id returned by job_start"`
	Signal  string `json:"signal,omitempty" jsonschema:"kill (default: kill the whole process tree), or on Linux interrupt, terminate, hangup, quit, stop, continue, usr1, usr2 sent to the job's process group. In a pty job interrupt sends Ctrl-C to the terminal (also on Windows)"`
	WaitSec int    `json:"wait_sec,omitempty" jsonschema:"seconds to wait for the job to exit after the signal (default 5)"`
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
