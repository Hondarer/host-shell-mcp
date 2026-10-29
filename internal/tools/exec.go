package tools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/proc"
)

// CommandSpec はコマンド実行系のツールに共通の入力です。
type CommandSpec struct {
	Command    string            `json:"command,omitempty" jsonschema:"shell command line, interpreted by the selected shell"`
	Argv       []string          `json:"argv,omitempty" jsonschema:"program and arguments to run without a shell (exclusive with command)"`
	Shell      string            `json:"shell,omitempty" jsonschema:"shell for command: sh or bash or pwsh on Linux; cmd or powershell or pwsh on Windows. Defaults to the host default (see sys_info)"`
	Cwd        string            `json:"cwd,omitempty" jsonschema:"working directory"`
	Env        map[string]string `json:"env,omitempty" jsonschema:"environment variables to add or override"`
	Stdin      string            `json:"stdin,omitempty" jsonschema:"text passed to standard input"`
	TimeoutSec int               `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds"`
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
	res, err := proc.Run(ctx, in.spec(), proc.Options{
		DefaultShell: d.DefaultShell,
		Stdin:        in.Stdin,
		Timeout:      d.Cfg.ClampTimeout(in.TimeoutSec),
		MaxOutput:    d.Cfg.MaxOutputBytes,
	})
	return nil, res, err
}

func (d *Deps) jobStart(_ context.Context, _ *mcp.CallToolRequest, in CommandSpec) (*mcp.CallToolResult, jobs.Status, error) {
	// ジョブは timeout_sec を省略すると時間制限なしで実行する。
	var timeout time.Duration
	if in.TimeoutSec > 0 {
		timeout = time.Duration(in.TimeoutSec) * time.Second
	}
	j, err := d.Jobs.Start(in.spec(), in.Stdin, timeout)
	if err != nil {
		return nil, jobs.Status{}, err
	}
	return nil, j.Status(), nil
}

// JobReadInput は job_read の入力です。
type JobReadInput struct {
	ID       string `json:"id"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"byte offset to read from (use next_offset of the previous call)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes to return"`
	WaitSec  int    `json:"wait_sec,omitempty" jsonschema:"wait up to this many seconds (max 30) for new output or job exit"`
}

// JobReadOutput は job_read の出力です。
type JobReadOutput struct {
	Status     jobs.Status `json:"status"`
	Output     string      `json:"output"`
	NextOffset int64       `json:"next_offset"`
	Dropped    int64       `json:"dropped,omitempty" jsonschema:"bytes discarded from the buffer before they were read"`
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
	return nil, JobReadOutput{Status: j.Status(), Output: data, NextOffset: next, Dropped: dropped}, nil
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
	ID string `json:"id"`
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
