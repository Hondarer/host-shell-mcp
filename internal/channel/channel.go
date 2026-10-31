// Package channel multiplexes TCP-like streams beside MCP on the bridge's SSH stdio session.
//
// Peers exchange host-shell/channel/* JSON-RPC notifications that never reach the SDK.
// Negotiation runs before MCP initialization: the bridge sends hello with the features it
// requests, the target answers hello with the features it permits, the bridge may then set
// up persistent remote forwards with listen/listen_result, and both sides exchange ready.
// A requested feature the target does not permit, or an HTTP proxy enabled on one side only,
// fails both peers at startup.
//
// Either side may open streams. The bridge numbers its streams with odd IDs and the target
// with even IDs, so concurrent opens never collide. The target can only ask the bridge to dial
// remote forwards the bridge registered with listen; it never names a destination itself.
package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Prefix is the method prefix of channel notifications.
const Prefix = "host-shell/channel/"

// legacyPrefix identifies peers built before the channel was shared by all features.
const legacyPrefix = "host-shell/proxy/"

const version = 1
const frameSize = 16 * 1024
const windowSize = 64 * 1024
const negotiationTimeout = 30 * time.Second

// MaxStreams bounds concurrent streams per session across all features, pending opens included.
const MaxStreams = 64

// MaxForwards bounds listeners per session, persistent and tool-created alike.
const MaxForwards = 16

// Features negotiated in hello.
const (
	HTTPProxy     = "http_proxy"
	ForwardLocal  = "forward_local"
	ForwardRemote = "forward_remote"
)

// Stream kinds carried by open.
const (
	KindHTTP    = "http"    // target to bridge: HTTP proxy request
	KindConnect = "connect" // target to bridge: HTTP proxy CONNECT
	KindForward = "forward" // bridge to target: local forward; target to bridge: remote forward
	KindSOCKS   = "socks"   // bridge to target: SOCKS5 CONNECT
)

// Fixed failure codes in open_result and listen_result. Raw network errors never cross the channel.
const (
	CodeNotAllowed  = "not_allowed"
	CodeLimit       = "limit"
	CodeRefused     = "refused"
	CodeUnreachable = "unreachable"
	CodeTimeout     = "timeout"
	CodeFailed      = "failed"
	CodeClosed      = "closed"
	CodeInUse       = "address_in_use"
	CodePermission  = "permission_denied"
)

// Error is a failure reported with a fixed code.
type Error struct{ Code string }

func (e *Error) Error() string { return strings.ReplaceAll(e.Code, "_", " ") }

// ErrLimit is returned by Dial when this side has no stream left; the peer was not asked.
var ErrLimit = &Error{Code: CodeLimit}

// Code returns the fixed code of err.
func Code(err error) string {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout
	}
	return CodeFailed
}

