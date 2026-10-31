package forward

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

func TestParseSpec(t *testing.T) {
	for _, tc := range []struct {
		dir, in string
		want    Spec
	}{
		{Local, "8080:localhost:3000", Spec{Local, "127.0.0.1", 8080, "localhost", 3000}},
		{Remote, "127.0.0.1:0:db.example:5432", Spec{Remote, "127.0.0.1", 0, "db.example", 5432}},
		{Local, "[::1]:8080:[2001:db8::1]:443", Spec{Local, "::1", 8080, "2001:db8::1", 443}},
		{SOCKS, "1080", Spec{SOCKS, "127.0.0.1", 1080, "", 0}},
		{SOCKS, "[::1]:0", Spec{SOCKS, "::1", 0, "", 0}},
	} {
		got, err := ParseSpec(tc.dir, tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%s %q: %+v %v", tc.dir, tc.in, got, err)
		}
	}
	for _, tc := range [][2]string{
		{Local, "0.0.0.0:8080:h:1"}, {Local, "localhost:8080:h:1"}, {Local, "8080:h"}, {Local, "8080:h:0"},
		{Local, "70000:h:1"}, {Local, "8080:2001:db8::1:443"}, {Local, "8080:[h]:1"}, {Remote, "8080::1"},
		{SOCKS, "1080:x"}, {SOCKS, "[::1"}, {SOCKS, ""}, {Local, "8080:a b:1"},
	} {
		if s, err := ParseSpec(tc[0], tc[1]); err == nil {
			t.Fatalf("%s %q accepted: %+v", tc[0], tc[1], s)
		}
	}
	if _, err := NewManager([]Spec{{Direction: SOCKS, Bind: "127.0.0.1"}}, nil, nil); err == nil || !strings.Contains(err.Error(), "--forward-allow-local") {
		t.Fatalf("socks without rules: %v", err)
	}
	many := make([]Spec, channel.MaxForwards+1)
	if _, err := NewManager(many, nil, nil); err == nil {
		t.Fatal("forward limit not enforced")
	}
	if _, err := NewManager(nil, []string{"10.0.0.0/8"}, nil); err == nil {
		t.Fatal("CIDR rule accepted")
	}
}

type session struct {
	manager *Manager
	target  *Target
	bridge  *channel.Conn
	log     string
}

type sessionOptions struct {
	specs                   []Spec
	allowLocal, allowRemote []string
	local, remote           bool
	allowHost               []string
	remotePorts             Ports
	timeout                 time.Duration
	targetDial              func(ctx context.Context, network, addr string) (net.Conn, error)
	// negotiation receives the bridge's connect error instead of failing the test.
	negotiation *error
}

