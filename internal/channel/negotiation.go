package channel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