// Frame is the payload of a channel notification.
type Frame struct {
	ID       uint64   `json:"id,omitempty"`
	Version  int      `json:"version,omitempty"`
	Features []string `json:"features,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Forward  uint64   `json:"forward,omitempty"`
	Bind     string   `json:"bind,omitempty"`
	Data     string   `json:"data,omitempty"`
	Bytes    int      `json:"bytes,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Options configures one side of the channel. Features are requested on the bridge and permitted on the target.
type Options struct {
	Features []string
	// open handles peer-initiated streams by kind; each call runs in its own goroutine and must Accept or Reject.
	open map[string]func(context.Context, *Incoming)
	// report receives limit and auth_failed notices from the target (bridge only).
	report []func(kind string, f Frame)
	attach []func(*Conn)
	// Listen opens a remote forward listener (target only) and returns the bound port.
	Listen func(c *Conn, forward uint64, bind string, port int) (int, error)
	// Unlisten closes a remote forward listener (target only).
	Unlisten func(forward uint64)
	// Setup runs on the bridge after hello and before ready, for example to listen persistent remote forwards.
	Setup func(ctx context.Context, c *Conn) error
}

// Enable adds a feature.
func (o *Options) Enable(feature string) {
	if !slices.Contains(o.Features, feature) {
		o.Features = append(o.Features, feature)
	}
}

// Handle registers the handler for peer-initiated streams of kind.
func (o *Options) Handle(kind string, fn func(context.Context, *Incoming)) {
	if o.open == nil {
		o.open = map[string]func(context.Context, *Incoming){}
	}
	o.open[kind] = fn
}

// OnReport adds a receiver of target notices.
func (o *Options) OnReport(fn func(kind string, f Frame)) { o.report = append(o.report, fn) }

// OnAttach adds a callback run once the channel is ready.
func (o *Options) OnAttach(fn func(*Conn)) { o.attach = append(o.attach, fn) }

type packet struct {
	msg  jsonrpc.Message
	done chan error
}

// Conn is a negotiated channel; it implements mcp.Connection for the SDK.
type Conn struct {
	base       mcp.Connection
	ctx        context.Context
	cancel     context.CancelFunc
	opt        Options
	bridge     bool
	features   map[string]bool
	high, data chan packet
	messages   chan jsonrpc.Message
	ready      chan struct{}
	readyOnce  sync.Once
	mu         sync.Mutex
	openMu     sync.Mutex
	streams    map[uint64]*stream
	opened     uint64 // own streams opened; guarded by openMu
	lastOwn    atomic.Uint64
	lastPeer   uint64 // reader only
	once       sync.Once

	// Bridge: remote forwards announced with listen (true while listening) and pending results.
	// Target: active listeners. Both: the last forward ID seen, which must increase.
	fwMu        sync.Mutex
	forwards    map[uint64]bool
	listens     map[uint64]chan Frame
	lastForward uint64
}

type transport struct {
	base   mcp.Transport
	opt    Options
	bridge bool
}

// Client wraps the bridge's connection to the target. Use it only when at least one feature is requested.
func Client(base mcp.Transport, opt Options) mcp.Transport {
	return &transport{base: base, opt: opt, bridge: true}
}

// Target wraps the target's stdio connection. It inspects the first message: ordinary MCP passes
// through unchanged unless the HTTP proxy is enabled, which requires the bridge's hello.
func Target(base mcp.Transport, opt Options) mcp.Transport {
	return &transport{base: base, opt: opt}
}

func newConn(ctx context.Context, base mcp.Connection, opt Options, bridge bool) *Conn {
	ctx, cancel := context.WithCancel(ctx)
	return &Conn{base: base, ctx: ctx, cancel: cancel, opt: opt, bridge: bridge, high: make(chan packet, 32), data: make(chan packet, 128), messages: make(chan jsonrpc.Message, 16), ready: make(chan struct{}), streams: map[uint64]*stream{}, forwards: map[uint64]bool{}, listens: map[uint64]chan Frame{}}
}

var errLegacy = errors.New("peer uses an older channel definition (host-shell/proxy); update host-shell on both ends")
var errNoAnswer = errors.New("target closed or did not answer; ensure host-shell is the same version on both ends and the target permits the requested features")

func (t *transport) Connect(ctx context.Context) (mcp.Connection, error) {
	base, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if t.bridge {
		return t.connectBridge(ctx, base)
	}
	return t.connectTarget(ctx, base)
}

func negotiationError(err error) error { return fmt.Errorf("client channel negotiation: %w", err) }

func (t *transport) connectBridge(ctx context.Context, base mcp.Connection) (mcp.Connection, error) {
	c := newConn(ctx, base, t.opt, true)
	timer := time.AfterFunc(negotiationTimeout, func() { base.Close() })
	defer timer.Stop()
	fail := func(err error) (mcp.Connection, error) {
		c.Close()
		return nil, negotiationError(err)
	}
	if writeFrame(ctx, base, "hello", Frame{Version: version, Features: t.opt.Features}) != nil {
		return fail(errNoAnswer)
	}
	msg, err := base.Read(ctx)
	if err != nil {
		return fail(errNoAnswer)
	}
	f, err := expectHello(msg)
	if err != nil {
		return fail(err)
	}
	if err = match(t.opt.Features, f.Features, true); err != nil {
		return fail(err)
	}
	c.features = set(t.opt.Features)
	go c.writer()
	go c.reader()
	if t.opt.Setup != nil {
		if err = t.opt.Setup(ctx, c); err != nil {
			return fail(err)
		}
	}
	if err = c.send("ready", Frame{}); err != nil {
		return fail(errNoAnswer)
	}
	select {
	case <-c.ready:
	case <-c.ctx.Done():
		return fail(errNoAnswer)
	}
	return c.start(), nil
}

func (t *transport) connectTarget(ctx context.Context, base mcp.Connection) (mcp.Connection, error) {
	permitted := set(t.opt.Features)
	// Only the HTTP proxy obliges the bridge to speak first; other features keep plain MCP usable.
	var timer *time.Timer
	if permitted[HTTPProxy] {
		timer = time.AfterFunc(negotiationTimeout, func() { base.Close() })
	}
	stop := func() {
		if timer != nil {
			timer.Stop()
		}
	}
	fail := func(err error) (mcp.Connection, error) {
		stop()
		base.Close()
		return nil, negotiationError(err)
	}
	msg, err := base.Read(ctx)
	if err != nil {
		if permitted[HTTPProxy] {
			return fail(errors.New("bridge did not enable the HTTP proxy (--proxy) required by --client-proxy"))
		}
		stop()
		return &peeked{Connection: base, err: err}, nil
	}
	req, _ := msg.(*jsonrpc.Request)
	switch {
	case req != nil && strings.HasPrefix(req.Method, legacyPrefix):
		return fail(errLegacy)
	case req != nil && strings.HasPrefix(req.Method, Prefix):
		f, err := expectHello(msg)
		if err != nil {
			return fail(err)
		}
		stop()
		timer = time.AfterFunc(negotiationTimeout, func() { base.Close() })
		defer timer.Stop()
		// Answer even on mismatch so the bridge can name the missing option.
		if err = writeFrame(ctx, base, "hello", Frame{Version: version, Features: t.opt.Features}); err != nil {
			return fail(err)
		}
		if err = match(f.Features, t.opt.Features, false); err != nil {
			return fail(err)
		}
		c := newConn(ctx, base, t.opt, false)
		c.features = set(f.Features)
		go c.writer()
		go c.reader()
		select {
		case <-c.ready:
		case <-c.ctx.Done():
			c.Close()
			return nil, negotiationError(errors.New("bridge closed during negotiation"))
		}
		return c.start(), nil
	default:
		if permitted[HTTPProxy] {
			return fail(errors.New("bridge did not enable the HTTP proxy (--proxy) required by --client-proxy"))
		}
		stop()
		return &peeked{Connection: base, first: msg}, nil
	}
}

func (c *Conn) start() *Conn {
	go func() { <-c.ctx.Done(); c.Close() }()
	for _, fn := range c.opt.attach {
		fn(c)
	}
	return c
}

func expectHello(msg jsonrpc.Message) (Frame, error) {
	var f Frame
	req, ok := msg.(*jsonrpc.Request)
	if ok && strings.HasPrefix(req.Method, legacyPrefix) {
		return f, errLegacy
	}
	if !ok || req.Method != Prefix+"hello" || req.ID.IsValid() || json.Unmarshal(req.Params, &f) != nil {
		return f, errors.New("peer did not send channel hello")
	}
	if f.Version != version {
		return f, fmt.Errorf("unsupported channel version %d; update host-shell on both ends", f.Version)
	}
	for _, name := range f.Features {
		if name != HTTPProxy && name != ForwardLocal && name != ForwardRemote {
			return f, fmt.Errorf("unknown channel feature %q; update host-shell on both ends", name)
		}
	}
	return f, nil
}

// match applies the request/permit rules; the HTTP proxy alone must be enabled on both ends.
func match(requested, permitted []string, bridge bool) error {
	req, perm := set(requested), set(permitted)
	type rule struct{ feature, bridgeMsg, targetMsg string }
	for _, r := range []rule{
		{HTTPProxy, "target does not enable the HTTP proxy; start host-shell --mcp with --client-proxy", "bridge requested the HTTP proxy, which requires --client-proxy"},
		{ForwardLocal, "target does not permit local forwarding; start host-shell --mcp with --allow-forward-local", "bridge requested local forwarding, which requires --allow-forward-local"},
		{ForwardRemote, "target does not permit remote forwarding; start host-shell --mcp with --allow-forward-remote", "bridge requested remote forwarding, which requires --allow-forward-remote"},
	} {
		if req[r.feature] && !perm[r.feature] {
			if bridge {
				return errors.New(r.bridgeMsg)
			}
			return errors.New(r.targetMsg)
		}
	}
	if perm[HTTPProxy] && !req[HTTPProxy] {
		if bridge {
			return errors.New("target requires the HTTP proxy (--client-proxy); start the bridge with --proxy")
		}
		return errors.New("bridge did not enable the HTTP proxy (--proxy) required by --client-proxy")
	}
	return nil
}

func set(names []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// peeked replays the first message read while checking for a channel hello.
type peeked struct {
	mcp.Connection
	mu    sync.Mutex
	first jsonrpc.Message
	err   error
}

func (p *peeked) Read(ctx context.Context) (jsonrpc.Message, error) {
	p.mu.Lock()
	m, e := p.first, p.err
	p.first, p.err = nil, nil
	p.mu.Unlock()
	if m != nil || e != nil {
		return m, e
	}
	return p.Connection.Read(ctx)
}

func writeFrame(ctx context.Context, base mcp.Connection, kind string, f Frame) error {
	b, e := json.Marshal(f)
	if e != nil {
		return e
	}
	return base.Write(ctx, &jsonrpc.Request{Method: Prefix + kind, Params: b})
}

// Has reports whether a feature was negotiated.
func (c *Conn) Has(feature string) bool { return c.features[feature] }

// Done is closed when the session ends.
func (c *Conn) Done() <-chan struct{} { return c.ctx.Done() }

// Active returns the number of open streams.
func (c *Conn) Active() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.streams)
}

