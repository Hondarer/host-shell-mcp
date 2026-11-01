package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

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

// handleFrame applies a notification to an existing stream. Only the channel reader calls it.
func (s *stream) handleFrame(kind string, f Frame) error {
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
