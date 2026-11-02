package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pair negotiates a bridge and a target channel over in-memory transports.
func pair(t *testing.T, bridgeOpt, targetOpt Options) (*Conn, *Conn) {
	t.Helper()
	a, b := mcp.NewInMemoryTransports()
	// The session also covers the 40-stream payload test. Allow for race
	// instrumentation and CPU contention when packages run together in CI.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	var target mcp.Connection
	var te error
	done := make(chan struct{})
	go func() { target, te = Target(a, targetOpt).Connect(ctx); close(done) }()
	bridge, be := Client(b, bridgeOpt).Connect(ctx)
	<-done
	if be != nil || te != nil {
		t.Fatalf("negotiation: bridge %v, target %v", be, te)
	}
	t.Cleanup(func() { bridge.Close(); target.Close() })
	return bridge.(*Conn), target.(*Conn)
}

// negotiate runs both sides and returns their errors.
func negotiate(t *testing.T, bridgeOpt, targetOpt Options) (error, error) {
	t.Helper()
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	te := make(chan error, 1)
	go func() {
		c, err := Target(a, targetOpt).Connect(ctx)
		if c != nil {
			defer c.Close()
		}
		te <- err
	}()
	c, be := Client(b, bridgeOpt).Connect(ctx)
	if c != nil {
		c.Close()
	}
	return be, <-te
}

func echo(ctx context.Context, in *Incoming) {
	c, err := in.Accept()
	if err != nil {
		return
	}
	defer c.Close()
	io.Copy(c, c)
}

func TestNegotiationRules(t *testing.T) {
	for _, tc := range []struct {
		name               string
		requested, allowed []string
		bridgeErr          string
		targetErr          string
	}{
		{"DNS on bridge only", []string{RemoteDNS}, []string{ForwardLocal}, "--remote-dns", "--remote-dns"},
		{"DNS on target only", []string{ForwardLocal}, []string{RemoteDNS, ForwardLocal}, "--remote-dns", "--remote-dns"},
		{"DNS on both ends", []string{RemoteDNS}, []string{RemoteDNS}, "", ""},
		{"remote not permitted", []string{ForwardRemote}, nil, "--allow-forward-remote", "--allow-forward-remote"},
		{"local not permitted", []string{ForwardLocal}, []string{ForwardRemote}, "--allow-forward-local", "--allow-forward-local"},
		{"proxy on bridge only", []string{HTTPProxy}, []string{ForwardLocal}, "--client-proxy", "--client-proxy"},
		{"proxy on target only", []string{ForwardLocal}, []string{HTTPProxy, ForwardLocal}, "start the bridge with --proxy", "--proxy"},
		{"permit without request", []string{HTTPProxy}, []string{HTTPProxy, ForwardLocal, ForwardRemote}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			be, te := negotiate(t, Options{Features: tc.requested}, Options{Features: tc.allowed})
			for side, pair := range map[string][2]any{"bridge": {be, tc.bridgeErr}, "target": {te, tc.targetErr}} {
				err, want := pair[0], pair[1].(string)
				if want == "" {
					if err != nil {
						t.Fatalf("%s: %v", side, err)
					}
				} else if err == nil || !strings.Contains(err.(error).Error(), want) {
					t.Fatalf("%s: %v, want %q", side, err, want)
				}
			}
		})
	}
	bridge, target := pair(t, Options{Features: []string{HTTPProxy}}, Options{Features: []string{HTTPProxy, ForwardLocal}})
	if bridge.Has(ForwardLocal) || target.Has(ForwardLocal) || !target.Has(HTTPProxy) {
		t.Fatal("only requested features are active")
	}
}