func (c *Conn) isReady() bool {
	select {
	case <-c.ready:
		return true
	default:
		return false
	}
}

func (c *Conn) send(kind string, f Frame) error {
	b, e := json.Marshal(f)
	if e != nil {
		return e
	}
	return c.queue(&jsonrpc.Request{Method: Prefix + kind, Params: b}, kind == "data")
}
func (c *Conn) queue(msg jsonrpc.Message, data bool) error {
	return c.queueContext(c.ctx, msg, data)
}
func (c *Conn) queueContext(ctx context.Context, msg jsonrpc.Message, data bool) error {
	p := packet{msg: msg, done: make(chan error, 1)}
	q := c.high
	if data {
		q = c.data
	}
	select {
	case q <- p:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	}
	select {
	case e := <-p.done:
		return e
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	}
}
func (c *Conn) writer() {
	defer c.Close()
	for {
		var p packet
		select {
		case <-c.ctx.Done():
			return
		case p = <-c.high:
		default:
			select {
			case <-c.ctx.Done():
				return
			case p = <-c.high:
			case p = <-c.data:
			}
		}
		timer := time.AfterFunc(30*time.Second, func() { c.Close() })
		e := c.base.Write(c.ctx, p.msg)
		timer.Stop()
		p.done <- e
		if e != nil {
			return
		}
	}
}
func (c *Conn) reader() {
	defer c.Close()
	for {
		msg, e := c.base.Read(c.ctx)
		if e != nil {
			return
		}
		if req, ok := msg.(*jsonrpc.Request); ok && strings.HasPrefix(req.Method, Prefix) {
			if req.ID.IsValid() || len(req.Params) > 24*1024 {
				return
			}
			var f Frame
			if json.Unmarshal(req.Params, &f) != nil {
				return
			}
			if e = c.handle(strings.TrimPrefix(req.Method, Prefix), f); e != nil {
				log.Printf("client channel protocol error: %v", e)
				return
			}
		} else {
			if !c.isReady() {
				log.Printf("client channel protocol error: MCP message before ready")
				return
			}
			select {
			case c.messages <- msg:
			case <-c.ctx.Done():
				return
			}
		}
	}
}
func (c *Conn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case m := <-c.messages:
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, io.EOF
	}
}
func (c *Conn) Write(ctx context.Context, m jsonrpc.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return c.queueContext(ctx, m, false)
	}
}
func (c *Conn) SessionID() string { return c.base.SessionID() }
func (c *Conn) Close() error {
	c.once.Do(func() {
		c.cancel()
		c.base.Close()
		c.mu.Lock()
		ss := make([]*stream, 0, len(c.streams))
		for _, s := range c.streams {
			ss = append(ss, s)
		}
		c.mu.Unlock()
		for _, s := range ss {
			s.stop()
		}
	})
	return nil
}

