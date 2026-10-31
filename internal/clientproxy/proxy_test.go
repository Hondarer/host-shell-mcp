package clientproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func pair(t *testing.T, rules []string) (*Target, *Gateway, *channel, *channel) {
	t.Helper()
	target, e := NewTarget()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(target.Close)
	gateway, e := NewGateway(rules)
	if e != nil {
		t.Fatal(e)
	}
	gateway.proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	var remote mcp.Connection
	var re error
	done := make(chan struct{})
	go func() { remote, re = Remote(a, target).Connect(ctx); close(done) }()
	bridge, e := Bridge(b, gateway).Connect(ctx)
	if e != nil {
		t.Fatal(e)
	}
	<-done
	if re != nil {
		t.Fatal(re)
	}
	t.Cleanup(func() { remote.Close(); bridge.Close() })
	return target, gateway, remote.(*channel), bridge.(*channel)
}
func proxyClient(t *testing.T, target *Target) *http.Client {
	t.Helper()
	u, e := url.Parse(target.URL)
	if e != nil {
		t.Fatal(e)
	}
	tr := &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}
func TestHTTPAndCONNECTWithMCP(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 255, 1, 128, 10}, 200000)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !bytes.Equal(b, payload) {
			t.Errorf("request body differs: %d", len(b))
		}
		w.Write(payload)
	})
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	target, _, remote, bridge := pair(t, []string{"127.0.0.1:*"})
	client := proxyClient(t, target)
	var wg sync.WaitGroup
	for _, endpoint := range []string{plain.URL, secure.URL} {
		wg.Go(func() {
			resp, e := client.Post(endpoint, "application/octet-stream", bytes.NewReader(payload))
			if e != nil {
				t.Error(e)
				return
			}
			defer resp.Body.Close()
			b, e := io.ReadAll(resp.Body)
			if e != nil || !bytes.Equal(b, payload) {
				t.Errorf("%s response differs: %d, %v", endpoint, len(b), e)
			}
		})
	}
	// The additional data channel must not consume normal MCP messages.
	wg.Go(func() {
		if e := bridge.Write(context.Background(), &jsonrpc.Request{Method: "notifications/test"}); e != nil {
			t.Error(e)
		}
	})
	msg, e := remote.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if msg.(*jsonrpc.Request).Method != "notifications/test" {
		t.Fatal("MCP message lost")
	}
	wg.Wait()
}
func TestDeniedAndAuthentication(t *testing.T) {
	target, g, _, _ := pair(t, []string{"allowed.example:443"})
	c := proxyClient(t, target)
	r, e := c.Get("http://denied.example/")
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("status %d", r.StatusCode)
	}
	u, _ := url.Parse(target.URL)
	u.User = nil
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	defer tr.CloseIdleConnections()
	r, e = tr.RoundTrip(mustRequest(t, "http://denied.example/"))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 407 {
		t.Fatalf("auth status %d", r.StatusCode)
	}
	status, e := g.Status(StatusInput{})
	deadline := time.Now().Add(time.Second)
	for len(status.Events) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		status, e = g.Status(StatusInput{})
	}
	if e != nil {
		t.Fatal(e)
	}
	if len(status.Events) < 2 || status.Events[0].Status != "denied" {
		t.Fatalf("events %+v", status.Events)
	}
}
func mustRequest(t *testing.T, u string) *http.Request {
	t.Helper()
	r, e := http.NewRequest("GET", u, nil)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestHalfClose(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.ReadAll(c)
		c.Write([]byte("after eof"))
	}()
	_, _, remote, _ := pair(t, []string{"127.0.0.1:*"})
	addr := listener.Addr().(*net.TCPAddr)
	c, e := remote.dial(context.Background(), "127.0.0.1", addr.Port, "connect")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.Write([]byte("hello"))
	c.(interface{ CloseWrite() error }).CloseWrite()
	b, e := io.ReadAll(c)
	if e != nil || string(b) != "after eof" {
		t.Fatalf("%q %v", b, e)
	}
}
func TestSettingsAndStatus(t *testing.T) {
	for _, rules := range [][]string{nil, {"10.0.0.0/8"}, {"unix:/tmp/x"}, {"x\n"}} {
		if _, e := NewGateway(rules); e == nil {
			t.Fatalf("accepted %q", rules)
		}
	}
	g, e := NewGateway([]string{"*.example.com:443"})
	if e != nil {
		t.Fatal(e)
	}
	if !g.allowed("a.example.com", 443) || g.allowed("a.example.com", 80) {
		t.Fatal("rules")
	}
	for i := 0; i < 105; i++ {
		g.record(uint64(i), "example.com", 443, "denied", "destination not allowed")
	}
	s, _ := g.Status(StatusInput{Limit: 2})
	if !s.Truncated || s.NextSeq != 7 || len(s.Events) != 2 {
		t.Fatalf("%+v", s)
	}
	target, e := NewTarget()
	if e != nil {
		t.Fatal(e)
	}
	target.Close()
	if strings.Contains(target.Mask(target.URL+" "+target.auth), target.token) {
		t.Fatal("secret leaked")
	}
}

