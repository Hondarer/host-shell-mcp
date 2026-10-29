// Package jobs は長時間実行するコマンドを管理します。
//
// 実行を開始して識別子を返し、後から出力の取得、状態の確認、停止を行います。
package jobs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/limit"
	"github.com/hondarer/host-shell-mcp/internal/proc"
)

// Manager はジョブの一覧を保持します。
// TODO: 終了したジョブの破棄 (件数や経過時間による上限) を実装する。
type Manager struct {
	mu           sync.Mutex
	jobs         map[string]*Job
	seq          int
	defaultShell string
	bufferBytes  int
}

// NewManager は Manager を作ります。bufferBytes はジョブごとに保持する出力の量です。
func NewManager(defaultShell string, bufferBytes int) *Manager {
	return &Manager{jobs: map[string]*Job{}, defaultShell: defaultShell, bufferBytes: bufferBytes}
}

// Job は 1 つの長時間実行コマンドです。標準出力と標準エラーは 1 つのストリームにまとめます。
type Job struct {
	id      string
	label   string
	started time.Time
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	out     *limit.Ring
	done    chan struct{}

	mu       sync.Mutex
	ended    time.Time
	exitCode int
	waitErr  error
}

// Status はジョブの状態です。
type Status struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	PID       int    `json:"pid"`
	Running   bool   `json:"running"`
	ExitCode  *int   `json:"exit_code,omitempty" jsonschema:"exit code, present after the job ended"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Output    int64  `json:"output_bytes" jsonschema:"total bytes of output produced so far"`
}

// Start はジョブを開始します。timeout が 0 の場合は時間制限なしで実行します。
func (m *Manager) Start(spec proc.Spec, stdin string, timeout time.Duration) (*Job, error) {
	// ジョブはツール呼び出しより長く生きるため、呼び出しの ctx ではなく独自の ctx を使う。
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	cmd, err := proc.Command(ctx, spec, m.defaultShell)
	if err != nil {
		cancel()
		return nil, err
	}
	out := limit.NewRing(m.bufferBytes)
	cmd.Stdout, cmd.Stderr = out, out
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	m.mu.Lock()
	m.seq++
	j := &Job{
		id:      fmt.Sprintf("job-%d", m.seq),
		label:   label(spec),
		started: time.Now(),
		cmd:     cmd,
		cancel:  cancel,
		out:     out,
		done:    make(chan struct{}),
	}
	m.jobs[j.id] = j
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		j.mu.Lock()
		j.ended = time.Now()
		j.exitCode = cmd.ProcessState.ExitCode()
		var ee *exec.ExitError
		if err != nil && !errors.As(err, &ee) {
			j.waitErr = err
		}
		j.mu.Unlock()
		cancel()
		close(j.done)
	}()
	return j, nil
}

// Get は識別子に対応するジョブを返します。
func (m *Manager) Get(id string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	return j, nil
}

// List はすべてのジョブの状態を開始順に返します。
func (m *Manager) List() []Status {
	m.mu.Lock()
	all := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		all = append(all, j)
	}
	m.mu.Unlock()
	sort.Slice(all, func(a, b int) bool { return all[a].started.Before(all[b].started) })
	out := make([]Status, len(all))
	for i, j := range all {
		out[i] = j.Status()
	}
	return out
}

// StopAll は実行中のすべてのジョブを停止します。host-shell の終了時に呼びます。
// TODO(未決事項): SSH の切断時にジョブを停止するか継続するか。現状は停止する。
func (m *Manager) StopAll(wait time.Duration) {
	m.mu.Lock()
	all := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		all = append(all, j)
	}
	m.mu.Unlock()
	for _, j := range all {
		j.Stop(wait)
	}
}

// ID はジョブの識別子を返します。
func (j *Job) ID() string { return j.id }

// Done はジョブの終了時に閉じるチャネルを返します。
func (j *Job) Done() <-chan struct{} { return j.done }

// Status はジョブの状態を返します。
func (j *Job) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := Status{
		ID:        j.id,
		Command:   j.label,
		PID:       j.cmd.Process.Pid,
		Running:   j.ended.IsZero(),
		StartedAt: j.started.Format(time.RFC3339),
		Output:    j.out.Total(),
	}
	if !s.Running {
		code := j.exitCode
		s.ExitCode = &code
		s.EndedAt = j.ended.Format(time.RFC3339)
	}
	if j.waitErr != nil {
		s.Error = j.waitErr.Error()
	}
	return s
}

// Read は絶対位置 offset から最大 n バイトの出力を返します。
func (j *Job) Read(offset int64, n int) (data string, next, dropped int64) {
	return j.out.Read(offset, n)
}

// Stop はジョブを停止し、終了を最大 wait だけ待ちます。
func (j *Job) Stop(wait time.Duration) {
	j.cancel()
	select {
	case <-j.done:
	case <-time.After(wait):
	}
}

func label(spec proc.Spec) string {
	if spec.Command != "" {
		return spec.Command
	}
	return strings.Join(spec.Argv, " ")
}