// emit queues a control notification without waiting, for paths that must not block.
func (c *Conn) emit(kind string, f Frame) error {
	b, _ := json.Marshal(f)
	p := packet{msg: &jsonrpc.Request{Method: Prefix + kind, Params: b}, done: make(chan error, 1)}
	select {
	case c.high <- p:
		return nil
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	default:
		return errors.New("control queue full")
	}
}

// Report sends a limit or auth_failed notice to the bridge (target only).
func (c *Conn) Report(kind string, f Frame) {
	if !c.bridge {
		c.emit(kind, f)
	}
}

func (c *Conn) ownID(id uint64) bool { return (id%2 == 1) == c.bridge }

func (c *Conn) handle(kind string, f Frame) error {
	switch kind {
	case "ready":
		if c.isReady() {
			return errors.New("duplicate ready")
		}
		if !c.bridge {
			if e := c.send("ready", Frame{}); e != nil {
				return e
			}
		}
		c.readyOnce.Do(func() { close(c.ready) })
		return nil
	case "listen":
		if c.bridge || !c.features[ForwardRemote] || c.opt.Listen == nil {
			return errors.New("unexpected listen")
		}
		return c.handleListen(f)
	case "unlisten":
		if c.bridge || !c.features[ForwardRemote] {
			return errors.New("unexpected unlisten")
		}
		c.fwMu.Lock()
		active := c.forwards[f.Forward]
		delete(c.forwards, f.Forward)
		last := c.lastForward
		c.fwMu.Unlock()
		if active && c.opt.Unlisten != nil {
			c.opt.Unlisten(f.Forward)
		} else if f.Forward > last {
			return errors.New("unknown forward")
		}
		return nil
	case "listen_result":
		if !c.bridge {
			return errors.New("unexpected listen result")
		}
		c.fwMu.Lock()
		ch := c.listens[f.Forward]
		delete(c.listens, f.Forward)
		c.fwMu.Unlock()
		if ch == nil {
			return errors.New("unexpected listen result")
		}
		ch <- f
		return nil
	}
	if !c.isReady() {
		return fmt.Errorf("%s before ready", kind)
	}
	if kind == "limit" || kind == "auth_failed" {
		if !c.bridge {
			return errors.New("unexpected report")
		}
		for _, fn := range c.opt.report {
			fn(kind, f)
		}
		return nil
	}
	if kind == "open" {
		return c.handleOpen(f)
	}
	c.mu.Lock()
	s := c.streams[f.ID]
	c.mu.Unlock()
	if s == nil {
		if kind == "close" {
			return nil
		}
		if (c.ownID(f.ID) && f.ID > c.lastOwn.Load()) || (!c.ownID(f.ID) && f.ID > c.lastPeer) {
			return errors.New("unknown stream")
		}
		return nil
	}
	s.flowMu.Lock()
	closed := s.peerClosed
	s.flowMu.Unlock()
	if closed {
		return nil
	}
	switch kind {
	case "open_result":
		if s.established {
			return errors.New("unexpected result")
		}
		s.established = f.Error == ""
		var e error
		if f.Error != "" {
			e = &Error{Code: f.Error}
		}
		select {
		case s.opened <- e:
		default:
			return errors.New("duplicate result")
		}
	case "data":
		if !s.established {
			return errors.New("data before open result")
		}
		b, e := base64.StdEncoding.DecodeString(f.Data)
		if e != nil || len(b) == 0 || len(b) > frameSize {
			return errors.New("invalid data")
		}
		s.flowMu.Lock()
		if s.receivedEOF || len(b) > s.recvCredit {
			s.flowMu.Unlock()
			return errors.New("receive credit exceeded")
		}
		s.recvCredit -= len(b)
		first := min(len(b), windowSize-s.recvTail)
		copy(s.recvBuffer[s.recvTail:], b[:first])
		copy(s.recvBuffer, b[first:])
		s.recvTail = (s.recvTail + len(b)) % windowSize
		s.buffered += len(b)
		s.flowMu.Unlock()
		s.wake()

	case "window":
		s.flowMu.Lock()
		if f.Bytes <= 0 || f.Bytes > windowSize || s.sendCredit+f.Bytes > windowSize {
			s.flowMu.Unlock()
			return errors.New("invalid window")
		}
		s.sendCredit += f.Bytes
		s.flowMu.Unlock()
		select {
		case s.credit <- 1:
		default:
		}
	case "half_close":
		s.flowMu.Lock()
		if s.receivedEOF {
			s.flowMu.Unlock()
			return errors.New("duplicate eof")
		}
		s.receivedEOF = true
		s.flowMu.Unlock()
		s.wake()
	case "close":
		// Drain incoming bytes before EOF. Closing the write half here would make
		// HTTP transports abort their reader before buffered responses are consumed.
		s.flowMu.Lock()
		s.peerClosed = true
		s.networkCancel()
		s.receivedEOF = true
		s.flowMu.Unlock()
		s.wake()
		select {
		case s.opened <- io.ErrClosedPipe:
		default:
		}

	default:
		return errors.New("unknown channel notification")
	}
	return nil
}

