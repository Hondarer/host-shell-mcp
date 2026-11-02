package clientproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/testutil"
)

func pair(t *testing.T, rules []string) (*Target, *Gateway, *channel.Conn, *channel.Conn) {
	t.Helper()
	target, err := NewTarget()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	gateway, err := NewGateway(rules)
	if err != nil {
		t.Fatal(err)
	}
	gateway.proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	var remote mcp.Connection
	var re error
	done := make(chan struct{})
	var targetOptions, bridgeOptions channel.Options
	target.Register(&targetOptions)
	gateway.Register(&bridgeOptions)
	go func() { remote, re = channel.Target(a, targetOptions).Connect(ctx); close(done) }()
	bridge, err := channel.Client(b, bridgeOptions).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if re != nil {
		t.Fatal(re)
	}
	t.Cleanup(func() { remote.Close(); bridge.Close() })
	return target, gateway, remote.(*channel.Conn), bridge.(*channel.Conn)
}
func proxyClient(t *testing.T, target *Target) *http.Client {
	t.Helper()
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
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
			resp, err := client.Post(endpoint, "application/octet-stream", bytes.NewReader(payload))
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err != nil || !bytes.Equal(b, payload) {
				t.Errorf("%s response differs: %d, %v", endpoint, len(b), err)
			}
		})
	}
	// The additional data channel must not consume normal MCP messages.
	wg.Go(func() {
		if err := bridge.Write(context.Background(), &jsonrpc.Request{Method: "notifications/test"}); err != nil {
			t.Error(err)
		}
	})
	msg, err := remote.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.(*jsonrpc.Request).Method != "notifications/test" {
		t.Fatal("MCP message lost")
	}
	wg.Wait()
}
func TestDeniedAndAuthentication(t *testing.T) {
	target, g, _, _ := pair(t, []string{"allowed.example:443"})
	c := proxyClient(t, target)
	r, err := c.Get("http://denied.example/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("status %d", r.StatusCode)
	}
	u, _ := url.Parse(target.URL)
	u.User = nil
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	defer tr.CloseIdleConnections()
	r, err = tr.RoundTrip(mustRequest(t, "http://denied.example/"))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 407 {
		t.Fatalf("auth status %d", r.StatusCode)
	}
	status, err := g.Status(StatusInput{})
	deadline := time.Now().Add(time.Second)
	for len(status.Events) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		status, err = g.Status(StatusInput{})
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Events) < 2 || status.Events[0].Status != "denied" {
		t.Fatalf("events %+v", status.Events)
	}
}
func mustRequest(t *testing.T, u string) *http.Request {
	t.Helper()
	r, err := http.NewRequest("GET", u, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestHalfClose(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.ReadAll(c)
		c.Write([]byte("after eof"))
	}()
	_, _, remote, _ := pair(t, []string{"127.0.0.1:*"})
	addr := listener.Addr().(*net.TCPAddr)
	c, err := remote.Dial(context.Background(), channel.KindConnect, "127.0.0.1", addr.Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("hello"))
	c.(interface{ CloseWrite() error }).CloseWrite()
	b, err := io.ReadAll(c)
	if err != nil || string(b) != "after eof" {
		t.Fatalf("%q %v", b, err)
	}
}
func TestSettingsAndStatus(t *testing.T) {
	for _, rules := range [][]string{nil, {"10.0.0.0/8"}, {"unix:/tmp/x"}, {"x\n"}} {
		if _, err := NewGateway(rules); err == nil {
			t.Fatalf("accepted %q", rules)
		}
	}
	g, err := NewGateway([]string{"*.example.com:443"})
	if err != nil {
		t.Fatal(err)
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
	target, err := NewTarget()
	if err != nil {
		t.Fatal(err)
	}
	target.Close()
	if strings.Contains(target.Mask(target.URL+" "+target.auth), target.token) {
		t.Fatal("secret leaked")
	}
}

func TestUpstreamHTTPAndCONNECT(t *testing.T) {
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		c, err := echo.Accept()
		if err != nil {
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
			external, err := net.Dial("tcp", r.Host)
			if err != nil {
				t.Error(err)
				return
			}
			defer external.Close()
			c, b, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
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
	resp, err := proxyClient(t, target).Get("http://allowed.example/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(b) != "upstream HTTP" {
		t.Fatalf("%q %v", b, err)
	}
	addr := echo.Addr().(*net.TCPAddr)
	c, err := remote.Dial(context.Background(), channel.KindConnect, "127.0.0.1", addr.Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = c.Write([]byte("echo")); err != nil {
		t.Fatal(err)
	}
	b = make([]byte, 4)
	if _, err = io.ReadFull(c, b); err != nil || string(b) != "echo" {
		t.Fatalf("%q %v", b, err)
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
		u, err := proxy(r)
		if err != nil {
			t.Fatal(err)
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
func TestHTTPSUpstream(t *testing.T) {
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		c, err := echo.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			external, err := net.Dial("tcp", echo.Addr().String())
			if err != nil {
				t.Error(err)
				return
			}
			defer external.Close()
			c, b, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
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
	r, err := proxyClient(t, target).Get("http://allowed.example/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil || string(b) != "HTTPS upstream" {
		t.Fatalf("%q %v", b, err)
	}
	c, err := remote.Dial(context.Background(), channel.KindConnect, "allowed.example", 443, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte("TLS"))
	b = make([]byte, 3)
	if _, err = io.ReadFull(c, b); err != nil || string(b) != "TLS" {
		t.Fatalf("%q %v", b, err)
	}
}
func TestConcurrentConnectionLimit(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); <-release }()
		}
	}()
	_, g, remote, _ := pair(t, []string{"127.0.0.1:*"})
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections := make(chan net.Conn, channel.MaxStreams)
	var wg sync.WaitGroup
	for i := 0; i < channel.MaxStreams; i++ {
		wg.Go(func() {
			c, err := remote.Dial(ctx, channel.KindConnect, "127.0.0.1", port, 0)
			if err != nil {
				t.Error(err)
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
	if status.ActiveConnections != channel.MaxStreams {
		t.Fatalf("active %d", status.ActiveConnections)
	}
	if c, err := remote.Dial(ctx, channel.KindConnect, "127.0.0.1", port, 0); err == nil {
		c.Close()
		t.Fatal("capacity exceeded")
	}
	close(connections)
	for c := range connections {
		c.Close()
	}
	testutil.WaitFor(t, time.Second, "connections not released", func() bool {
		status, _ = g.Status(StatusInput{})
		return status.ActiveConnections == 0
	})
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
	for i := 0; i < channel.MaxStreams; i++ {
		c, err := net.DialTimeout("tcp", u.Host, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	extra, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	extra.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 128)
	n, err := extra.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "503") {
		t.Fatalf("capacity response: %q", buf[:n])
	}
	testutil.WaitFor(t, time.Second, "capacity diagnostic missing", func() bool {
		s, _ := g.Status(StatusInput{})
		return slices.ContainsFunc(s.Events, func(ev Event) bool { return ev.Reason == "connection limit reached" })
	})
}
