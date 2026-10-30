// Package jobs manages long-running commands.
//
// Starting a job returns an ID; output, input, status and stopping are handled afterwards.
// Standard input and output are connected either to pipes or to a pseudo terminal (PTY).
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

// maxFinished is how many finished jobs are kept. Older ones are dropped beyond that.
const maxFinished = 50

// drainWait is how long to wait for the remaining output after the process exits.
const drainWait = 2 * time.Second

// Manager holds the jobs.
type Manager struct {
	mu           sync.Mutex
	jobs         map[string]*Job
	seq          int
	defaultShell string
	bufferBytes  int
}

// NewManager creates a Manager. bufferBytes is the amount of output kept per job.
func NewManager(defaultShell string, bufferBytes int) *Manager {
	return &Manager{jobs: map[string]*Job{}, defaultShell: defaultShell, bufferBytes: bufferBytes}
}

// Job is one long-running command. Standard output and standard error are merged into one stream.
type Job struct {
	id      string
	label   string
	started time.Time
	pid     int
	cancel  context.CancelFunc
	out     *limit.Ring
	conv    *textenc.Writer
	done    chan struct{}
	term    *pty.Terminal // PTY jobs only

	// stdin is where input is written. stdinMu serializes writes.
	stdinMu  sync.Mutex
	stdin    io.WriteCloser
	stdinEnc textenc.Encoding

	mu       sync.Mutex
	ended    time.Time
	exitCode int
	waitErr  error
	signaled string
}

// Status is the state of a job.
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

// StartOptions specifies how to start a job.
type StartOptions struct {
	// Stdin is the bytes first passed to standard input (already encoded).
	Stdin string
	// KeepStdin keeps standard input open so that Write can send more later. A PTY is always open.
	KeepStdin bool
	// StdinEncoding is the encoding of strings sent with Write.
	StdinEncoding textenc.Encoding
	// Output is the encoding of the output. Output is converted to UTF-8 before it is kept.
	Output textenc.Choice
	// Timeout is the time limit. 0 runs without a time limit.
	Timeout time.Duration
	// PTY starts the job attached to a pseudo terminal. Cols and Rows are the terminal size.
	PTY        bool
	Cols, Rows int
}

// Start starts a job.
func (m *Manager) Start(spec proc.Spec, opt StartOptions) (*Job, error) {
	// A job outlives the tool call, so it uses its own ctx instead of the call's.
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
		// Cap the wait for the remaining output in case a grandchild keeps the terminal open.
		// On Windows, also allow for waiting until the output settles before the terminal is closed.
		select {
		case <-copied:
		case <-time.After(drainWait + time.Second):
		}
		t.Close()
		<-copied
		j.conv.Close()
		return code, err
	}, nil
}

// ptyInput is the input to the terminal. Closing it sends end of input to the terminal (Ctrl-D, Ctrl-Z on Windows).
type ptyInput struct{ t *pty.Terminal }

func (p ptyInput) Write(b []byte) (int, error) { return p.t.Write(b) }
func (p ptyInput) Close() error {
	_, err := io.WriteString(p.t, pty.EOF)
	return err
}

// pruneLocked drops the oldest finished jobs beyond maxFinished.
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

// Get returns the job with the given ID.
func (m *Manager) Get(id string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found (finished jobs are kept up to %d; see job_list)", id, maxFinished)
	}
	return j, nil
}

// List returns the status of all jobs in start order.
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

// StopAll stops all running jobs. It is called when host-shell exits.
// host-shell exits when SSH disconnects, so jobs stop then as well.
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

// ID returns the job ID.
func (j *Job) ID() string { return j.id }

// PTY reports whether the job runs in a pseudo terminal.
func (j *Job) PTY() bool { return j.term != nil }

// Done returns a channel that is closed when the job ends.
func (j *Job) Done() <-chan struct{} { return j.done }

// Status returns the state of the job.
func (j *Job) Status() Status {
	// Count a prompt without a newline as output too, so it can be read.
	j.conv.Flush()
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

// Read returns up to n bytes of output from absolute offset offset.
func (j *Job) Read(offset int64, n int) (data string, next, dropped int64) {
	j.conv.Flush()
	return j.out.Read(offset, n)
}

// StdinEncoding returns the encoding of strings sent with Write.
func (j *Job) StdinEncoding() textenc.Encoding { return j.stdinEnc }

// ErrStdinClosed reports a write to a job whose standard input is not open.
var ErrStdinClosed = errors.New("stdin is not open (start the job with stdin_open or pty, and do not close it before writing)")

// Write writes bytes (already encoded) to standard input.
// If the child does not read them within wait, it returns an error and the write continues in the background.
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

// CloseStdin closes standard input, telling the child the input has ended (EOF).
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
		err = nil // Errors from closing the input of a finished job mean nothing.
	default:
	}
	return err
}

func (j *Job) stdinOpen() bool {
	if !j.stdinMu.TryLock() {
		return true // a write is in progress
	}
	defer j.stdinMu.Unlock()
	return j.stdin != nil
}

// Resize changes the terminal size of a PTY job.
func (j *Job) Resize(cols, rows int) error {
	if j.term == nil {
		return errors.New("the job does not run in a pseudo terminal (start it with pty)")
	}
	return j.term.Resize(cols, rows)
}

// Signal sends a signal to the job. For a PTY job, interrupt sends Ctrl-C to the terminal.
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

// Stop kills the job and waits up to wait for it to end.
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
