package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