func TestTargetPassesPlainMCP(t *testing.T) {
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	peer, err := b.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	go peer.Write(ctx, &jsonrpc.Request{Method: "initialize"})
	c, err := Target(a, Options{Features: []string{ForwardLocal, ForwardRemote}}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	msg, err := c.Read(ctx)
	if err != nil || msg.(*jsonrpc.Request).Method != "initialize" {
		t.Fatalf("first message %v %v", msg, err)
	}
	// With the HTTP proxy, plain MCP is a missing bridge --proxy.
	a, b = mcp.NewInMemoryTransports()
	peer2, _ := b.Connect(ctx)
	defer peer2.Close()
	go peer2.Write(ctx, &jsonrpc.Request{Method: "initialize"})
	if _, err = Target(a, Options{Features: []string{HTTPProxy}}).Connect(ctx); err == nil || !strings.Contains(err.Error(), "--proxy") {
		t.Fatalf("proxy without hello: %v", err)
	}
}

func TestOlderPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct {
		method string
		f      Frame
		want   string
	}{
		{"host-shell/proxy/hello", Frame{Version: 1}, "older channel definition"},
		{prefix + "hello", Frame{Version: 2}, "unsupported channel version 2"},
		{prefix + "hello", Frame{Version: 1, Features: []string{"udp"}}, "unknown channel feature"},
	} {
		a, b := mcp.NewInMemoryTransports()
		peer, _ := b.Connect(ctx)
		params, _ := json.Marshal(tc.f)
		go peer.Write(ctx, &jsonrpc.Request{Method: tc.method, Params: params})
		_, err := Target(a, Options{}).Connect(ctx)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.method, err)
		}
		peer.Close()
	}
	// An older target never answers the hello; the bridge explains both likely causes.
	a, b := mcp.NewInMemoryTransports()
	peer, _ := a.Connect(ctx)
	go func() { peer.Read(ctx); peer.Close() }()
	if _, err := Client(b, Options{Features: []string{ForwardLocal}}).Connect(ctx); err == nil || !strings.Contains(err.Error(), "same version on both ends") {
		t.Fatalf("closed target: %v", err)
	}
}

func TestBidirectionalStreams(t *testing.T) {
	var bridgeOpt, targetOpt Options
	bridgeOpt.Features = []string{ForwardLocal, ForwardRemote}
	bridgeOpt.Handle(KindForward, echo)
	targetOpt.Features = []string{ForwardLocal, ForwardRemote}
	targetOpt.Handle(KindSOCKS, echo)
	targetOpt.Listen = func(c *Conn, forward uint64, bind string, port int) (int, error) { return 4000 + int(forward), nil }
	targetOpt.Unlisten = func(uint64) {}
	bridgeOpt.Setup = func(ctx context.Context, c *Conn) error {
		port, err := c.Listen(ctx, 1, "127.0.0.1", 0)
		if port != 4001 {
			return fmt.Errorf("port %d %v", port, err)
		}
		return err
	}
	bridge, target := pair(t, bridgeOpt, targetOpt)
	payload := bytes.Repeat([]byte{0, 1, 2, 255}, 100000)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			c, err := bridge.Dial(context.Background(), KindSOCKS, "example.com", 443, 7)
			if err == nil {
				err = roundTrip(c, payload)
			}
			if err != nil {
				t.Error("bridge open:", err)
			}
		})
		wg.Go(func() {
			c, err := target.Dial(context.Background(), KindForward, "", 0, 1)
			if err == nil {
				err = roundTrip(c, payload)
			}
			if err != nil {
				t.Error("target open:", err)
			}
		})
	}
	wg.Wait()
	if bridge.lastOwn.Load()%2 != 1 || target.lastOwn.Load()%2 != 0 {
		t.Fatalf("ID parity: bridge %d, target %d", bridge.lastOwn.Load(), target.lastOwn.Load())
	}
}

func roundTrip(c net.Conn, payload []byte) error {
	defer c.Close()
	errc := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		c.(interface{ CloseWrite() error }).CloseWrite()
		errc <- err
	}()
	got, err := io.ReadAll(c)
	if err == nil {
		err = <-errc
	}
	if err == nil && !bytes.Equal(got, payload) {
		err = fmt.Errorf("payload differs: %d bytes", len(got))
	}
	return err
}

// fakeTarget negotiates by hand so tests can send frames a real target never would.
func fakeTarget(t *testing.T, bridgeOpt Options, permitted []string) (*Conn, mcp.Connection) {
	t.Helper()
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	peer, _ := a.Connect(ctx)
	go func() {
		peer.Read(ctx) // hello
		writeFrame(ctx, peer, "hello", Frame{Version: 1, Features: permitted})
		for {
			m, err := peer.Read(ctx)
			if err != nil {
				return
			}
			if r, ok := m.(*jsonrpc.Request); ok && r.Method == prefix+"listen" {
				var f Frame
				json.Unmarshal(r.Params, &f)
				writeFrame(ctx, peer, "listen_result", Frame{Forward: f.Forward, Port: 9})
			}
			if r, ok := m.(*jsonrpc.Request); ok && r.Method == prefix+"ready" {
				writeFrame(ctx, peer, "ready", Frame{})
				return
			}
		}
	}()
	c, err := Client(b, bridgeOpt).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); peer.Close() })
	return c.(*Conn), peer
}