func validHost(host string, port int) bool {
	return host != "" && len(host) <= 253 && !strings.ContainsAny(host, "/\\\r\n\t @") && port > 0 && port <= 65535
}

// acceptable reports whether the peer may open a stream of this kind.
func (c *Conn) acceptable(f Frame) bool {
	if c.bridge {
		switch f.Kind {
		case KindHTTP, KindConnect:
			return c.features[HTTPProxy]
		case KindForward:
			return c.features[ForwardRemote] && f.Forward != 0 && f.Host == "" && f.Port == 0
		}
		return false
	}
	return (f.Kind == KindForward || f.Kind == KindSOCKS) && c.features[ForwardLocal] && f.Forward != 0 && validHost(f.Host, f.Port)
}

func (c *Conn) handleOpen(f Frame) error {
	if f.ID == 0 || c.ownID(f.ID) || f.ID <= c.lastPeer {
		return errors.New("invalid open")
	}
	c.lastPeer = f.ID
	if !c.acceptable(f) {
		return fmt.Errorf("unexpected open of kind %q", f.Kind)
	}
	if c.bridge && f.Kind == KindForward {
		c.fwMu.Lock()
		listening, known := c.forwards[f.Forward]
		c.fwMu.Unlock()
		if !known {
			return errors.New("open for an unregistered forward")
		}
		if !listening {
			// The forward was closed while the target accepted a connection.
			return c.emit("open_result", Frame{ID: f.ID, Error: CodeClosed})
		}
	}
	s, e := c.newStream(f.ID, true)
	if e != nil {
		if c.bridge {
			for _, fn := range c.opt.report {
				fn("limit", f)
			}
		}
		return c.emit("open_result", Frame{ID: f.ID, Error: CodeLimit})
	}
	in := &Incoming{ID: f.ID, Kind: f.Kind, Host: f.Host, Port: f.Port, Forward: f.Forward, s: s}
	fn := c.opt.open[f.Kind]
	if fn == nil {
		in.Reject(CodeFailed)
		return nil
	}
	go fn(s.networkCtx, in)
	return nil
}

