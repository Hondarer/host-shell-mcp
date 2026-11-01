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
	// RemotePorts limits the ports remote forwards listen on; nil permits any port.
	RemotePorts policy.Ports
}

// Target is the target side: it connects local forwards and SOCKS, and listens remote forwards.
type Target struct {
	local, remote bool
	ports         policy.Ports
	policy        *policy.Policy
	audit         *audit.Logger
	dial          func(ctx context.Context, network, addr string) (net.Conn, error)
	timeout       time.Duration
	mu            sync.Mutex
	listeners     map[uint64]*targetListener
	// unlistenMu serializes unlisten so that Close can wait for one a closed connection started.
	unlistenMu sync.Mutex
}

type targetListener struct {
	addr string
	l    net.Listener
}

// TargetOptions configures a Target.
type TargetOptions struct {
	Local       bool           // permit local forwards and SOCKS (--allow-forward-local)
	Remote      bool           // permit remote forwards (--allow-forward-remote)
	RemotePorts policy.Ports   // ports remote forwards may listen on (--allow-forward-remote-port); nil permits any
	Policy      *policy.Policy // applies --allow-host to destinations
	Audit       *audit.Logger
}

// NewTarget creates the target side of port forwarding.
func NewTarget(o TargetOptions) *Target {
	return &Target{local: o.Local, remote: o.Remote, ports: o.RemotePorts, policy: o.Policy, audit: o.Audit, dial: (&net.Dialer{}).DialContext, timeout: establishTimeout, listeners: map[uint64]*targetListener{}}
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
			result(channel.Denied, channel.ReasonNotAllowed)
			in.Reject(channel.CodeNotAllowed)
			return
		}
	}
	in.Answer(ctx, channel.Answer{
		Timeout: t.timeout,
		Dial: func(ctx context.Context) (net.Conn, error) {
			return t.dial(ctx, "tcp", net.JoinHostPort(in.Host, strconv.Itoa(in.Port)))
		},
		Failed: func(err error) string {
			code := dialCode(err)
			result(channel.Failed, dialFailure(code))
			return code
		},
		Relay: func(stream, external net.Conn) {
			result(channel.Allowed, channel.ReasonEstablished)
			pipe(stream, external)
		},
	})
}

func (t *Target) listen(c *channel.Conn, id uint64, bind string, port int) (int, error) {
	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	if port != 0 && !t.ports.Contains(port) {
		t.event("forward_listen", "forward_id", id, "address", addr, "result", channel.Denied, "reason", "listen port not allowed")
		return 0, &channel.Error{Code: channel.CodeNotAllowed}
	}
	l, err := t.bind(bind, port)
	if err != nil {
		code := listenCode(err)
		t.event("forward_listen", "forward_id", id, "address", addr, "result", channel.Failed, "reason", dialFailure(code))
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

// bind listens on port. With permitted ranges, port 0 takes the first free port in them, in the
// order given, so that the bridge's default of port 0 still works without knowing the ranges.
// Only busy ports are skipped; any other failure ends the search.
func (t *Target) bind(host string, port int) (net.Listener, error) {
	if port != 0 || t.ports == nil {
		return net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	}
	var err error
	for _, r := range t.ports {
		for p := r.Lo; p <= r.Hi; p++ {
			var l net.Listener
			if l, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p))); err == nil || listenCode(err) != channel.CodeInUse {
				return l, err
			}
		}
	}
	return nil, err
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
	t.unlistenMu.Lock()
	defer t.unlistenMu.Unlock()
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
	info := TargetInfo{Local: t.local, Remote: t.remote, RemoteForwards: []RemoteForward{}, RemotePorts: t.ports}
	for id, tl := range t.listeners {
		info.RemoteForwards = append(info.RemoteForwards, RemoteForward{ForwardID: id, Address: tl.addr})
	}
	slices.SortFunc(info.RemoteForwards, func(a, b RemoteForward) int { return int(a.ForwardID) - int(b.ForwardID) })
	return info
}

// Close stops all remote forward listeners. When it returns, every listener's forward_unlisten
// record has been written, including one that a closed connection's goroutine was writing.
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
	// A goroutine that removed a listener before the snapshot still holds unlistenMu until it has
	// recorded forward_unlisten.
	t.unlistenMu.Lock()
	t.unlistenMu.Unlock()
}
