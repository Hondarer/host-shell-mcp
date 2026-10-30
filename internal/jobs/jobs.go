// Package jobs は長時間実行するコマンドを管理します。
//
// 実行を開始して識別子を返し、後から出力の取得、入力の送信、状態の確認、停止を行います。
// 標準入出力をパイプにつなぐ方式と、疑似端末 (PTY) につなぐ方式があります。
package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/limit"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/pty"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// maxFinished は保持する終了済みのジョブの数です。超えると古いものから破棄します。
const maxFinished = 50

// drainWait は、プロセスの終了後に残りの出力を読み切るまで待つ時間です。
const drainWait = 2 * time.Second

// Manager はジョブの一覧を保持します。
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
	pid     int
	cancel  context.CancelFunc
	out     *limit.Ring
	conv    *textenc.Writer
	done    chan struct{}
	term    *pty.Terminal // PTY のジョブの場合だけ

	// stdin は入力の書き込み口です。stdinMu で書き込みを直列にします。
	stdinMu  sync.Mutex
	stdin    io.WriteCloser
	stdinEnc textenc.Encoding

	mu       sync.Mutex
	ended    time.Time
	exitCode int
	waitErr  error
	signaled string
}

// Status はジョブの状態です。
type Status struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	PID       int    `json:"pid"`
	PTY       bool   `json:"pty,omitempty" jsonschema:"the job runs in a pseudo terminal"`
	Running   bool   `json:"running"`
	StdinOpen bool   `json:"stdin_open,omitempty" jsonschema:"input can be sent with job_write"`
	ExitCode  *int   `json:"exit_code,omitempty" jsonschema:"exit code, present after the job ended"`
	Signal    string `json:"signal,omitempty" jsonschema:"last signal sent with job_stop"`
	Encoding  string `json:"output_encoding,omitempty" jsonschema:"set when the output was converted to UTF-8 from this encoding"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Output    int64  `json:"output_bytes" jsonschema:"total bytes of output produced so far"`
}

// StartOptions はジョブの起動の指定です。
type StartOptions struct {
	// Stdin は最初に標準入力に渡すバイト列です (変換済み)。
	Stdin string
	// KeepStdin は、標準入力を開いたままにし、Write で後から送れるようにします。PTY の場合は常に開いています。
	KeepStdin bool
	// StdinEncoding は Write で送る文字列の変換先です。
	StdinEncoding textenc.Encoding
	// Output は出力の文字コードです。出力は UTF-8 に変換してから保持します。
	Output textenc.Choice
	// Timeout は時間制限です。0 の場合は時間制限なしで実行します。
	Timeout time.Duration
	// PTY は疑似端末につないで起動することを示します。Cols と Rows は端末の大きさです。
	PTY        bool
	Cols, Rows int
}

// Start はジョブを開始します。
func (m *Manager) Start(spec proc.Spec, opt StartOptions) (*Job, error) {
	// ジョブはツール呼び出しより長く生きるため、呼び出しの ctx ではなく独自の ctx を使う。
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if opt.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), opt.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	spec.Interactive = opt.PTY
	cmd, err := proc.Command(ctx, spec, m.defaultShell)
	if err != nil {
		cancel()
		return nil, err
	}
	out := limit.NewRing(m.bufferBytes)
	j := &Job{
		label:    label(spec),
		started:  time.Now(),
		cancel:   cancel,
		out:      out,
		conv:     textenc.NewWriter(out, opt.Output),
		done:     make(chan struct{}),
		stdinEnc: opt.StdinEncoding,
	}
	var wait func() (int, error)
	if opt.PTY {
		wait, err = j.startPTY(ctx, cmd, opt)
	} else {
		wait, err = j.startPipe(cmd, opt)
	}
	if err != nil {
		cancel()
		return nil, err
	}

	m.mu.Lock()
	m.seq++
	j.id = fmt.Sprintf("job-%d", m.seq)
	m.jobs[j.id] = j
	m.pruneLocked()
	m.mu.Unlock()

	if opt.Stdin != "" && j.stdin != nil {
		go j.write(opt.Stdin)
	}
	go func() {
		code, err := wait()
		j.mu.Lock()
		j.ended = time.Now()
		j.exitCode = code
		j.waitErr = err
		j.mu.Unlock()
		cancel()
		close(j.done)
		j.CloseStdin()
	}()
	return j, nil
}

func (j *Job) startPipe(cmd *exec.Cmd, opt StartOptions) (func() (int, error), error) {
	cmd.Stdout, cmd.Stderr = j.conv, j.conv
	if opt.KeepStdin {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		j.stdin = stdin
	} else if opt.Stdin != "" {
		cmd.Stdin = strings.NewReader(opt.Stdin)
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	j.pid = cmd.Process.Pid
	return func() (int, error) {
		err := cmd.Wait()
		j.conv.Close()
		var ee *exec.ExitError
		if err != nil && errors.As(err, &ee) {
			err = nil
		}
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return code, err
	}, nil
}

func (j *Job) startPTY(ctx context.Context, cmd *exec.Cmd, opt StartOptions) (func() (int, error), error) {
	t, err := pty.Start(ctx, cmd, opt.Cols, opt.Rows)
	if err != nil {
		return nil, err
	}
	j.term = t
	j.pid = t.Pid()
	j.stdin = ptyInput{t}
	copied := make(chan struct{})
	go func() {
		io.Copy(j.conv, t)
		close(copied)
	}()
	return func() (int, error) {
		code, err := t.Wait()
		// 孫プロセスが端末を握ったままの場合に備え、読み切るのを待つ時間に上限を設ける。
		select {
		case <-copied:
		case <-time.After(drainWait):
		}
		t.Close()
		<-copied
		j.conv.Close()
		return code, err
	}, nil
}

// ptyInput は端末への入力です。閉じると、端末に入力の終わり (Ctrl-D、Windows では Ctrl-Z) を送ります。
type ptyInput struct{ t *pty.Terminal }

func (p ptyInput) Write(b []byte) (int, error) { return p.t.Write(b) }
func (p ptyInput) Close() error {
	_, err := io.WriteString(p.t, pty.EOF)
	return err
}

// pruneLocked は終了済みのジョブが maxFinished を超えた分を古い順に破棄します。
func (m *Manager) pruneLocked() {
	var finished []*Job
	for _, j := range m.jobs {
		select {
		case <-j.done:
			finished = append(finished, j)
		default:
		}
	}
	if len(finished) <= maxFinished {
		return
	}
	sort.Slice(finished, func(a, b int) bool { return finished[a].started.Before(finished[b].started) })
	for _, j := range finished[:len(finished)-maxFinished] {
		delete(m.jobs, j.id)
	}
}

// Get は識別子に対応するジョブを返します。
func (m *Manager) Get(id string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found (finished jobs are kept up to %d; see job_list)", id, maxFinished)
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
// SSH が切断されると host-shell も終了するため、ジョブも停止します。
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

// PTY はジョブが疑似端末で動いているかを返します。
func (j *Job) PTY() bool { return j.term != nil }

// Done はジョブの終了時に閉じるチャネルを返します。
func (j *Job) Done() <-chan struct{} { return j.done }

// Status はジョブの状態を返します。
func (j *Job) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := Status{
		ID:        j.id,
		Command:   j.label,
		PID:       j.pid,
		PTY:       j.term != nil,
		Running:   j.ended.IsZero(),
		StdinOpen: j.stdinOpen(),
		Signal:    j.signaled,
		StartedAt: j.started.Format(time.RFC3339),
		Output:    j.out.Total(),
		Encoding:  j.conv.Converted(),
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

// StdinEncoding は Write で送る文字列の変換先を返します。
func (j *Job) StdinEncoding() textenc.Encoding { return j.stdinEnc }

// ErrStdinClosed は標準入力が開いていないジョブに書き込んだことを示します。
var ErrStdinClosed = errors.New("stdin is not open (start the job with stdin_open or pty, and do not close it before writing)")

// Write は標準入力にバイト列 (変換済み) を書き込みます。
// 子プロセスが読み取らずに wait を過ぎた場合はエラーを返し、書き込みは裏で続けます。
func (j *Job) Write(data string, wait time.Duration) error {
	if !j.stdinOpen() {
		return ErrStdinClosed
	}
	done := make(chan error, 1)
	go func() { done <- j.write(data) }()
	select {
	case err := <-done:
		return err
	case <-time.After(wait):
		return fmt.Errorf("the process did not read its input within %s; the rest is written in the background", wait)
	}
}

func (j *Job) write(data string) error {
	j.stdinMu.Lock()
	defer j.stdinMu.Unlock()
	if j.stdin == nil {
		return ErrStdinClosed
	}
	_, err := io.WriteString(j.stdin, data)
	return err
}

// CloseStdin は標準入力を閉じ、子プロセスに入力の終わり (EOF) を伝えます。
func (j *Job) CloseStdin() error {
	j.stdinMu.Lock()
	defer j.stdinMu.Unlock()
	if j.stdin == nil {
		return nil
	}
	err := j.stdin.Close()
	j.stdin = nil
	select {
	case <-j.done:
		err = nil // 終了したジョブの入力を閉じるときのエラーは意味がない。
	default:
	}
	return err
}

func (j *Job) stdinOpen() bool {
	if !j.stdinMu.TryLock() {
		return true // 書き込み中
	}
	defer j.stdinMu.Unlock()
	return j.stdin != nil
}

// Resize は PTY のジョブの端末の大きさを変えます。
func (j *Job) Resize(cols, rows int) error {
	if j.term == nil {
		return errors.New("the job does not run in a pseudo terminal (start it with pty)")
	}
	return j.term.Resize(cols, rows)
}

// Signal はジョブにシグナルを送ります。PTY のジョブの interrupt は、端末に Ctrl-C を送ります。
func (j *Job) Signal(name string) error {
	select {
	case <-j.done:
		return errors.New("the job has already ended")
	default:
	}
	var err error
	if j.term != nil && name == "interrupt" {
		_, err = io.WriteString(j.term, "\x03")
	} else {
		err = proc.Signal(j.pid, name, true)
	}
	if err == nil {
		j.mu.Lock()
		j.signaled = name
		j.mu.Unlock()
	}
	return err
}

// Stop はジョブを強制終了し、終了を最大 wait だけ待ちます。
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
