package channel

import (
	"context"
	"errors"
	"io"
)

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