func (c *Conn) handleListen(f Frame) error {
	if f.Forward == 0 || (f.Bind != "127.0.0.1" && f.Bind != "::1") || f.Port < 0 || f.Port > 65535 {
		return errors.New("invalid listen")
	}
	c.fwMu.Lock()
	if f.Forward <= c.lastForward {
		c.fwMu.Unlock()
		return errors.New("invalid listen")
	}
	c.lastForward = f.Forward
	full := len(c.forwards) >= MaxForwards
	c.fwMu.Unlock()
	if full {
		return c.emit("listen_result", Frame{Forward: f.Forward, Error: CodeLimit})
	}
	port, e := c.opt.Listen(c, f.Forward, f.Bind, f.Port)
	if e != nil {
		return c.emit("listen_result", Frame{Forward: f.Forward, Error: Code(e)})
	}
	c.fwMu.Lock()
	c.forwards[f.Forward] = true
	c.fwMu.Unlock()
	return c.emit("listen_result", Frame{Forward: f.Forward, Port: port})
}

// Listen asks the target to listen for a remote forward and returns the bound port (bridge only).
// Forward IDs must increase across calls.
func (c *Conn) Listen(ctx context.Context, forward uint64, bind string, port int) (int, error) {
	if !c.bridge || !c.features[ForwardRemote] {
		return 0, errors.New("remote forwarding is not enabled")
	}
	ch := make(chan Frame, 1)
	c.fwMu.Lock()
	if forward <= c.lastForward {
		c.fwMu.Unlock()
		return 0, errors.New("forward IDs must increase")
	}
	c.lastForward = forward
	// Registered before sending: the target may accept a connection before its result arrives.
	c.forwards[forward] = true
	c.listens[forward] = ch
	c.fwMu.Unlock()
	fail := func(e error) (int, error) {
		c.fwMu.Lock()
		c.forwards[forward] = false
		delete(c.listens, forward)
		c.fwMu.Unlock()
		return 0, e
	}
	if e := c.send("listen", Frame{Forward: forward, Bind: bind, Port: port}); e != nil {
		return fail(e)
	}
	select {
	case f := <-ch:
		if f.Error != "" {
			return fail(&Error{Code: f.Error})
		}
		return f.Port, nil
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-c.ctx.Done():
		return fail(io.ErrClosedPipe)
	}
}

// Unlisten closes a remote forward on the target (bridge only). Later opens for it are refused.
func (c *Conn) Unlisten(forward uint64) {
	c.fwMu.Lock()
	listening := c.forwards[forward]
	c.forwards[forward] = false
	c.fwMu.Unlock()
	if listening && c.emit("unlisten", Frame{Forward: forward}) != nil {
		c.Close()
	}
}

