package forward

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

const establishTimeout = 30 * time.Second

// Event is a connection result in forward_status.
type Event struct {
	Seq          uint64 `json:"seq"`
	Time         string `json:"time"`
	ForwardID    uint64 `json:"forward_id"`
	ConnectionID uint64 `json:"connection_id,omitempty"`
	Direction    string `json:"direction"`
	Host         string `json:"host,omitempty"`
	Port         int    `json:"port,omitempty"`
	Status       string `json:"status" jsonschema:"allowed, denied or failed"`
	Reason       string `json:"reason"`
}

// Info describes one forward.
type Info struct {
	ForwardID         uint64 `json:"forward_id"`
	Direction         string `json:"direction" jsonschema:"local, remote or socks"`
	Persistent        bool   `json:"persistent" jsonschema:"defined by bridge options; forward_close cannot close it"`
	ListenAddress     string `json:"listen_address" jsonschema:"on this machine for local and socks, on the target for remote"`
	ListenPort        int    `json:"listen_port"`
	Host              string `json:"host,omitempty" jsonschema:"destination (local: as seen from the target; remote: as seen from this machine)"`
	Port              int    `json:"port,omitempty"`
	ActiveConnections int    `json:"active_connections"`
}

// OpenInput is the input of forward_open.
type OpenInput struct {
	Direction     string `json:"direction" jsonschema:"local (listen here, connect from the target), remote (listen on the target, connect from here) or socks (SOCKS5 here, connect from the target)"`
	ListenAddress string `json:"listen_address,omitempty" jsonschema:"127.0.0.1 (default) or ::1"`
	ListenPort    int    `json:"listen_port,omitempty" jsonschema:"listen port; 0 (default) picks a free port"`
	Host          string `json:"host,omitempty" jsonschema:"destination host; required for local and remote, not allowed for socks"`
	Port          int    `json:"port,omitempty" jsonschema:"destination port; required for local and remote, not allowed for socks"`
}

// CloseInput is the input of forward_close.
type CloseInput struct {
	ForwardID uint64 `json:"forward_id"`
}