func TestUpstreamHTTPAndCONNECT(t *testing.T) {
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		c, e := echo.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Error("missing upstream credentials")
		}
		if r.Method == "CONNECT" {
			external, e := net.Dial("tcp", r.Host)
			if e != nil {
				t.Error(e)
				return
			}
			defer external.Close()
			c, b, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			defer c.Close()
			b.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			b.Flush()
			tunnel(c, external)
			return
		}
		if r.URL.Host != "allowed.example:80" {
			t.Errorf("unexpected destination %s", r.URL)
		}
		w.Write([]byte("upstream HTTP"))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	u.User = url.UserPassword("user", "pass")
	target, g, remote, _ := pair(t, []string{"allowed.example:*", "127.0.0.1:*"})
	g.proxy = func(*http.Request) (*url.URL, error) { return u, nil }
	resp, e := proxyClient(t, target).Get("http://allowed.example/")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(b) != "upstream HTTP" {
		t.Fatalf("%q %v", b, e)
	}
	addr := echo.Addr().(*net.TCPAddr)
	c, e := remote.dial(context.Background(), "127.0.0.1", addr.Port, "connect")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, e = c.Write([]byte("echo")); e != nil {
		t.Fatal(e)
	}
	b = make([]byte, 4)
	if _, e = io.ReadFull(c, b); e != nil || string(b) != "echo" {
		t.Fatalf("%q %v", b, e)
	}
}
func TestProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://secure-proxy.example:8080")
	t.Setenv("NO_PROXY", ".internal.example,10.0.0.0/8")
	proxy := snapshotProxy()
	for _, tc := range []struct{ u, want string }{
		{"http://public.example", "proxy.example:8080"}, {"https://public.example", "secure-proxy.example:8080"},
		{"http://x.internal.example", ""}, {"http://10.1.2.3", ""}, {"http://localhost", ""},
	} {
		r := mustRequest(t, tc.u)
		u, e := proxy(r)
		if e != nil {
			t.Fatal(e)
		}
		got := ""
		if u != nil {
			got = u.Host
		}
		if got != tc.want {
			t.Fatalf("%s: %s != %s", tc.u, got, tc.want)
		}
	}
	t.Setenv("HTTP_PROXY", "http://changed.example")
	u, _ := proxy(mustRequest(t, "http://public.example"))
	if u.Host != "proxy.example:8080" {
		t.Fatal("not a startup snapshot")
	}
}
func TestStreamLimitsAndValidation(t *testing.T) {
	_, _, remote, bridge := pair(t, []string{"example.com:443"})
	var streams []*stream
	for i := 0; i < maxStreams; i++ {
		s, e := remote.newStream(remote.next.Add(1))
		if e != nil {
			t.Fatal(e)
		}
		streams = append(streams, s)
	}
	if _, e := remote.newStream(remote.next.Add(1)); e == nil {
		t.Fatal("limit not enforced")
	}
	for _, s := range streams {
		s.stop()
	}
	s, e := bridge.newStream(1)
	if e != nil {
		t.Fatal(e)
	}
	defer s.stop()
	for _, tc := range []struct {
		kind string
		f    frame
	}{
		{"data", frame{ID: 1, Data: "??"}}, {"data", frame{ID: 1, Data: strings.Repeat("A", 24*1024)}},
		{"window", frame{ID: 1, Bytes: 1}}, {"window", frame{ID: 1, Bytes: -1}},
		{"open", frame{ID: 0}}, {"unknown", frame{ID: 1}},
	} {
		if bridge.handle(tc.kind, tc.f) == nil {
			t.Fatalf("accepted %s", tc.kind)
		}
	}
}