// Incoming is a stream opened by the peer, waiting for Accept or Reject.
type Incoming struct {
	ID      uint64
	Kind    string
	Host    string
	Port    int
	Forward uint64
	s       *stream
}

// Accept confirms the stream and returns it as a connection.
func (in *Incoming) Accept() (net.Conn, error) {
	if e := in.s.mux.send("open_result", Frame{ID: in.ID}); e != nil {
		in.s.stop()
		return nil, e
	}
	return &streamConn{s: in.s}, nil
}

// Reject refuses the stream with a fixed code.
func (in *Incoming) Reject(code string) {
	in.s.mux.send("open_result", Frame{ID: in.ID, Error: code})
	in.s.stop()
}

// Dial opens a stream to the peer. Failures reported by the peer are *Error values.
func (c *Conn) Dial(ctx context.Context, kind, host string, port int, forward uint64) (net.Conn, error) {
	select {
	case <-c.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, io.ErrClosedPipe
	}
	c.openMu.Lock()
	if e := ctx.Err(); e != nil {
		c.openMu.Unlock()
		return nil, e
	}
	c.opened++
	id := 2 * c.opened
	if c.bridge {
		id--
	}
	c.lastOwn.Store(id)
	s, e := c.newStream(id, false)
	if e != nil {
		c.openMu.Unlock()
		return nil, ErrLimit
	}
	payload, _ := json.Marshal(Frame{ID: id, Host: host, Port: port, Kind: kind, Forward: forward})
	e = c.queueContext(ctx, &jsonrpc.Request{Method: Prefix + "open", Params: payload}, false)
	c.openMu.Unlock()
	if e != nil {
		s.stop()
		if c.emit("close", Frame{ID: id}) != nil {
			c.Close()
		}
		return nil, e
	}
	select {
	case e = <-s.opened:
		if e != nil {
			s.stop()
			return nil, e
		}
		return &streamConn{s: s}, nil
	case <-ctx.Done():
		s.stop()
		c.emit("close", Frame{ID: id})
		return nil, ctx.Err()
	case <-s.closed:
		return nil, io.ErrClosedPipe
	}
}

type stream struct {
	id                           uint64
	mux                          *Conn
	appR, appW, pipeR, pipeW     net.Conn
	flowMu                       sync.Mutex
	writeMu                      sync.Mutex
	sent                         atomic.Uint64
	written                      uint64
	ack                          chan struct{}
	recvCredit, sendCredit       int
	receivedEOF                  bool
	peerClosed                   bool
	established                  bool
	writeDeadline                time.Time
	deadlineWake                 chan struct{}
	recvWake                     chan struct{}
	recvBuffer                   []byte
	recvHead, recvTail, buffered int
	credit                       chan int
	opened                       chan error
	closed                       chan struct{}
	once                         sync.Once
	cancel                       context.CancelFunc
	networkCtx                   context.Context
	networkCancel                context.CancelFunc
}

