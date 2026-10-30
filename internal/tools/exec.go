package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// CommandSpec はコマンド実行系のツールに共通の入力です。
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

// IOEncoding はコマンドの入出力の文字コードの指定です。
type IOEncoding struct {
	OutputEncoding string `json:"output_encoding,omitempty" jsonschema:"encoding of stdout/stderr: auto (default: UTF-8 if valid, otherwise the host console code page, decided per line), utf-8 (no conversion), system, or a charset name such as shift_jis, euc-jp, utf-16le"`
	StdinEncoding  string `json:"stdin_encoding,omitempty" jsonschema:"encoding to convert stdin into: utf-8 (default), system, or a charset name"`
}

// resolve は出力の変換方法と、変換済みの標準入力を返します。
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

func (c CommandSpec) spec() proc.Spec {
	return proc.Spec{Command: c.Command, Argv: c.Argv, Shell: c.Shell, Dir: c.Cwd, Env: c.Env}
}

// AuditArgs は標準入力の内容をログに残さないようにします。
func (c CommandSpec) AuditArgs() any {
	c.Stdin = redactedLen(c.Stdin)
	return c
}

func (d *Deps) execRun(ctx context.Context, _ *mcp.CallToolRequest, in CommandSpec) (*mcp.CallToolResult, proc.Result, error) {
	output, stdin, err := in.resolve(in.Stdin)
	if err != nil {
		return nil, proc.Result{}, err
	}
	res, err := proc.Run(ctx, in.spec(), proc.Options{
		DefaultShell: d.DefaultShell,
		Stdin:        stdin,
		Output:       output,
		Timeout:      d.Cfg.ClampTimeout(in.TimeoutSec),
		MaxOutput:    d.Cfg.MaxOutputBytes,
	})
	return nil, res, err
}

// JobStartInput は job_start の入力です。
type JobStartInput struct {
	CommandSpec
	StdinOpen bool `json:"stdin_open,omitempty" jsonschema:"keep standard input open so that input can be sent later with job_write (for REPLs and programs that prompt). stdin, if given, is sent first"`
}

// AuditArgs は標準入力の内容をログに残さないようにします。
func (in JobStartInput) AuditArgs() any {
	in.Stdin = redactedLen(in.Stdin)
	return in
}

func (d *Deps) jobStart(_ context.Context, _ *mcp.CallToolRequest, in JobStartInput) (*mcp.CallToolResult, jobs.Status, error) {
	// ジョブは timeout_sec を省略すると時間制限なしで実行する。
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
	j, err := d.Jobs.Start(in.spec(), jobs.StartOptions{
		Stdin:         stdin,
		KeepStdin:     in.StdinOpen,
		StdinEncoding: stdinEnc.Enc,
		Output:        output,
		Timeout:       timeout,
	})
	if err != nil {
		return nil, jobs.Status{}, err
	}
	return nil, j.Status(), nil
}

// JobWriteInput は job_write の入力です。
type JobWriteInput struct {
	ID    string `json:"id" jsonschema:"job id returned by job_start"`
	Input string `json:"input,omitempty" jsonschema:"text to send to standard input. Include a trailing newline to submit a line, e.g. \"yes\\n\"; it is not added automatically"`
	Close bool   `json:"close,omitempty" jsonschema:"close standard input after sending input (signals end of input, like Ctrl-D)"`
}

// AuditArgs は入力の内容をログに残さないようにします。
func (in JobWriteInput) AuditArgs() any {
	in.Input = redactedLen(in.Input)
	return in
}

// JobWriteOutput は job_write の出力です。
type JobWriteOutput struct {
	Status       jobs.Status `json:"status"`
	BytesWritten int         `json:"bytes_written"`
}

func (d *Deps) jobWrite(_ context.Context, _ *mcp.CallToolRequest, in JobWriteInput) (*mcp.CallToolResult, JobWriteOutput, error) {
	j, err := d.Jobs.Get(in.ID)
	if err != nil {
		return nil, JobWriteOutput{}, err
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
	} else if in.Input == "" {
		return nil, JobWriteOutput{}, errors.New("input or close is required")
	}
	return nil, JobWriteOutput{Status: j.Status(), BytesWritten: len(data)}, nil
}

// JobReadInput は job_read の入力です。
type JobReadInput struct {
	ID       string `json:"id" jsonschema:"job id returned by job_start"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"byte offset to read from: 0 for the beginning, then next_offset of the previous call"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes to return (default and cap: the server output limit)"`
	WaitSec  int    `json:"wait_sec,omitempty" jsonschema:"wait up to this many seconds (max 30) until output beyond offset arrives or the job exits. Use this instead of polling rapidly"`
}

// JobReadOutput は job_read の出力です。
type JobReadOutput struct {
	Status     jobs.Status `json:"status"`
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
	if in.WaitSec > 0 {
		waitForOutput(ctx, j, in.Offset, time.Duration(min(in.WaitSec, 30))*time.Second)
	}
	data, next, dropped := j.Read(in.Offset, max)
	st := j.Status()
	return nil, JobReadOutput{Status: st, Output: data, NextOffset: next, More: next < st.Output, Dropped: dropped}, nil
}

// waitForOutput は offset より後の出力が届くか、ジョブが終了するまで待ちます。
func waitForOutput(ctx context.Context, j *jobs.Job, offset int64, wait time.Duration) {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for j.Status().Output <= offset {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-j.Done():
			return
		case <-tick.C:
		}
	}
}

// JobIDInput はジョブの識別子だけを受け取るツールの入力です。
type JobIDInput struct {
	ID string `json:"id" jsonschema:"job id returned by job_start"`
}

func (d *Deps) jobStop(_ context.Context, _ *mcp.CallToolRequest, in JobIDInput) (*mcp.CallToolResult, jobs.Status, error) {
	j, err := d.Jobs.Get(in.ID)
	if err != nil {
		return nil, jobs.Status{}, err
	}
	j.Stop(5 * time.Second)
	return nil, j.Status(), nil
}

// JobListOutput は job_list の出力です。
type JobListOutput struct {
	Jobs []jobs.Status `json:"jobs"`
}

func (d *Deps) jobList(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, JobListOutput, error) {
	return nil, JobListOutput{Jobs: d.Jobs.List()}, nil
}
