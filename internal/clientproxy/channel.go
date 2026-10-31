// Package clientproxy carries authenticated HTTP proxy traffic beside MCP.
package clientproxy

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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const prefix = "host-shell/proxy/"
const frameSize = 16 * 1024
const windowSize = 64 * 1024
const maxStreams = 32

type frame struct {
	ID      uint64 `json:"id,omitempty"`
	Version int    `json:"version,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Data    string `json:"data,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	Error   string `json:"error,omitempty"`
}
type packet struct {
	msg  jsonrpc.Message
	done chan error
}
type stream struct {
	id                           uint64
	mux                          *channel
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
type channel struct {
	base       mcp.Connection
	ctx        context.Context
	cancel     context.CancelFunc
	high, data chan packet
	messages   chan jsonrpc.Message
	mu         sync.Mutex
	openMu     sync.Mutex
	streams    map[uint64]*stream
	next       atomic.Uint64
	lastID     uint64
	remote     bool
	gateway    *Gateway
	once       sync.Once
}
type transport struct {
	base    mcp.Transport
	gateway *Gateway
	target  *Target
}

// Bridge wraps the remote connection with the client gateway.
func Bridge(base mcp.Transport, g *Gateway) mcp.Transport { return &transport{base: base, gateway: g} }

// Remote wraps the target's stdio connection.
func Remote(base mcp.Transport, t *Target) mcp.Transport { return &transport{base: base, target: t} }
func (t *transport) Connect(ctx context.Context) (mcp.Connection, error) {
	base, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &channel{base: base, ctx: ctx, cancel: cancel, high: make(chan packet, 32), data: make(chan packet, 128), messages: make(chan jsonrpc.Message, 16), streams: map[uint64]*stream{}, remote: t.target != nil, gateway: t.gateway}
	// Both sides send hello, then ready, before SDK initialization.
	timer := time.AfterFunc(30*time.Second, func() { base.Close() })
	fail := func(err error) (mcp.Connection, error) {
		timer.Stop()
		cancel()
		base.Close()
		return nil, fmt.Errorf("client proxy negotiation: %w", err)
	}
	for _, kind := range []string{"hello", "ready"} {
		if !c.remote {
			if err = writeFrame(ctx, base, kind, frame{Version: 1}); err != nil {
				return fail(err)
			}
		}
		msg, e := base.Read(ctx)
		if e != nil {
			return fail(e)
		}
		req, ok := msg.(*jsonrpc.Request)
		var f frame
		if !ok || req.Method != prefix+kind || req.ID.IsValid() || json.Unmarshal(req.Params, &f) != nil || f.Version != 1 {
			return fail(errors.New("both peers must explicitly enable proxy version 1"))
		}
		if c.remote {
			if err = writeFrame(ctx, base, kind, frame{Version: 1}); err != nil {
				return fail(err)
			}
		}
	}
	timer.Stop()
	go c.writer()
	go c.reader()
	go func() { <-ctx.Done(); c.Close() }()
	if t.target != nil {
		t.target.attach(c)
	} else {
		t.gateway.attach(c)
	}
	return c, nil
}
func writeFrame(ctx context.Context, base mcp.Connection, kind string, f frame) error {
	b, e := json.Marshal(f)
	if e != nil {
		return e
	}
	return base.Write(ctx, &jsonrpc.Request{Method: prefix + kind, Params: b})
}
func (c *channel) send(kind string, f frame) error {
	b, e := json.Marshal(f)
	if e != nil {
		return e
	}
	return c.queue(&jsonrpc.Request{Method: prefix + kind, Params: b}, kind == "data")
}
func (c *channel) queue(msg jsonrpc.Message, data bool) error {
	return c.queueContext(c.ctx, msg, data)
}
func (c *channel) queueContext(ctx context.Context, msg jsonrpc.Message, data bool) error {
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
func (c *channel) writer() {
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
func (c *channel) reader() {
	defer c.Close()
	for {
		msg, e := c.base.Read(c.ctx)
		if e != nil {
			return
		}
		if req, ok := msg.(*jsonrpc.Request); ok && strings.HasPrefix(req.Method, prefix) {
			if req.ID.IsValid() || len(req.Params) > 24*1024 {
				return
			}
			var f frame
			if json.Unmarshal(req.Params, &f) != nil {
				return
			}
			if e = c.handle(strings.TrimPrefix(req.Method, prefix), f); e != nil {
				log.Printf("proxy channel protocol error: %v", e)
				return
			}
		} else {
			select {
			case c.messages <- msg:
			case <-c.ctx.Done():
				return
			}
		}
	}
}
func (c *channel) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case m := <-c.messages:
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, io.EOF
	}
}
func (c *channel) Write(ctx context.Context, m jsonrpc.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return c.queueContext(ctx, m, false)
	}
}
func (c *channel) SessionID() string { return c.base.SessionID() }
func (c *channel) Close() error {
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
func (c *channel) newStream(id uint64) (*stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.streams) >= maxStreams {
		return nil, errors.New("connection limit reached")
	}
	if _, ok := c.streams[id]; ok {
		return nil, errors.New("duplicate connection")
	}
	a, b := net.Pipe()
	d, e := net.Pipe()
	ctx, cancel := context.WithCancel(c.ctx)
	networkCtx, networkCancel := context.WithCancel(c.ctx)
	s := &stream{established: !c.remote, networkCtx: networkCtx, networkCancel: networkCancel, id: id, mux: c, appR: a, pipeW: b, appW: d, pipeR: e, recvCredit: windowSize, sendCredit: windowSize, recvWake: make(chan struct{}, 1), recvBuffer: make([]byte, windowSize), credit: make(chan int, 1), deadlineWake: make(chan struct{}, 1), ack: make(chan struct{}, 1), opened: make(chan error, 1), closed: make(chan struct{}), cancel: cancel}
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
			if s.mux.send("window", frame{ID: s.id, Bytes: n}) != nil {
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
			if s.mux.send("data", frame{ID: s.id, Data: base64.StdEncoding.EncodeToString(buf[:n])}) != nil {
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
				s.mux.send("half_close", frame{ID: s.id})
			}
			return
		}
	}
}
func (c *channel) emit(kind string, f frame) error {
	b, _ := json.Marshal(f)
	p := packet{msg: &jsonrpc.Request{Method: prefix + kind, Params: b}, done: make(chan error, 1)}
	select {
	case c.high <- p:
		return nil
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	default:
		return errors.New("control queue full")
	}
}
func (c *channel) handle(kind string, f frame) error {
	if kind == "limit" {
		if c.remote {
			return errors.New("unexpected limit report")
		}
		c.gateway.record(0, f.Host, f.Port, "denied", "connection limit reached")
		return nil
	}
	if kind == "auth_failed" {
		if c.remote {
			return errors.New("unexpected auth report")
		}
		c.gateway.record(0, "", 0, "failed", "authentication")
		return nil
	}
	if kind == "open" {
		if c.remote || f.ID == 0 || f.ID <= c.lastID {
			return errors.New("invalid open")
		}
		c.lastID = f.ID
		s, e := c.newStream(f.ID)
		if e != nil {
			c.gateway.record(f.ID, f.Host, f.Port, "denied", "connection limit reached")
			return c.emit("open_result", frame{ID: f.ID, Error: "connection limit reached"})
		}
		go c.gateway.open(s.networkCtx, s, f)
		return nil
	}
	c.mu.Lock()
	s := c.streams[f.ID]
	c.mu.Unlock()
	if s == nil {
		if kind == "close" {
			return nil
		}
		if (c.remote && f.ID > c.next.Load()) || (!c.remote && f.ID > c.lastID) {
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
		if !c.remote || s.established {
			return errors.New("unexpected result")
		}
		s.established = f.Error == ""
		var e error
		if f.Error != "" {
			e = errors.New(f.Error)
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
		return errors.New("unknown proxy notification")
	}
	return nil
}
func (c *channel) dial(ctx context.Context, host string, port int, kind string) (net.Conn, error) {
	c.openMu.Lock()
	if e := ctx.Err(); e != nil {
		c.openMu.Unlock()
		return nil, e
	}
	id := c.next.Add(1)
	s, e := c.newStream(id)
	if e != nil {
		c.openMu.Unlock()
		return nil, e
	}
	payload, _ := json.Marshal(frame{ID: id, Host: host, Port: port, Kind: kind})
	e = c.queueContext(ctx, &jsonrpc.Request{Method: prefix + "open", Params: payload}, false)
	c.openMu.Unlock()
	if e != nil {
		s.stop()
		if c.emit("close", frame{ID: id}) != nil {
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
		c.emit("close", frame{ID: id})
		return nil, ctx.Err()
	case <-s.closed:
		return nil, io.ErrClosedPipe
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
	err := s.s.mux.emit("close", frame{ID: s.s.id})
	if err != nil {
		s.s.mux.Close()
	}
	return err
}