// newStream registers a stream; peer-initiated streams start established, own streams wait for open_result.
func (c *Conn) newStream(id uint64, established bool) (*stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.streams) >= MaxStreams {
		return nil, errors.New("connection limit reached")
	}
	if _, ok := c.streams[id]; ok {
		return nil, errors.New("duplicate connection")
	}
	a, b := net.Pipe()
	d, e := net.Pipe()
	ctx, cancel := context.WithCancel(c.ctx)
	networkCtx, networkCancel := context.WithCancel(c.ctx)
	s := &stream{established: established, networkCtx: networkCtx, networkCancel: networkCancel, id: id, mux: c, appR: a, pipeW: b, appW: d, pipeR: e, recvCredit: windowSize, sendCredit: windowSize, recvWake: make(chan struct{}, 1), recvBuffer: make([]byte, windowSize), credit: make(chan int, 1), deadlineWake: make(chan struct{}, 1), ack: make(chan struct{}, 1), opened: make(chan error, 1), closed: make(chan struct{}), cancel: cancel}
	s.credit <- windowSize
	c.streams[id] = s
	go s.receive(ctx)
	go s.transmit(ctx)
	return s, nil
}
func (s *stream) stop() {
	s.once.Do(func() {
		close(s.closed)
		s.cancel()
		s.networkCancel()
		s.appR.Close()
		s.appW.Close()
		s.pipeR.Close()
		s.pipeW.Close()
		s.mux.mu.Lock()
		delete(s.mux.streams, s.id)
		s.mux.mu.Unlock()
	})
}
func (s *stream) wake() {
	select {
	case s.recvWake <- struct{}{}:
	default:
	}
}
func (s *stream) receive(ctx context.Context) {
	buf := make([]byte, frameSize)
	for {
		s.flowMu.Lock()
		n := min(len(buf), s.buffered)
		first := min(n, windowSize-s.recvHead)
		copy(buf[:first], s.recvBuffer[s.recvHead:s.recvHead+first])
		copy(buf[first:n], s.recvBuffer[:n-first])
		s.recvHead = (s.recvHead + n) % windowSize
		s.buffered -= n
		eof := s.receivedEOF
		s.flowMu.Unlock()
		if n > 0 {
			if _, e := s.pipeW.Write(buf[:n]); e != nil {
				s.stop()
				return
			}
			s.flowMu.Lock()
			s.recvCredit += n
			s.flowMu.Unlock()
			if s.mux.send("window", Frame{ID: s.id, Bytes: n}) != nil {
				s.stop()
				return
			}
		} else if eof {
			s.pipeW.Close()
			return
		} else {
			select {
			case <-s.recvWake:
			case <-ctx.Done():
				return
			}
		}
	}
}
func (s *stream) transmit(ctx context.Context) {
	buf := make([]byte, frameSize)
	for {
		s.flowMu.Lock()
		credit := s.sendCredit
		s.flowMu.Unlock()
		if credit == 0 {
			select {
			case <-s.credit:
				continue
			case <-ctx.Done():
				return
			}
		}
		n, e := s.pipeR.Read(buf[:min(frameSize, credit)])
		if n > 0 {
			s.flowMu.Lock()
			s.sendCredit -= n
			s.flowMu.Unlock()
			if s.mux.send("data", Frame{ID: s.id, Data: base64.StdEncoding.EncodeToString(buf[:n])}) != nil {
				return
			}
			s.sent.Add(uint64(n))
			select {
			case s.ack <- struct{}{}:
			default:
			}

		}
		if e != nil {
			if e == io.EOF {
				s.mux.send("half_close", Frame{ID: s.id})
			}
			return
		}
	}
}

type streamConn struct{ s *stream }

func (s *streamConn) Read(b []byte) (int, error) { return s.s.appR.Read(b) }
func (s *streamConn) Write(b []byte) (int, error) {
	s.s.writeMu.Lock()
	defer s.s.writeMu.Unlock()
	n, e := s.s.appW.Write(b)
	s.s.written += uint64(n)
	for s.s.sent.Load() < s.s.written {
		s.s.flowMu.Lock()
		deadline := s.s.writeDeadline
		s.s.flowMu.Unlock()
		var timer *time.Timer
		var expired <-chan time.Time
		if !deadline.IsZero() {
			timer = time.NewTimer(time.Until(deadline))
			expired = timer.C
		}
		select {
		case <-expired:
			return n, os.ErrDeadlineExceeded
		case <-s.s.deadlineWake:

		case <-s.s.ack:
		case <-s.s.closed:
			if timer != nil {
				timer.Stop()
			}
			return n, io.ErrClosedPipe
		case <-s.s.mux.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return n, io.ErrClosedPipe
		}
		if timer != nil {
			timer.Stop()
		}
	}
	return n, e
}
func (s *streamConn) CloseWrite() error    { return s.s.appW.Close() }
func (s *streamConn) CloseRead() error     { return s.s.appR.Close() }
func (s *streamConn) LocalAddr() net.Addr  { return s.s.appR.LocalAddr() }
func (s *streamConn) RemoteAddr() net.Addr { return s.s.appW.RemoteAddr() }
func (s *streamConn) SetDeadline(t time.Time) error {
	s.SetReadDeadline(t)
	return s.SetWriteDeadline(t)
}
func (s *streamConn) SetReadDeadline(t time.Time) error { return s.s.appR.SetReadDeadline(t) }
func (s *streamConn) SetWriteDeadline(t time.Time) error {
	s.s.flowMu.Lock()
	s.s.writeDeadline = t
	s.s.flowMu.Unlock()
	select {
	case s.s.deadlineWake <- struct{}{}:
	default:
	}
	return s.s.appW.SetWriteDeadline(t)
}
func (s *streamConn) Close() error {
	s.s.stop()
	err := s.s.mux.emit("close", Frame{ID: s.s.id})
	if err != nil {
		s.s.mux.Close()
	}
	return err
}