func start(t *testing.T, o sessionOptions) *session {
	t.Helper()
	m, err := NewManager(o.specs, o.allowLocal, o.allowRemote)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Bind(); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(t.TempDir(), "audit.log")
	al, err := audit.Open(logFile)
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.New(policy.Options{AllowHosts: o.allowHost}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tg := NewTarget(o.local, o.remote, pol, al)
	tg.RestrictRemotePorts(o.remotePorts)
	if o.timeout != 0 {
		m.timeout, tg.timeout = o.timeout, o.timeout
	}
	if o.targetDial != nil {
		tg.dial = o.targetDial
	}
	var bo, to channel.Options
	m.Register(&bo)
	tg.Register(&to)
	a, b := mcp.NewInMemoryTransports()
	// The channel lives as long as the Connect context.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var targetConn mcp.Connection
	var te error
	done := make(chan struct{})
	go func() { targetConn, te = channel.Target(a, to).Connect(ctx); close(done) }()
	bridgeConn, be := channel.Client(b, bo).Connect(ctx)
	<-done
	if o.negotiation != nil {
		*o.negotiation = be
		if be != nil {
			t.Cleanup(func() { m.Close(); tg.Close() })
			return nil
		}
	}
	if be != nil || te != nil {
		t.Fatalf("negotiation: %v %v", be, te)
	}
	s := &session{manager: m, target: tg, bridge: bridgeConn.(*channel.Conn), log: logFile}
	t.Cleanup(func() { bridgeConn.Close(); targetConn.Close(); m.Close(); tg.Close() })
	return s
}

// echoServer echoes each connection and closes its write half at EOF.
func echoServer(t *testing.T, network, addr string) int {
	t.Helper()
	l, err := net.Listen(network, addr)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.Copy(c, c)
				c.(*net.TCPConn).CloseWrite()
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func closedPort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

var payload = bytes.Repeat([]byte{0, 255, 13, 10, 'x'}, 300000)

func roundTrip(t *testing.T, c net.Conn) {
	t.Helper()
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	go func() {
		c.Write(payload)
		c.(*net.TCPConn).CloseWrite()
	}()
	got, err := io.ReadAll(c)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("round trip: %d bytes, %v", len(got), err)
	}
}

func dial(t *testing.T, host string, port int) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (s *session) forward(t *testing.T, id uint64) Info {
	t.Helper()
	st, _ := s.manager.Status(StatusInput{})
	for _, f := range st.Forwards {
		if f.ForwardID == id {
			return f
		}
	}
	t.Fatalf("forward %d missing: %+v", id, st.Forwards)
	return Info{}
}

func (s *session) waitEvent(t *testing.T, reason string) Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := s.manager.Status(StatusInput{Limit: 100})
		for _, e := range st.Events {
			if e.Reason == reason {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	st, _ := s.manager.Status(StatusInput{Limit: 100})
	t.Fatalf("no %q event: %+v", reason, st.Events)
	return Event{}
}

func TestPersistentForwards(t *testing.T) {
	port := echoServer(t, "tcp4", "127.0.0.1:0")
	s := start(t, sessionOptions{
		specs:  []Spec{{Local, "127.0.0.1", 0, "127.0.0.1", port}, {Remote, "127.0.0.1", 0, "127.0.0.1", port}},
		local:  true,
		remote: true,
	})
	local, remote := s.forward(t, 1), s.forward(t, 2)
	if local.ListenPort == 0 || remote.ListenPort == 0 || !local.Persistent {
		t.Fatalf("ports not assigned: %+v %+v", local, remote)
	}
	info := s.target.Info()
	if len(info.RemoteForwards) != 1 || !strings.HasSuffix(info.RemoteForwards[0].Address, ":"+strconv.Itoa(remote.ListenPort)) {
		t.Fatalf("target info %+v", info)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { roundTrip(t, dial(t, "127.0.0.1", local.ListenPort)) })
		wg.Go(func() { roundTrip(t, dial(t, "127.0.0.1", remote.ListenPort)) })
	}
	wg.Wait()
	// Forwarded connections have no idle timeout.
	c := dial(t, "127.0.0.1", local.ListenPort)
	time.Sleep(300 * time.Millisecond)
	roundTrip(t, c)
	if e := s.waitEvent(t, "connection established"); e.Status != "allowed" {
		t.Fatalf("event %+v", e)
	}
	if err := s.manager.CloseForward(1); err == nil {
		t.Fatal("persistent forward closed")
	}
	b, _ := os.ReadFile(auditFile(t, s.log))
	text := string(b)
	for _, want := range []string{`"msg":"forward_listen"`, `"msg":"forward_connect"`, `"direction":"local"`, `"result":"allowed"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("audit lacks %s: %s", want, text)
		}
	}
	if strings.Contains(text, "xxxxx") {
		t.Fatal("audit recorded forwarded data")
	}
}

// auditFile resolves the daily file the logger writes for base.
func auditFile(t *testing.T, base string) string {
	t.Helper()
	m, _ := filepath.Glob(strings.TrimSuffix(base, ".log") + "*")
	if len(m) == 0 {
		t.Fatal("no audit file")
	}
	return m[0]
}

func socksConnect(t *testing.T, port int, atyp byte, addr []byte, dport int, cmd byte) (net.Conn, byte) {
	t.Helper()
	c := dial(t, "127.0.0.1", port)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte{5, 1, 0})
	r := make([]byte, 2)
	if _, err := io.ReadFull(c, r); err != nil || r[1] != 0 {
		t.Fatalf("method %v %v", r, err)
	}
	req := []byte{5, cmd, 0, atyp}
	if atyp == 3 {
		req = append(req, byte(len(addr)))
	}
	req = append(req, addr...)
	req = binary.BigEndian.AppendUint16(req, uint16(dport))
	c.Write(req)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatalf("reply %v", err)
	}
	return c, reply[1]
}

func TestSOCKS(t *testing.T) {
	port := echoServer(t, "tcp4", "127.0.0.1:0")
	s := start(t, sessionOptions{
		specs:      []Spec{{SOCKS, "127.0.0.1", 0, "", 0}},
		allowLocal: []string{"127.0.0.1", "localhost", "::1", "denied-by-target.example", "127.0.0.2"},
		local:      true,
		allowHost:  []string{"127.0.0.1", "localhost", "::1"},
	})
	sp := s.forward(t, 1).ListenPort
	c, code := socksConnect(t, sp, 1, []byte{127, 0, 0, 1}, port, 1)
	if code != socksSucceeded {
		t.Fatalf("IPv4 reply %d", code)
	}
	roundTrip(t, c)
	c, code = socksConnect(t, sp, 3, []byte("localhost"), port, 1)
	if code == socksSucceeded {
		roundTrip(t, c)
	} else if code != socksRefused { // localhost may resolve to ::1 first where nothing listens
		t.Fatalf("domain reply %d", code)
	}
	if p6 := echoServer6(t); p6 != 0 {
		c, code = socksConnect(t, sp, 4, net.ParseIP("::1").To16(), p6, 1)
		if code != socksSucceeded {
			t.Fatalf("IPv6 reply %d", code)
		}
		roundTrip(t, c)
	}
	for _, tc := range []struct {
		atyp   byte
		addr   []byte
		port   int
		cmd    byte
		want   byte
		reason string
	}{
		{3, []byte("not-in-rules.example"), 80, 1, socksNotAllowed, "destination not allowed"},
		{3, []byte("denied-by-target.example"), 80, 1, socksNotAllowed, "destination not allowed by target"},
		{1, []byte{127, 0, 0, 1}, closedPort(t), 1, socksRefused, "connection refused"},
		{1, []byte{127, 0, 0, 1}, port, 2, socksCommandUnsupport, "unsupported SOCKS request"},
	} {
		c, code := socksConnect(t, sp, tc.atyp, tc.addr, tc.port, tc.cmd)
		c.Close()
		if code != tc.want {
			t.Fatalf("%s: reply %d, want %d", tc.addr, code, tc.want)
		}
		s.waitEvent(t, tc.reason)
	}
	c = dial(t, "127.0.0.1", sp)
	c.Write([]byte{4, 1, 0, 80, 127, 0, 0, 1, 0})
	r := make([]byte, 8)
	if _, err := io.ReadFull(c, r); err != nil || r[1] != 0x5b {
		t.Fatalf("SOCKS4 reply %v %v", r, err)
	}
	c.Close()
	c = dial(t, "127.0.0.1", sp)
	c.Write([]byte{5, 1, 2})
	if _, err := io.ReadFull(c, r[:2]); err != nil || r[1] != 0xff {
		t.Fatalf("auth reply %v %v", r[:2], err)
	}
	c.Close()
}

func echoServer6(t *testing.T) int {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return 0
	}
	l.Close()
	return echoServer(t, "tcp6", "[::1]:0")
}

func TestForwardTools(t *testing.T) {
	port := echoServer(t, "tcp4", "127.0.0.1:0")
	s := start(t, sessionOptions{allowLocal: []string{"127.0.0.1:*"}, allowRemote: []string{"127.0.0.1:" + strconv.Itoa(port)}, local: true, remote: true})
	ctx := context.Background()
	for _, in := range []OpenInput{
		{Direction: "dynamic"},
		{Direction: Local, Host: "denied.example", Port: 80},
		{Direction: Remote, Host: "127.0.0.1", Port: port + 1},
		{Direction: SOCKS, Host: "127.0.0.1", Port: 80},
		{Direction: Local, ListenAddress: "0.0.0.0", Host: "127.0.0.1", Port: port},
		{Direction: Local, Host: "127.0.0.1"},
	} {
		if _, err := s.manager.Open(ctx, in); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
	local, err := s.manager.Open(ctx, OpenInput{Direction: Local, Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := s.manager.Open(ctx, OpenInput{Direction: Remote, Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, dial(t, "127.0.0.1", local.ListenPort))
	roundTrip(t, dial(t, "127.0.0.1", remote.ListenPort))
	// forward_close ends established connections.
	held := dial(t, "127.0.0.1", local.ListenPort)
	held.Write([]byte("x"))
	b := make([]byte, 1)
	held.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = io.ReadFull(held, b); err != nil {
		t.Fatal(err)
	}
	if err = s.manager.CloseForward(local.ForwardID); err != nil {
		t.Fatal(err)
	}
	if _, err = held.Read(b); err == nil {
		t.Fatal("connection survived forward_close")
	}
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local.ListenPort)), time.Second); err == nil {
		c.Close()
		t.Fatal("listener survived forward_close")
	}
	if err = s.manager.CloseForward(remote.ForwardID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(s.target.Info().RemoteForwards) == 0 })
	if err = s.manager.CloseForward(remote.ForwardID); err == nil {
		t.Fatal("closed twice")
	}
	var opened []Info
	for i := 0; i < channel.MaxForwards; i++ {
		f, err := s.manager.Open(ctx, OpenInput{Direction: SOCKS})
		if err != nil {
			t.Fatal(i, err)
		}
		opened = append(opened, f)
	}
	if _, err = s.manager.Open(ctx, OpenInput{Direction: SOCKS}); err == nil {
		t.Fatal("forward limit not enforced")
	}
	s.manager.CloseForward(opened[0].ForwardID)
	if _, err = s.manager.Open(ctx, OpenInput{Direction: Remote, Host: "127.0.0.1", Port: port}); err != nil {
		t.Fatal(err)
	}
	// The target refuses a busy remote listen port.
	busy, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer busy.Close()
	s.manager.CloseForward(opened[1].ForwardID)
	if _, err = s.manager.Open(ctx, OpenInput{Direction: Remote, ListenPort: busy.Addr().(*net.TCPAddr).Port, Host: "127.0.0.1", Port: port}); err == nil || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("busy port: %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRemoteFailuresAndRelease(t *testing.T) {
	before := runtime.NumGoroutine()
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	m, _ := NewManager([]Spec{{Local, "127.0.0.1", busy.Addr().(*net.TCPAddr).Port, "h", 1}}, nil, nil)
	if err = m.Bind(); err == nil {
		t.Fatal("busy persistent port accepted")
	}
	func() {
		s := start(t, sessionOptions{specs: []Spec{{Remote, "127.0.0.1", 0, "127.0.0.1", closedPort(t)}}, remote: true})
		c := dial(t, "127.0.0.1", s.forward(t, 1).ListenPort)
		c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Read(make([]byte, 1)); err == nil {
			t.Fatal("refused destination kept the connection")
		}
		c.Close()
		if e := s.waitEvent(t, "connection refused"); e.Direction != Remote || e.Status != "failed" {
			t.Fatalf("event %+v", e)
		}
		s.bridge.Close()
		waitFor(t, func() bool { return len(s.target.Info().RemoteForwards) == 0 })
	}()
	waitFor(t, func() bool { return runtime.NumGoroutine() <= before+2 })
}

func TestStatusPaging(t *testing.T) {
	m, _ := NewManager(nil, []string{"*"}, nil)
	f := &forward{id: 1, spec: Spec{Direction: Local}}
	for i := 0; i < 105; i++ {
		m.record(f, uint64(i), "h", 1, "denied", "destination not allowed")
	}
	st, err := m.Status(StatusInput{Limit: 2})
	if err != nil || !st.Truncated || st.NextSeq != 7 || len(st.Events) != 2 || st.State != "disconnected" {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err = m.Status(StatusInput{Limit: 101}); err == nil {
		t.Fatal("limit accepted")
	}
}

func TestSOCKSFailuresAndIPv6Bind(t *testing.T) {
	if l, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback")
	} else {
		l.Close()
	}
	port := echoServer(t, "tcp4", "127.0.0.1:0")
	s := start(t, sessionOptions{
		specs:      []Spec{{SOCKS, "::1", 0, "", 0}, {Local, "::1", 0, "denied.example", 80}},
		allowLocal: []string{"*"},
		local:      true,
		allowHost:  []string{"127.0.0.1", "*.invalid"},
	})
	sp := s.forward(t, 1).ListenPort
	c, err := net.DialTimeout("tcp", net.JoinHostPort("::1", strconv.Itoa(sp)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	socks6 := func(atyp byte, addr []byte, dport int) byte {
		c := dial(t, "::1", sp)
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		c.Write([]byte{5, 1, 0})
		io.ReadFull(c, make([]byte, 2))
		req := []byte{5, 1, 0, atyp}
		if atyp == 3 {
			req = append(req, byte(len(addr)))
		}
		req = binary.BigEndian.AppendUint16(append(req, addr...), uint16(dport))
		c.Write(req)
		reply := make([]byte, 10)
		if _, err := io.ReadFull(c, reply[:2]); err != nil {
			t.Fatal(err)
		}
		return reply[1]
	}
	if code := socks6(1, []byte{127, 0, 0, 1}, port); code != socksSucceeded {
		t.Fatalf("over [::1]: %d", code)
	}
	if code := socks6(3, []byte("host-shell-test.invalid"), 80); code != socksHostUnreachable {
		t.Fatalf("unresolvable: %d", code)
	}
	if code := socks6(9, nil, 80); code != socksAddressUnsupport {
		t.Fatalf("address type: %d", code)
	}
	// A local forward to a destination outside the target's --allow-host is closed.
	lc := dial(t, "::1", s.forward(t, 2).ListenPort)
	lc.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := lc.Read(make([]byte, 1)); err == nil {
		t.Fatal("denied destination connected")
	}
	lc.Close()
	s.waitEvent(t, "destination not allowed by target")
}

func TestSOCKSTimeout(t *testing.T) {
	// A destination that never answers ends with the timeout reply.
	s := start(t, sessionOptions{
		specs: []Spec{{SOCKS, "127.0.0.1", 0, "", 0}}, allowLocal: []string{"*"}, local: true, timeout: 200 * time.Millisecond,
		targetDial: func(ctx context.Context, network, addr string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() },
	})
	c, code := socksConnect(t, s.forward(t, 1).ListenPort, 1, []byte{192, 0, 2, 1}, 80, 1)
	c.Close()
	if code != socksTTLExpired {
		t.Fatalf("timeout reply %d", code)
	}
	s.waitEvent(t, "connection timeout")
}

func TestParsePorts(t *testing.T) {
	p, err := ParsePorts([]string{"15432, 20000-20099", "8080"})
	if err != nil || p.String() != "15432, 20000-20099, 8080" {
		t.Fatalf("%v %v", p, err)
	}
	if !p.Contains(20050) || !p.Contains(8080) || p.Contains(20100) || p.Contains(0) {
		t.Fatalf("contains %v", p)
	}
	if p, err = ParsePorts(nil); p != nil || err != nil || !p.Contains(1) {
		t.Fatalf("no ranges: %v %v", p, err)
	}
	for _, v := range []string{"0", "65536", "20-10", "a", "", "1-", "-5", "080", "1-2-3"} {
		if _, err := ParsePorts([]string{v}); err == nil || !strings.Contains(err.Error(), "--allow-forward-remote-port") {
			t.Errorf("%q: %v", v, err)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestRemotePortRanges(t *testing.T) {
	port := echoServer(t, "tcp4", "127.0.0.1:0")
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	taken, free, outside := busy.Addr().(*net.TCPAddr).Port, freePort(t), freePort(t)
	ranges := Ports{{taken, taken}, {free, free}}
	s := start(t, sessionOptions{allowRemote: []string{"127.0.0.1:" + strconv.Itoa(port)}, remote: true, remotePorts: ranges})
	ctx := context.Background()
	// Port 0 skips the busy port and takes the next free one in the ranges.
	f, err := s.manager.Open(ctx, OpenInput{Direction: Remote, Host: "127.0.0.1", Port: port})
	if err != nil || f.ListenPort != free {
		t.Fatalf("port 0: %+v %v (want %d)", f, err, free)
	}
	roundTrip(t, dial(t, "127.0.0.1", f.ListenPort))
	// No free port is left in the ranges.
	if _, err = s.manager.Open(ctx, OpenInput{Direction: Remote, Host: "127.0.0.1", Port: port}); err == nil || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("ranges exhausted: %v", err)
	}
	_, err = s.manager.Open(ctx, OpenInput{Direction: Remote, ListenPort: outside, Host: "127.0.0.1", Port: port})
	if err == nil || !strings.Contains(err.Error(), "listen port not allowed by target (see sys_get_info restrictions)") {
		t.Fatalf("port outside the ranges: %v", err)
	}
	// An explicit port in the ranges is accepted.
	s.manager.CloseForward(f.ForwardID)
	waitFor(t, func() bool { return len(s.target.Info().RemoteForwards) == 0 })
	if f, err = s.manager.Open(ctx, OpenInput{Direction: Remote, ListenPort: free, Host: "127.0.0.1", Port: port}); err != nil || f.ListenPort != free {
		t.Fatalf("port in the ranges: %+v %v", f, err)
	}
	if got := s.target.Info().RemotePorts.String(); got != ranges.String() {
		t.Fatalf("info ports %q", got)
	}
	b, _ := os.ReadFile(auditFile(t, s.log))
	if !strings.Contains(string(b), `"address":"127.0.0.1:`+strconv.Itoa(outside)+`","result":"denied","reason":"listen port not allowed"`) {
		t.Fatalf("audit lacks the refusal: %s", b)
	}
}

func TestPersistentRemotePortNotAllowed(t *testing.T) {
	free, outside := freePort(t), freePort(t)
	var err error
	start(t, sessionOptions{specs: []Spec{{Remote, "127.0.0.1", outside, "127.0.0.1", 1}}, remote: true, remotePorts: Ports{{free, free}}, negotiation: &err})
	if err == nil || !strings.Contains(err.Error(), "--forward-remote on target 127.0.0.1:"+strconv.Itoa(outside)+": listen failed: listen port not allowed by target (see sys_get_info restrictions)") {
		t.Fatalf("persistent forward outside the ranges: %v", err)
	}
}
