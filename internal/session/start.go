package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/bridge"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// serveOptions configures serve.
type serveOptions struct {
	Name    string
	Bridge  bridge.Options
	Remote  mcp.Transport // starts or reaches host-shell on the target
	Mode    string        // foreground or detached, for State
	Confirm *prompter     // nil refuses tools that need confirmation
	Idle    time.Duration // 0 keeps the session without clients
	Log     io.Writer     // diagnostics and the ready line
}

// server is a running session.
type server struct {
	opt   serveOptions
	ln    net.Listener
	ready chan struct{}
	relay *bridge.Relay // set before ready is closed
	done  chan string   // receives the reason the session should end
	idle  *time.Timer

	mu    sync.Mutex
	state State
}

// serve runs a session until ctx ends, a client asks it to stop, it stays idle, or the target
// disconnects. Only a target disconnect returns an error.
func serve(ctx context.Context, opt serveOptions) error {
	sock, statePath, _, err := paths(opt.Name)
	if err != nil {
		return err
	}
	if conn, err := Dial(opt.Name); err == nil {
		conn.Close()
		return fmt.Errorf("session %q is already running; stop it with: host-shell session stop --name %s", opt.Name, opt.Name)
	}
	// A socket left by a session that ended abruptly no longer accepts connections.
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sock, err)
	}
	s := &server{opt: opt, ln: ln, ready: make(chan struct{}), done: make(chan string, 1)}
	s.state = State{
		Name: opt.Name, State: "connecting", PID: os.Getpid(), Started: time.Now().UTC().Truncate(time.Second),
		Mode: opt.Mode, Confirm: "refuse", Version: version.Version, Command: opt.Bridge.Command, Socket: sock,
	}
	if opt.Confirm != nil {
		s.state.Confirm = "terminal"
	}
	if opt.Idle > 0 {
		s.state.IdleTimeout = opt.Idle.String()
	}
	defer func() {
		ln.Close()
		_ = os.Remove(sock)
		_ = os.Remove(statePath)
	}()
	s.writeState(statePath)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.accept(ctx)

	local := bridge.Local{Confirm: refuseConfirm}
	if opt.Confirm != nil {
		local.Confirm = opt.Confirm.confirm
	}
	relay, err := bridge.Open(ctx, opt.Bridge, opt.Remote, local)
	if err != nil {
		return err
	}
	defer relay.Close()
	s.relay = relay
	s.mu.Lock()
	s.state.State, s.state.RemoteVersion = "ready", relay.RemoteVersion()
	if opt.Idle > 0 {
		s.idle = time.AfterFunc(opt.Idle, func() { s.end("no client for " + opt.Idle.String()) })
		if s.state.Clients > 0 {
			s.idle.Stop()
		}
	}
	s.mu.Unlock()
	s.writeState(statePath)
	close(s.ready)
	fmt.Fprintf(opt.Log, "host-shell: session %q is ready (target host-shell %s). Use host-shell call and host-shell tools; stop it with: host-shell session stop --name %s\n", opt.Name, relay.RemoteVersion(), opt.Name)

	remote := make(chan error, 1)
	go func() { remote <- relay.Wait() }()
	select {
	case <-ctx.Done():
		fmt.Fprintf(opt.Log, "host-shell: session %q stopped\n", opt.Name)
		return nil
	case reason := <-s.done:
		fmt.Fprintf(opt.Log, "host-shell: session %q stopped: %s\n", opt.Name, reason)
		return nil
	case err := <-remote:
		return fmt.Errorf("session %q ended: %w", opt.Name, err)
	}
}

// refuseConfirm answers confirmation requests in a session without a terminal.
func refuseConfirm(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	return nil, errors.New("this tool needs the user's confirmation, but this session cannot ask: it was started with --detach or without a terminal. Ask the user to run host-shell session start in the foreground in a terminal, where the session asks them")
}

func (s *server) end(reason string) {
	select {
	case s.done <- reason:
	default:
	}
}

func (s *server) writeState(path string) {
	s.mu.Lock()
	b, _ := json.MarshalIndent(s.state, "", "  ")
	s.mu.Unlock()
	tmp := path + ".tmp"
	if os.WriteFile(tmp, append(b, '\n'), 0600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// clients adjusts the number of connected MCP clients and the idle timer.
func (s *server) clients(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Clients += delta
	if s.idle == nil {
		return
	}
	if s.state.Clients == 0 {
		s.idle.Reset(s.opt.Idle)
	} else {
		s.idle.Stop()
	}
}

func (s *server) accept(ctx context.Context) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(ctx, conn)
	}
}

// handle serves one connection: a control request or an MCP session.
func (s *server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	first, err := br.ReadBytes('\n')
	if err != nil {
		return
	}
	var req map[string]any
	if json.Unmarshal(first, &req) == nil {
		if op, ok := req[controlKey].(string); ok {
			s.control(conn, op)
			return
		}
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return
	}
	s.clients(1)
	defer s.clients(-1)
	t := &mcp.IOTransport{Reader: readCloser{io.MultiReader(bytes.NewReader(first), br), conn}, Writer: conn}
	ss, err := s.relay.Connect(ctx, t)
	if err != nil {
		return
	}
	_ = ss.Wait()
}

func (s *server) control(conn net.Conn, op string) {
	switch op {
	case "status":
		s.mu.Lock()
		st := s.state
		s.mu.Unlock()
		_ = writeJSONLine(conn, st)
	case "stop":
		_ = writeJSONLine(conn, map[string]string{"state": "stopping"})
		s.end("stop requested")
	default:
		_ = writeJSONLine(conn, map[string]string{"error": fmt.Sprintf("unknown session control request %q", op)})
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}