func TestHTTPSUpstream(t *testing.T) {
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		c, e := echo.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			external, e := net.Dial("tcp", echo.Addr().String())
			if e != nil {
				t.Error(e)
				return
			}
			defer external.Close()
			c, b, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			defer c.Close()
			b.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			b.Flush()
			tunnel(c, external)
			return
		}
		w.Write([]byte("HTTPS upstream"))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	target, g, remote, _ := pair(t, []string{"allowed.example:*"})
	g.proxy = func(*http.Request) (*url.URL, error) { return u, nil }
	g.tlsConfig = upstream.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	r, e := proxyClient(t, target).Get("http://allowed.example/")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || string(b) != "HTTPS upstream" {
		t.Fatalf("%q %v", b, e)
	}
	c, e := remote.dial(context.Background(), "allowed.example", 443, "connect")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte("TLS"))
	b = make([]byte, 3)
	if _, e = io.ReadFull(c, b); e != nil || string(b) != "TLS" {
		t.Fatalf("%q %v", b, e)
	}
}
func TestBlockedStreamDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := &channel{ctx: ctx, cancel: cancel, streams: map[uint64]*stream{}, high: make(chan packet, 1), data: make(chan packet, 1)}
	s, e := mux.newStream(1)
	if e != nil {
		t.Fatal(e)
	}
	defer s.stop()
	c := &streamConn{s: s}
	c.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
	// The data writer is stalled: completion must respect the caller's deadline.
	_, e = c.Write([]byte("pending"))
	if !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline: %v", e)
	}
}

func TestNegotiationMismatch(t *testing.T) {
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	target, e := NewTarget()
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	done := make(chan error, 1)
	go func() { _, e := Remote(a, target).Connect(ctx); done <- e }()
	peer, e := b.Connect(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	if e = writeFrame(ctx, peer, "hello", frame{Version: 2}); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("mismatch accepted")
		}
	case <-ctx.Done():
		t.Fatal("negotiation did not fail")
	}
}

func TestConcurrentConnectionLimit(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); <-release }()
		}
	}()
	_, g, remote, _ := pair(t, []string{"127.0.0.1:*"})
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections := make(chan net.Conn, maxStreams)
	var wg sync.WaitGroup
	for i := 0; i < maxStreams; i++ {
		wg.Go(func() {
			c, e := remote.dial(ctx, "127.0.0.1", port, "connect")
			if e != nil {
				t.Error(e)
				return
			}
			connections <- c
		})
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	status, _ := g.Status(StatusInput{})
	if status.ActiveConnections != maxStreams {
		t.Fatalf("active %d", status.ActiveConnections)
	}
	if c, e := remote.dial(ctx, "127.0.0.1", port, "connect"); e == nil {
		c.Close()
		t.Fatal("capacity exceeded")
	}
	close(connections)
	for c := range connections {
		c.Close()
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, _ = g.Status(StatusInput{})
		if status.ActiveConnections == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("connections not released")
}

func TestIdleHTTPConnectionLimit(t *testing.T) {
	target, g, _, _ := pair(t, []string{"allowed.example:443"})
	u, _ := url.Parse(target.URL)
	var connections []net.Conn
	defer func() {
		for _, c := range connections {
			c.Close()
		}
	}()
	for i := 0; i < maxStreams; i++ {
		c, e := net.DialTimeout("tcp", u.Host, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		connections = append(connections, c)
	}
	extra, e := net.DialTimeout("tcp", u.Host, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer extra.Close()
	extra.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 128)
	n, e := extra.Read(buf)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(buf[:n]), "503") {
		t.Fatalf("capacity response: %q", buf[:n])
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s, _ := g.Status(StatusInput{})
		for _, ev := range s.Events {
			if ev.Reason == "connection limit reached" {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("capacity diagnostic missing")
}