// StatusInput is the input of forward_status.
type StatusInput struct {
	AfterSeq uint64 `json:"after_seq,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// Status is the output of forward_status.
type Status struct {
	State             string   `json:"state"`
	Forwards          []Info   `json:"forwards"`
	AllowLocal        []string `json:"allow_local" jsonschema:"--forward-allow-local rules for tool-created local forwards and SOCKS destinations"`
	AllowRemote       []string `json:"allow_remote" jsonschema:"--forward-allow-remote rules for tool-created remote forwards"`
	ActiveConnections int      `json:"active_connections" jsonschema:"open channel streams, HTTP proxy included"`
	Events            []Event  `json:"events"`
	NextSeq           uint64   `json:"next_seq"`
	Truncated         bool     `json:"truncated"`
}

type forward struct {
	id         uint64
	spec       Spec
	persistent bool
	listener   net.Listener // local and SOCKS forwards
	mu         sync.Mutex
	conns      map[net.Conn]struct{} // every socket and stream to close with the forward
	active     int                   // forwarded connections
	closed     bool
}

func (f *forward) track(c net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	f.conns[c] = struct{}{}
	return true
}
func (f *forward) untrack(c net.Conn) {
	f.mu.Lock()
	delete(f.conns, c)
	f.mu.Unlock()
}
func (f *forward) count(delta int) {
	f.mu.Lock()
	f.active += delta
	f.mu.Unlock()
}

// close stops listening and ends every connection of the forward.
func (f *forward) close() {
	f.mu.Lock()
	f.closed = true
	conns := f.conns
	f.conns = map[net.Conn]struct{}{}
	f.mu.Unlock()
	if f.listener != nil {
		f.listener.Close()
	}
	for c := range conns {
		c.Close()
	}
}

// Manager is the bridge side: it owns listeners, tool-created forwards and diagnostics.
type Manager struct {
	allowLocal, allowRemote policy.NameRules
	history                 channel.History[Event]
	// dial connects remote forwards to their destinations on this machine, never through a proxy.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)

	listenMu sync.Mutex // keeps remote forward IDs increasing in listen order
	mu       sync.Mutex
	next     uint64
	pending  int
	forwards map[uint64]*forward
	conn     *channel.Conn
	connSeq  uint64
	shutdown bool
}

// NewManager validates persistent forwards and tool rules.
func NewManager(specs []Spec, allowLocal, allowRemote []string) (*Manager, error) {
	local, err := policy.ParseNameRules("--forward-allow-local", allowLocal)
	if err != nil {
		return nil, err
	}
	remote, err := policy.ParseNameRules("--forward-allow-remote", allowRemote)
	if err != nil {
		return nil, err
	}
	if len(specs) > channel.MaxForwards {
		return nil, fmt.Errorf("at most %d forwards per session", channel.MaxForwards)
	}
	m := &Manager{allowLocal: local, allowRemote: remote, forwards: map[uint64]*forward{}, dial: (&net.Dialer{Timeout: establishTimeout}).DialContext}
	for _, s := range specs {
		if s.Direction == SOCKS && len(local) == 0 {
			return nil, errors.New("--forward-socks requires --forward-allow-local (use '*' to allow every destination)")
		}
		m.next++
		m.forwards[m.next] = &forward{id: m.next, spec: s, persistent: true, conns: map[net.Conn]struct{}{}}
	}
	return m, nil
}

// Enabled reports whether any forward or rule was given.
func (m *Manager) Enabled() bool { return len(m.forwards) > 0 || m.Tools() }

// Tools reports whether forward_open and forward_close are available.
func (m *Manager) Tools() bool { return len(m.allowLocal) > 0 || len(m.allowRemote) > 0 }

func (m *Manager) wants(directions ...string) bool {
	for _, f := range m.forwards {
		if slices.Contains(directions, f.spec.Direction) {
			return true
		}
	}
	return false
}

// Register requests the forwarding directions on the bridge's channel options.
func (m *Manager) Register(o *channel.Options) {
	if len(m.allowLocal) > 0 || m.wants(Local, SOCKS) {
		o.Enable(channel.ForwardLocal)
	}
	if len(m.allowRemote) > 0 || m.wants(Remote) {
		o.Enable(channel.ForwardRemote)
		o.Handle(channel.KindForward, m.openRemote)
	}
	o.OnReport(m.report)
	o.Setup = m.setup
	o.OnAttach(m.attach)
}

// Bind listens persistent local and SOCKS forwards; call it before starting the target.
func (m *Manager) Bind() error {
	for _, f := range m.sorted() {
		if f.spec.Direction == Remote {
			continue
		}
		l, err := listen(f.spec)
		if err != nil {
			m.Close()
			return fmt.Errorf("--forward-%s on %s: %w", f.spec.Direction, f.spec.Address(), err)
		}
		f.listener = l
		f.spec.Port = l.Addr().(*net.TCPAddr).Port
	}
	return nil
}

func listen(s Spec) (net.Listener, error) {
	return net.Listen("tcp", s.Address())
}

func (m *Manager) setup(ctx context.Context, c *channel.Conn) error {
	m.listenMu.Lock()
	defer m.listenMu.Unlock()
	for _, f := range m.sorted() {
		if f.spec.Direction != Remote {
			continue
		}
		port, err := c.Listen(ctx, f.id, f.spec.Bind, f.spec.Port)
		if err != nil {
			return fmt.Errorf("--forward-remote on target %s: listen failed: %v", f.spec.Address(), err)
		}
		f.spec.Port = port
	}
	return nil
}

func (m *Manager) attach(c *channel.Conn) {
	m.mu.Lock()
	m.conn = c
	m.mu.Unlock()
	for _, f := range m.sorted() {
		if f.listener != nil {
			go m.accept(f)
		}
	}
	go func() { <-c.Done(); m.Close() }()
}

// Close ends every forward; it runs when the session ends.
func (m *Manager) Close() {
	m.mu.Lock()
	m.shutdown = true
	fs := make([]*forward, 0, len(m.forwards))
	for _, f := range m.forwards {
		fs = append(fs, f)
	}
	m.mu.Unlock()
	for _, f := range fs {
		f.close()
	}
}

func (m *Manager) sorted() []*forward {
	m.mu.Lock()
	defer m.mu.Unlock()
	fs := make([]*forward, 0, len(m.forwards))
	for _, f := range m.forwards {
		fs = append(fs, f)
	}
	slices.SortFunc(fs, func(a, b *forward) int { return int(a.id) - int(b.id) })
	return fs
}

func (m *Manager) record(f *forward, conn uint64, host string, port int, status, reason string) {
	// Reasons are fixed strings; forwarded data is never recorded.
	m.history.Add(func(seq uint64) Event {
		return Event{Seq: seq, Time: time.Now().Format(time.RFC3339), ForwardID: f.id, ConnectionID: conn, Direction: f.spec.Direction, Host: host, Port: port, Status: status, Reason: reason}
	})
	log.Printf("forward: id=%d connection=%d direction=%s host=%q port=%d status=%s reason=%s", f.id, conn, f.spec.Direction, host, port, status, reason)
}

func (m *Manager) connection() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connSeq++
	return m.connSeq
}

func (m *Manager) lookup(id uint64) *forward {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forwards[id]
}

// report records connection limit notices for remote forwards.
func (m *Manager) report(kind string, f channel.Frame) {
	if kind != "limit" || f.Forward == 0 {
		return
	}
	if fw := m.lookup(f.Forward); fw != nil {
		m.record(fw, 0, fw.spec.Host, fw.spec.HostPort, "denied", "connection limit reached")
	}
}

func (m *Manager) accept(f *forward) {
	for {
		c, err := f.listener.Accept()
		if err != nil {
			return
		}
		go m.serve(f, c)
	}
}

func (m *Manager) serve(f *forward, c net.Conn) {
	defer c.Close()
	if !f.track(c) {
		return
	}
	defer f.untrack(c)
	f.count(1)
	defer f.count(-1)
	id := m.connection()
	if f.spec.Direction == SOCKS {
		m.socks(f, id, c)
		return
	}
	remote, err := m.open(f, channel.KindForward, f.spec.Host, f.spec.HostPort)
	if err != nil {
		m.record(f, id, f.spec.Host, f.spec.HostPort, status(channel.Code(err)), reason(channel.Code(err)))
		return
	}
	m.record(f, id, f.spec.Host, f.spec.HostPort, "allowed", "connection established")
	if !f.track(remote) {
		remote.Close()
		return
	}
	defer f.untrack(remote)
	pipe(c, remote)
}

func (m *Manager) open(f *forward, kind, host string, port int) (net.Conn, error) {
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return nil, &channel.Error{Code: channel.CodeClosed}
	}
	ctx, cancel := context.WithTimeout(context.Background(), establishTimeout)
	defer cancel()
	return conn.Dial(ctx, kind, host, port, f.id)
}

// openRemote connects a remote forward accepted on the target to its registered destination.
func (m *Manager) openRemote(ctx context.Context, in *channel.Incoming) {
	f := m.lookup(in.Forward)
	if f == nil || f.spec.Direction != Remote {
		in.Reject(channel.CodeClosed)
		return
	}
	id := m.connection()
	dialCtx, cancel := context.WithTimeout(ctx, establishTimeout)
	defer cancel()
	external, err := m.dial(dialCtx, "tcp", f.spec.destination())
	if err != nil {
		code := dialCode(err)
		m.record(f, id, f.spec.Host, f.spec.HostPort, "failed", reason(code))
		in.Reject(code)
		return
	}
	defer external.Close()
	if !f.track(external) {
		in.Reject(channel.CodeClosed)
		return
	}
	defer f.untrack(external)
	f.count(1)
	defer f.count(-1)
	stream, err := in.Accept()
	if err != nil {
		return
	}
	defer stream.Close()
	m.record(f, id, f.spec.Host, f.spec.HostPort, "allowed", "connection established")
	pipe(stream, external)
}

// Open creates a forward for forward_open.
func (m *Manager) Open(ctx context.Context, in OpenInput) (Info, error) {
	s := Spec{Direction: in.Direction, Port: in.ListenPort, Host: in.Host, HostPort: in.Port}
	var rules policy.NameRules
	var option string
	switch in.Direction {
	case Local, SOCKS:
		rules, option = m.allowLocal, "--forward-allow-local"
	case Remote:
		rules, option = m.allowRemote, "--forward-allow-remote"
	default:
		return Info{}, errors.New("direction must be local, remote or socks")
	}
	if len(rules) == 0 {
		return Info{}, fmt.Errorf("%s forwards require bridge %s", in.Direction, option)
	}
	var err error
	if s.Bind, err = listenAddress(in.ListenAddress); err != nil {
		return Info{}, err
	}
	if s.Port < 0 || s.Port > 65535 {
		return Info{}, errors.New("listen_port must be 0 to 65535")
	}
	if in.Direction == SOCKS {
		if in.Host != "" || in.Port != 0 {
			return Info{}, errors.New("host and port are not allowed for socks; SOCKS clients choose destinations per connection")
		}
	} else {
		if s.Host, err = parseHost(bracket(in.Host)); err != nil {
			return Info{}, err
		}
		if s.HostPort < 1 || s.HostPort > 65535 {
			return Info{}, errors.New("port must be 1 to 65535")
		}
		if !rules.Allow(s.Host, s.HostPort) {
			return Info{}, fmt.Errorf("destination %s is not allowed by %s (allowed: %v)", s.destination(), option, []string(rules))
		}
	}
	if err = m.reserve(); err != nil {
		return Info{}, err
	}
	defer m.release()
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return Info{}, errors.New("session is not ready")
	}
	f := &forward{spec: s, conns: map[net.Conn]struct{}{}}
	if in.Direction == Remote {
		m.listenMu.Lock()
		defer m.listenMu.Unlock()
		f.id = m.allocate()
		port, err := conn.Listen(ctx, f.id, s.Bind, s.Port)
		if err != nil {
			return Info{}, fmt.Errorf("listen on target %s: %v", s.Address(), err)
		}
		f.spec.Port = port
	} else {
		l, err := listen(s)
		if err != nil {
			return Info{}, fmt.Errorf("listen on %s: %w", s.Address(), err)
		}
		f.listener = l
		f.spec.Port = l.Addr().(*net.TCPAddr).Port
		f.id = m.allocate()
	}
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		f.close()
		return Info{}, errors.New("session ended")
	}
	m.forwards[f.id] = f
	m.mu.Unlock()
	if f.listener != nil {
		go m.accept(f)
	}
	return f.info(), nil
}

func bracket(h string) string {
	if ip := net.ParseIP(h); ip != nil && ip.To4() == nil {
		return "[" + h + "]"
	}
	return h
}

func (m *Manager) allocate() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	return m.next
}

func (m *Manager) reserve() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.forwards)+m.pending >= channel.MaxForwards {
		return fmt.Errorf("at most %d forwards per session; close one with forward_close", channel.MaxForwards)
	}
	m.pending++
	return nil
}

func (m *Manager) release() {
	m.mu.Lock()
	m.pending--
	m.mu.Unlock()
}

// CloseForward removes a tool-created forward and ends its connections.
func (m *Manager) CloseForward(id uint64) error {
	m.mu.Lock()
	f := m.forwards[id]
	conn := m.conn
	switch {
	case f == nil:
		m.mu.Unlock()
		return fmt.Errorf("unknown forward_id %d", id)
	case f.persistent:
		m.mu.Unlock()
		return fmt.Errorf("forward %d is defined by bridge options and cannot be closed", id)
	}
	delete(m.forwards, id)
	m.mu.Unlock()
	if f.spec.Direction == Remote && conn != nil {
		conn.Unlisten(id)
	}
	f.close()
	return nil
}

func (f *forward) info() Info {
	f.mu.Lock()
	active := f.active
	f.mu.Unlock()
	return Info{ForwardID: f.id, Direction: f.spec.Direction, Persistent: f.persistent, ListenAddress: f.spec.Bind, ListenPort: f.spec.Port, Host: f.spec.Host, Port: f.spec.HostPort, ActiveConnections: active}
}

// Status returns forwards, rules and recent connection results.
func (m *Manager) Status(in StatusInput) (Status, error) {
	events, next, truncated, err := m.history.Page(in.AfterSeq, in.Limit)
	if err != nil {
		return Status{}, err
	}
	o := Status{State: "disconnected", Forwards: []Info{}, AllowLocal: append([]string{}, m.allowLocal...), AllowRemote: append([]string{}, m.allowRemote...), Events: events, NextSeq: next, Truncated: truncated}
	for _, f := range m.sorted() {
		o.Forwards = append(o.Forwards, f.info())
	}
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn != nil {
		select {
		case <-conn.Done():
		default:
			o.State = "ready"
			o.ActiveConnections = conn.Active()
		}
	}
	return o, nil
}

// Describe summarizes a forward for the bridge's instructions.
func (i Info) Describe() string {
	switch i.Direction {
	case SOCKS:
		return fmt.Sprintf("socks %s", net.JoinHostPort(i.ListenAddress, strconv.Itoa(i.ListenPort)))
	case Remote:
		return fmt.Sprintf("remote target %s -> %s", net.JoinHostPort(i.ListenAddress, strconv.Itoa(i.ListenPort)), net.JoinHostPort(i.Host, strconv.Itoa(i.Port)))
	}
	return fmt.Sprintf("local %s -> target %s", net.JoinHostPort(i.ListenAddress, strconv.Itoa(i.ListenPort)), net.JoinHostPort(i.Host, strconv.Itoa(i.Port)))
}