func TestTargetCannotChooseDestinations(t *testing.T) {
	var opt Options
	opt.Features = []string{ForwardRemote}
	opened := make(chan *Incoming, 1)
	opt.Handle(KindForward, func(ctx context.Context, in *Incoming) { opened <- in; in.Reject(CodeFailed) })
	opt.Setup = func(ctx context.Context, c *Conn) error { _, err := c.Listen(ctx, 1, "127.0.0.1", 0); return err }
	for _, tc := range []struct {
		name  string
		f     Frame
		alive bool
	}{
		{"unregistered forward", Frame{ID: 2, Kind: KindForward, Forward: 99}, false},
		{"destination named by target", Frame{ID: 2, Kind: KindForward, Forward: 1, Host: "evil.example", Port: 22}, false},
		{"socks from target", Frame{ID: 2, Kind: KindSOCKS, Forward: 1, Host: "evil.example", Port: 22}, false},
		{"HTTP proxy not negotiated", Frame{ID: 2, Kind: KindConnect, Host: "evil.example", Port: 443}, false},
		{"bridge ID parity", Frame{ID: 3, Kind: KindForward, Forward: 1}, false},
		{"registered forward", Frame{ID: 2, Kind: KindForward, Forward: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, peer := fakeTarget(t, opt, []string{ForwardRemote})
			writeFrame(context.Background(), peer, "open", tc.f)
			select {
			case <-c.Done():
				if tc.alive {
					t.Fatal("valid open closed the session")
				}
			case in := <-opened:
				if !tc.alive || in.Host != "" || in.Forward != 1 {
					t.Fatalf("opened %+v", in)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no outcome")
			}
		})
	}
	// A forward closed while the target accepted is refused without ending the session.
	c, peer := fakeTarget(t, opt, []string{ForwardRemote})
	c.Unlisten(1)
	writeFrame(context.Background(), peer, "open", Frame{ID: 2, Kind: KindForward, Forward: 1})
	for {
		m, err := peer.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if r := m.(*jsonrpc.Request); r.Method == prefix+"open_result" {
			if !strings.Contains(string(r.Params), CodeClosed) {
				t.Fatalf("result %s", r.Params)
			}
			break
		}
	}
	select {
	case <-c.Done():
		t.Fatal("session closed")
	default:
	}
}

func TestListenValidationAndLimit(t *testing.T) {
	var targetOpt Options
	targetOpt.Features = []string{ForwardRemote}
	targetOpt.Listen = func(c *Conn, forward uint64, bind string, port int) (int, error) {
		if port == 1 {
			return 0, &Error{Code: CodeInUse}
		}
		return port, nil
	}
	targetOpt.Unlisten = func(uint64) {}
	bridge, _ := pair(t, Options{Features: []string{ForwardRemote}}, targetOpt)
	ctx := context.Background()
	if _, err := bridge.Listen(ctx, 1, "127.0.0.1", 1); Code(err) != CodeInUse {
		t.Fatalf("in use: %v", err)
	}
	for i := uint64(2); i < 2+MaxForwards; i++ {
		if _, err := bridge.Listen(ctx, i, "::1", 1000+int(i)); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := bridge.Listen(ctx, 100, "127.0.0.1", 2000); Code(err) != CodeLimit {
		t.Fatalf("limit: %v", err)
	}
	bridge.Unlisten(2)
	if _, err := bridge.Listen(ctx, 101, "127.0.0.1", 2001); err != nil {
		t.Fatalf("after unlisten: %v", err)
	}
	if _, err := bridge.Listen(ctx, 50, "127.0.0.1", 2002); err == nil {
		t.Fatal("decreasing forward ID accepted")
	}
	// A non-loopback bind is a protocol violation on the target.
	bridge.send("listen", Frame{Forward: 200, Bind: "0.0.0.0", Port: 80})
	select {
	case <-bridge.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("non-loopback listen accepted")
	}
}

func TestStreamLimitsAndValidation(t *testing.T) {
	var targetOpt Options
	targetOpt.Features = []string{ForwardLocal}
	release := make(chan struct{})
	defer close(release)
	targetOpt.Handle(KindForward, func(ctx context.Context, in *Incoming) {
		c, err := in.Accept()
		if err == nil {
			<-release
			c.Close()
		}
	})
	bridge, _ := pair(t, Options{Features: []string{ForwardLocal}}, targetOpt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < MaxStreams; i++ {
		if _, err := bridge.Dial(ctx, KindForward, "localhost", 80, 1); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := bridge.Dial(ctx, KindForward, "localhost", 80, 1); err != ErrLimit {
		t.Fatalf("limit: %v", err)
	}
	s, err := bridge.newStream(201, true)
	if err == nil {
		s.stop()
	}
	b2, _ := pair(t, Options{Features: []string{ForwardLocal}}, Options{Features: []string{ForwardLocal}})
	s, err = b2.newStream(2, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	for _, tc := range []struct {
		kind string
		f    Frame
	}{
		{"data", Frame{ID: 2, Data: "??"}}, {"data", Frame{ID: 2, Data: strings.Repeat("A", 24*1024)}},
		{"window", Frame{ID: 2, Bytes: 1}}, {"window", Frame{ID: 2, Bytes: -1}},
		{"open", Frame{ID: 0}}, {"unknown", Frame{ID: 2}}, {"listen", Frame{Forward: 1, Bind: "127.0.0.1"}},
		{"ready", Frame{}}, {"open_result", Frame{ID: 2}}, {"data", Frame{ID: 100, Data: "AA=="}},
	} {
		if b2.handle(tc.kind, tc.f) == nil {
			t.Fatalf("accepted %s %+v", tc.kind, tc.f)
		}
	}
}

func TestBlockedStreamDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := &Conn{ctx: ctx, cancel: cancel, streams: map[uint64]*stream{}, high: make(chan packet, 1), data: make(chan packet, 1)}
	s, err := mux.newStream(1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	c := &streamConn{s: s}
	c.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
	// The data writer is stalled: completion must respect the caller's deadline.
	_, err = c.Write([]byte("pending"))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

func TestHistoryPaging(t *testing.T) {
	var h History[uint64]
	for i := 0; i < 105; i++ {
		h.Add(func(seq uint64) uint64 { return seq })
	}
	events, next, truncated, err := h.Page(0, 2)
	if err != nil || !truncated || next != 7 || len(events) != 2 || events[0] != 6 {
		t.Fatalf("%v %d %v %v", events, next, truncated, err)
	}
	events, next, truncated, _ = h.Page(103, 0)
	if truncated || next != 105 || len(events) != 2 {
		t.Fatalf("%v %d %v", events, next, truncated)
	}
	if _, _, _, err = h.Page(0, 101); err == nil {
		t.Fatal("limit accepted")
	}
	if !h.Merge(func(last *uint64) bool { *last = 0; return true }) {
		t.Fatal("merge")
	}
}

func TestDescribe(t *testing.T) {
	for code, want := range map[string][2]string{
		CodeNotAllowed:  {Denied, ReasonNotAllowed},
		CodeLimit:       {Denied, ReasonLimit},
		CodeClosed:      {Denied, "forward closed"},
		CodeRefused:     {Failed, "connection refused"},
		CodeUnreachable: {Failed, "host unreachable"},
		CodeTimeout:     {Failed, "connection timeout"},
		CodeFailed:      {Failed, "connection failed"},
		CodeInUse:       {Failed, "connection failed"},
	} {
		if result, reason := Describe(code); result != want[0] || reason != want[1] {
			t.Errorf("Describe(%s) = %s, %s; want %s, %s", code, result, reason, want[0], want[1])
		}
	}
}

// tcpPair returns both ends of a loopback TCP connection.
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c, err := net.Dial("tcp4", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); s.Close() })
	return c, s
}

func TestRelayPassesHalfClose(t *testing.T) {
	client, a := tcpPair(t)
	b, server := tcpPair(t)
	done := make(chan struct{})
	go func() { Relay(a, b, RelayOptions{}); close(done) }()
	client.Write([]byte("ping"))
	CloseWrite(client)
	got, _ := io.ReadAll(server)
	if string(got) != "ping" {
		t.Fatalf("server read %q", got)
	}
	server.Write([]byte("pong"))
	CloseWrite(server)
	got, _ = io.ReadAll(client)
	if string(got) != "pong" {
		t.Fatalf("client read %q", got)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not end after both half-closes")
	}
}

func TestRelayLingerAndIdle(t *testing.T) {
	client, a := tcpPair(t)
	b, _ := tcpPair(t)
	done := make(chan struct{})
	go func() { Relay(a, b, RelayOptions{Idle: time.Minute, Linger: 50 * time.Millisecond}); close(done) }()
	CloseWrite(client) // one direction ends; the silent other side is cut after Linger
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("linger did not end the relay")
	}

	client, a = tcpPair(t)
	b, _ = tcpPair(t)
	done = make(chan struct{})
	go func() { Relay(a, b, RelayOptions{Idle: 50 * time.Millisecond}); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("idle timeout did not end the relay")
	}
	_ = client
}
