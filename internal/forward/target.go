package forward

import (
	"context"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// RemoteForward is a remote forward listening on the target.
type RemoteForward struct {
	ForwardID uint64 `json:"forward_id"`
	Address   string `json:"address"`
}

// TargetInfo is the forwarding state reported by sys_get_info.
type TargetInfo struct {
	Local          bool
	Remote         bool
	RemoteForwards []RemoteForward
}

// Target is the target side: it connects local forwards and SOCKS, and listens remote forwards.
type Target struct {
	local, remote bool
	policy        *policy.Policy
	audit         *audit.Logger
	dial          func(ctx context.Context, network, addr string) (net.Conn, error)
	timeout       time.Duration
	mu            sync.Mutex
	listeners     map[uint64]*targetListener
}

type targetListener struct {
	addr string
	l    net.Listener
}

// NewTarget permits the given directions. pol applies --allow-host to destinations.
func NewTarget(local, remote bool, pol *policy.Policy, al *audit.Logger) *Target {
	return &Target{local: local, remote: remote, policy: pol, audit: al, dial: (&net.Dialer{}).DialContext, timeout: establishTimeout, listeners: map[uint64]*targetListener{}}
}

// Register permits the directions on the target's channel options.
func (t *Target) Register(o *channel.Options) {
	if t.local {
		o.Enable(channel.ForwardLocal)
		o.Handle(channel.KindForward, t.open)
		o.Handle(channel.KindSOCKS, t.open)
	}
	if t.remote {
		o.Enable(channel.ForwardRemote)
		o.Listen = t.listen
		o.Unlisten = t.unlisten
	}
}

func (t *Target) event(msg string, attrs ...any) {
	if t.audit != nil {
		t.audit.Event(msg, attrs...)
	}
}

// open connects a local forward or SOCKS stream from the bridge to its destination.
func (t *Target) open(ctx context.Context, in *channel.Incoming) {
	direction := Local
	if in.Kind == channel.KindSOCKS {
		direction = SOCKS
	}
	result := func(status, reason string) {
		t.event("forward_connect", "direction", direction, "forward_id", in.Forward, "host", in.Host, "port", in.Port, "result", status, "reason", reason)
	}
	if t.policy != nil {
		if err := t.policy.CheckHost(in.Host, in.Port); err != nil {
			result("denied", "destination not allowed")
			in.Reject(channel.CodeNotAllowed)
			return
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	external, err := t.dial(dialCtx, "tcp", net.JoinHostPort(in.Host, strconv.Itoa(in.Port)))
	if err != nil {
		code := dialCode(err)
		result("failed", reason(code))
		in.Reject(code)
		return
	}
	defer external.Close()
	stream, err := in.Accept()
	if err != nil {
		return
	}
	defer stream.Close()
	result("allowed", "connection established")
	pipe(stream, external)
}

func (t *Target) listen(c *channel.Conn, id uint64, bind string, port int) (int, error) {
	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		code := listenCode(err)
		t.event("forward_listen", "forward_id", id, "address", addr, "result", "failed", "reason", reason(code))
		return 0, &channel.Error{Code: code}
	}
	addr = l.Addr().String()
	t.mu.Lock()
	t.listeners[id] = &targetListener{addr: addr, l: l}
	t.mu.Unlock()
	t.event("forward_listen", "forward_id", id, "address", addr, "result", "ok", "reason", "listening")
	go func() {
		<-c.Done()
		t.unlisten(id)
	}()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go t.serve(c, id, conn)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// serve hands a connection accepted on a remote forward to the bridge, which picks the destination.
func (t *Target) serve(c *channel.Conn, id uint64, conn net.Conn) {
	defer conn.Close()
	// The bridge answers within its own 30 second connect timeout; this only bounds a stalled peer.
	ctx, cancel := context.WithTimeout(context.Background(), 2*t.timeout)
	stream, err := c.Dial(ctx, channel.KindForward, "", 0, id)
	cancel()
	if err != nil {
		if err == channel.ErrLimit {
			c.Report("limit", channel.Frame{Forward: id})
		}
		return
	}
	defer stream.Close()
	pipe(conn, stream)
}

func (t *Target) unlisten(id uint64) {
	t.mu.Lock()
	tl := t.listeners[id]
	delete(t.listeners, id)
	t.mu.Unlock()
	if tl != nil {
		tl.l.Close()
		t.event("forward_unlisten", "forward_id", id, "address", tl.addr)
	}
}

// Info returns the permitted directions and listening remote forwards.
func (t *Target) Info() TargetInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	info := TargetInfo{Local: t.local, Remote: t.remote, RemoteForwards: []RemoteForward{}}
	for id, tl := range t.listeners {
		info.RemoteForwards = append(info.RemoteForwards, RemoteForward{ForwardID: id, Address: tl.addr})
	}
	slices.SortFunc(info.RemoteForwards, func(a, b RemoteForward) int { return int(a.ForwardID) - int(b.ForwardID) })
	return info
}

// Close stops all remote forward listeners.
func (t *Target) Close() {
	t.mu.Lock()
	ids := make([]uint64, 0, len(t.listeners))
	for id := range t.listeners {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	for _, id := range ids {
		t.unlisten(id)
	}
}
