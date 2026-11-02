package channel

import (
	"context"
	"io"
	"net"
	"time"
)

// Answer describes how to answer an incoming stream with an outbound connection.
type Answer struct {
	Timeout time.Duration                               // bounds Dial
	Dial    func(ctx context.Context) (net.Conn, error) // opens the outbound connection
	Failed  func(err error) string                      // records a dial failure and returns the reject code
	// Hold, when set, claims the outbound connection before the stream is accepted. A non-empty
	// code rejects the stream; release runs once the relay has ended.
	Hold  func(external net.Conn) (release func(), code string)
	Relay func(stream, external net.Conn) // runs once the stream is accepted; both are closed afterwards
}

// Answer dials for in and, once connected, accepts the stream and relays it.
func (in *Incoming) Answer(ctx context.Context, a Answer) {
	dialCtx, cancel := context.WithTimeout(ctx, a.Timeout)
	external, err := a.Dial(dialCtx)
	cancel()
	if err != nil {
		in.Reject(a.Failed(err))
		return
	}
	defer external.Close()
	if a.Hold != nil {
		release, code := a.Hold(external)
		if code != "" {
			in.Reject(code)
			return
		}
		defer release()
	}
	stream, err := in.Accept()
	if err != nil {
		return
	}
	defer stream.Close()
	a.Relay(stream, external)
}

// RelayOptions tunes Relay.
type RelayOptions struct {
	// Idle closes both connections after this long without traffic in either direction; 0 disables it.
	Idle time.Duration
	// Linger closes both connections when one direction has ended and the other has not ended
	// within this time; 0 waits for both.
	Linger time.Duration
}

// Relay copies both directions between a and b, passing half-closes on, until both directions
// end, then closes both. A copy error closes both connections, ending the other direction too.
func Relay(a, b net.Conn, o RelayOptions) {
	wrap := func(c net.Conn) (io.Reader, io.Writer) { return c, c }
	if o.Idle > 0 {
		touch := func() {
			deadline := time.Now().Add(o.Idle)
			a.SetDeadline(deadline)
			b.SetDeadline(deadline)
		}
		touch()
		wrap = func(c net.Conn) (io.Reader, io.Writer) {
			return &activityReader{Reader: c, touch: touch}, &activityWriter{Writer: c, touch: touch}
		}
	}
	done := make(chan struct{}, 2)
	half := func(dst, src net.Conn) {
		_, w := wrap(dst)
		r, _ := wrap(src)
		if _, err := io.Copy(w, r); err != nil {
			a.Close()
			b.Close()
		} else if !CloseWrite(dst) {
			dst.Close()
		}
		done <- struct{}{}
	}
	go half(a, b)
	go half(b, a)
	<-done
	if o.Linger > 0 {
		select {
		case <-done:
		case <-time.After(o.Linger):
			a.Close()
			b.Close()
			<-done
		}
	} else {
		<-done
	}
	a.Close()
	b.Close()
}

// Successful traffic in either direction refreshes the idle deadline of both connections.
type activityReader struct {
	io.Reader
	touch func()
}

func (r *activityReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if n > 0 {
		r.touch()
	}
	return n, err
}

type activityWriter struct {
	io.Writer
	touch func()
}

func (w *activityWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	if n > 0 {
		w.touch()
	}
	return n, err
}

// CloseWrite half-closes c when it supports that (TCP connections and channel streams) and
// reports whether it did.
func CloseWrite(c net.Conn) bool {
	cw, ok := c.(interface{ CloseWrite() error })
	if ok {
		cw.CloseWrite()
	}
	return ok
}
