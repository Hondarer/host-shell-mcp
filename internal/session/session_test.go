package session

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/bridge"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	target "github.com/hondarer/host-shell-mcp/internal/server"
)

// shortDir sets EnvDir to a new directory with a short path: the per-test temporary directory
// on macOS is too long for socket paths.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv(EnvDir, dir)
	return dir
}

// hostShell returns a transport to an in-process target host-shell with the given policy.
func hostShell(t *testing.T, pol policy.Options) mcp.Transport {
	t.Helper()
	cfg := config.Default()
	cfg.LogFile = ""
	cfg.Policy = pol
	al, _ := audit.Open("")
	p, err := target.NewPolicy(cfg, nil, al)
	if err != nil {
		t.Fatal(err)
	}
	s, closeServer := target.New(cfg, target.Session{Policy: p, Audit: al})
	return serveTransport(t, s, closeServer)
}

func serveTransport(t *testing.T, s *mcp.Server, closeServer func()) mcp.Transport {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ss.Close()
		closeServer()
	})
	return ct
}

// startServe runs serve until the test ends and waits until the session is ready.
func startServe(t *testing.T, opt serveOptions) <-chan error {
	t.Helper()
	if opt.Name == "" {
		opt.Name = DefaultName
	}
	if opt.Mode == "" {
		opt.Mode = "foreground"
	}
	if opt.Log == nil {
		opt.Log = io.Discard
	}
	opt.Bridge.Command = []string{"test"}
	ctx, cancel := context.WithCancel(context.Background())
	done, finished := make(chan error, 1), make(chan struct{})
	go func() { done <- serve(ctx, opt); close(finished) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("session did not stop")
		}
	})
	waitReady(t, opt.Name, done)
	return done
}

func waitReady(t *testing.T, name string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("session ended: %v", err)
		default:
		}
		if st, err := Status(name); err == nil && st.State == "ready" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("session did not become ready")
}

// client connects an MCP client to the named session.
func client(t *testing.T, name string, copt *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	conn, err := Dial(name)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, copt).Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestControl(t *testing.T) {
	dir := shortDir(t)
	startServe(t, serveOptions{Name: "a", Remote: hostShell(t, policy.Options{}), Idle: time.Hour})
	st, err := Status("a")
	if err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() || st.Mode != "foreground" || st.Confirm != "refuse" || st.IdleTimeout != "1h0m0s" || st.Socket != filepath.Join(dir, "a.sock") {
		t.Errorf("status = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.json")); err != nil {
		t.Errorf("state file: %v", err)
	}
	cs := client(t, "a", nil)
	if st, _ := Status("a"); st.Clients != 1 {
		t.Errorf("clients = %d, want 1", st.Clients)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sys_get_info"})
	if err != nil || res.IsError {
		t.Fatalf("sys_get_info: %v %+v", err, res)
	}

	err = serve(context.Background(), serveOptions{Name: "a", Remote: hostShell(t, policy.Options{}), Log: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second session with the same name: %v", err)
	}
	states, err := list()
	if err != nil || len(states) != 1 || states[0].Name != "a" {
		t.Errorf("list = %+v, %v", states, err)
	}
	if err := stop("a", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := Status("a"); err == nil {
		t.Error("session still answers after stop")
	}
	for _, f := range []string{"a.sock", "a.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s remains after stop", f)
		}
	}
}

func TestNames(t *testing.T) {
	shortDir(t)
	for _, n := range []string{"", ".", "..", "a/b", `a\b`, "a b", strings.Repeat("x", 33)} {
		if _, err := Dial(n); err == nil || errors.As(err, new(*NotRunningError)) {
			t.Errorf("name %q: %v", n, err)
		}
	}
	t.Setenv(EnvName, "")
	if got := Name(""); got != DefaultName {
		t.Errorf("Name() = %q", got)
	}
	t.Setenv(EnvName, "web")
	if got := Name(""); got != "web" {
		t.Errorf("Name() with %s = %q", EnvName, got)
	}
	if got := Name("db"); got != "db" {
		t.Errorf("Name(db) = %q", got)
	}
}

func TestStaleSocket(t *testing.T) {
	dir := shortDir(t)
	// A socket file without a listener, as a killed session leaves it.
	ln, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	ln.Close()
	if _, err := os.Stat(filepath.Join(dir, "s.sock")); err != nil {
		t.Fatalf("socket file: %v", err)
	}
	startServe(t, serveOptions{Name: "s", Remote: hostShell(t, policy.Options{})})
}

func TestDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows applies an owner-only DACL instead of checking")
	}
	dir := shortDir(t)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial("x"); err == nil || !strings.Contains(err.Error(), "other users") {
		t.Errorf("Dial with a directory other users can read: %v", err)
	}
}

