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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

func newConn(ctx context.Context, base mcp.Connection, opt Options, bridge bool) *Conn {
	ctx, cancel := context.WithCancel(ctx)
	return &Conn{base: base, ctx: ctx, cancel: cancel, opt: opt, bridge: bridge, high: make(chan packet, 32), data: make(chan packet, 128), messages: make(chan jsonrpc.Message, 16), ready: make(chan struct{}), streams: map[uint64]*stream{}, forwards: map[uint64]bool{}, listens: map[uint64]chan Frame{}}
}

func (c *Conn) start() *Conn {
	go func() { <-c.ctx.Done(); c.Close() }()
	for _, fn := range c.opt.attach {
		fn(c)
	}
	return c
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
	return s.handleFrame(kind, f)
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
