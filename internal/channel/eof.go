package channel

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EOFOptions configures TrackRequests.
type EOFOptions struct {
	// Finish delays the end of input until every request read so far has been answered, so a
	// client that writes its requests and closes standard input still receives the responses.
	Finish bool
}

// Tracker wraps the target's stdio transport and counts requests that have no response yet.
//
// When reading reaches the end of input, the SDK cancels the requests still in flight and ends
// the session without writing their responses. host-shell treats the end of standard input as
// an SSH disconnect and relies on that cancellation to stop running commands and jobs, so the
// default keeps it; EOFOptions.Finish holds the end of input back until the responses are
// written instead. Either way, Unanswered reports what was left behind.
type Tracker struct {
	base mcp.Transport
	opt  EOFOptions

	mu      sync.Mutex
	pending map[jsonrpc.ID]bool
	changed chan struct{} // closed and replaced whenever pending shrinks
}

// TrackRequests wraps base; see Tracker.
func TrackRequests(base mcp.Transport, opt EOFOptions) *Tracker {
	return &Tracker{base: base, opt: opt, pending: map[jsonrpc.ID]bool{}, changed: make(chan struct{})}
}

// Connect implements mcp.Transport.
func (t *Tracker) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &trackedConn{Connection: conn, t: t}, nil
}

// Unanswered returns the number of requests read without a response written.
func (t *Tracker) Unanswered() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

func (t *Tracker) add(id jsonrpc.ID) {
	t.mu.Lock()
	t.pending[id] = true
	t.mu.Unlock()
}

func (t *Tracker) done(id jsonrpc.ID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending[id] {
		delete(t.pending, id)
		close(t.changed)
		t.changed = make(chan struct{})
	}
}

// drain waits until no request is pending or ctx ends.
func (t *Tracker) drain(ctx context.Context) {
	for {
		t.mu.Lock()
		n, changed := len(t.pending), t.changed
		t.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

type trackedConn struct {
	mcp.Connection
	t *Tracker
}

func (c *trackedConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err != nil {
		if c.t.opt.Finish {
			c.t.drain(ctx)
		}
		return msg, err
	}
	if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
		c.t.add(req.ID)
	}
	return msg, nil
}

func (c *trackedConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	if resp, ok := msg.(*jsonrpc.Response); ok && err == nil {
		c.t.done(resp.ID)
	}
	return err
}