func TestIdleTimeout(t *testing.T) {
	shortDir(t)
	done := startServe(t, serveOptions{Name: "i", Remote: hostShell(t, policy.Options{}), Idle: 500 * time.Millisecond})
	cs := client(t, "i", nil)
	time.Sleep(time.Second)
	select {
	case err := <-done:
		t.Fatalf("session ended while a client was connected: %v", err)
	default:
	}
	cs.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("idle end: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end after the idle timeout")
	}
}

func removeCall(t *testing.T, cs *mcp.ClientSession, p *mcp.CallToolParams) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), p)
	if err != nil {
		return err.Error(), false
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String(), false
	}
	return "", true
}

func TestConfirmRefusedWithoutTerminal(t *testing.T) {
	shortDir(t)
	startServe(t, serveOptions{Name: "r", Remote: hostShell(t, policy.Options{Confirm: "fs_remove"})})
	f := filepath.Join(t.TempDir(), "x")
	os.WriteFile(f, nil, 0o644)
	cs := client(t, "r", nil)
	msg, ok := removeCall(t, cs, &mcp.CallToolParams{Name: "fs_remove", Arguments: map[string]any{"path": f}})
	if ok || !strings.Contains(msg, "cannot ask") {
		t.Errorf("fs_remove without a terminal: ok=%v %q", ok, msg)
	}
	// Answers a client attaches are not passed to the target.
	p := &mcp.CallToolParams{Name: "fs_remove", Arguments: map[string]any{"path": f},
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": true}}}, RequestState: "forged"}
	if _, ok := removeCall(t, cs, p); ok {
		t.Error("a client's own answer was accepted")
	}
	if _, err := os.Stat(f); err != nil {
		t.Error("file was removed without the user's confirmation")
	}
}

// terminal stands in for the user at the terminal of a foreground session.
type terminal struct {
	in  *io.PipeWriter
	out *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newTerminal(t *testing.T) (*terminal, *prompter) {
	r, w := io.Pipe()
	t.Cleanup(func() { w.Close() })
	term := &terminal{in: w, out: &syncBuffer{}}
	return term, newPrompter("c", r, term.out)
}

// answer waits for the next question and types line.
func (term *terminal) answer(t *testing.T, questions int, line string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for strings.Count(term.out.String(), "Allow? [y/N]") < questions {
		if time.Now().After(deadline) {
			t.Fatalf("no question; terminal shows %q", term.out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	io.WriteString(term.in, line+"\n")
}

func TestConfirmInTerminal(t *testing.T) {
	shortDir(t)
	term, p := newTerminal(t)
	startServe(t, serveOptions{Name: "c", Remote: hostShell(t, policy.Options{Confirm: "fs_remove"}), Confirm: p})
	if st, _ := Status("c"); st.Confirm != "terminal" {
		t.Errorf("confirm = %q", st.Confirm)
	}
	cs := client(t, "c", nil)
	f := filepath.Join(t.TempDir(), "x")
	os.WriteFile(f, nil, 0o644)
	call := &mcp.CallToolParams{Name: "fs_remove", Arguments: map[string]any{"path": f}}

	result := make(chan bool, 1)
	go func() { _, ok := removeCall(t, cs, call); result <- ok }()
	term.answer(t, 1, "n")
	if <-result {
		t.Error("declined call succeeded")
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("file was removed after the user declined")
	}
	if !strings.Contains(term.out.String(), "fs_remove") {
		t.Errorf("question does not name the tool: %q", term.out.String())
	}

	go func() { _, ok := removeCall(t, cs, call); result <- ok }()
	term.answer(t, 2, "y")
	if !<-result {
		t.Error("approved call failed")
	}
	if _, err := os.Stat(f); err == nil {
		t.Error("file was not removed after the user approved")
	}
}

func TestConfirmWithdrawnOnCancel(t *testing.T) {
	shortDir(t)
	term, p := newTerminal(t)
	startServe(t, serveOptions{Name: "c", Remote: hostShell(t, policy.Options{Confirm: "fs_remove"}), Confirm: p})
	cs := client(t, "c", nil)
	f := filepath.Join(t.TempDir(), "x")
	os.WriteFile(f, nil, 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "fs_remove", Arguments: map[string]any{"path": f}})
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(term.out.String(), "Allow?") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(term.out.String(), "withdrawn") {
		if time.Now().After(deadline) {
			t.Fatalf("question was not withdrawn: %q", term.out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A late answer does not approve anything.
	io.WriteString(term.in, "y\n")
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(f); err != nil {
		t.Error("file was removed by an answer to a withdrawn question")
	}
}

// blockingTarget is a target with a tool that reports progress and waits until cancelled.
func blockingTarget(t *testing.T, started chan<- string, cancelled chan<- string) mcp.Transport {
	s := mcp.NewServer(&mcp.Implementation{Name: "host-shell", Version: "test"}, nil)
	type in struct {
		ID string `json:"id"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "wait"}, func(ctx context.Context, req *mcp.CallToolRequest, in in) (*mcp.CallToolResult, any, error) {
		if tok := req.Params.GetProgressToken(); tok != nil {
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: tok, Message: in.ID})
		}
		started <- in.ID
		<-ctx.Done()
		cancelled <- in.ID
		return nil, nil, ctx.Err()
	})
	return serveTransport(t, s, func() {})
}

func TestProgressRoutingAndCancel(t *testing.T) {
	shortDir(t)
	started, cancelled := make(chan string, 2), make(chan string, 2)
	startServe(t, serveOptions{Name: "p", Remote: blockingTarget(t, started, cancelled), Bridge: bridge.Options{NoTransfer: true}})

	// Two clients use the same progress token; each must get only its own progress.
	var got [2]atomic.Value
	cancels := make([]context.CancelFunc, 2)
	for i, id := range []string{"one", "two"} {
		cs := client(t, "p", &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			if req.Params.ProgressToken != "same" && req.Params.ProgressToken != any("same") {
				t.Errorf("progress token %v", req.Params.ProgressToken)
			}
			got[i].Store(req.Params.Message)
		}})
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		go func() {
			p := &mcp.CallToolParams{Name: "wait", Arguments: map[string]any{"id": id}}
			p.SetProgressToken("same")
			_, _ = cs.CallTool(ctx, p)
		}()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("call did not reach the target")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for (got[0].Load() == nil || got[1].Load() == nil) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got[0].Load() != "one" || got[1].Load() != "two" {
		t.Errorf("progress = %v, %v", got[0].Load(), got[1].Load())
	}
	cancels[1]()
	select {
	case id := <-cancelled:
		if id != "two" {
			t.Errorf("cancelled %q, want two", id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not reach the target")
	}
	cancels[0]()
	<-cancelled
}

func TestClientDisconnectCancels(t *testing.T) {
	shortDir(t)
	started, cancelled := make(chan string, 1), make(chan string, 1)
	startServe(t, serveOptions{Name: "k", Remote: blockingTarget(t, started, cancelled), Bridge: bridge.Options{NoTransfer: true}})
	conn, err := Dial("k")
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "wait", Arguments: map[string]any{"id": "k"}})
	}()
	<-started
	// A killed host-shell call closes its connection without a cancellation notice.
	conn.Close()
	select {
	case <-cancelled:
	case <-time.After(10 * time.Second):
		t.Fatal("closing the connection did not cancel the call on the target")
	}
}

func TestTargetDisconnect(t *testing.T) {
	shortDir(t)
	s := mcp.NewServer(&mcp.Implementation{Name: "host-shell", Version: "test"}, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := startServe(t, serveOptions{Name: "d", Remote: ct, Bridge: bridge.Options{NoTransfer: true}})
	ss.Close()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "disconnected") {
			t.Errorf("serve = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end when the target disconnected")
	}
}
