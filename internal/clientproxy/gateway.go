// Package clientproxy carries authenticated HTTP proxy traffic over the client channel.
package clientproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

type Event struct {
	Seq          uint64 `json:"seq"`
	Time         string `json:"time"`
	ConnectionID uint64 `json:"connection_id"`
	Host         string `json:"host,omitempty"`
	Port         int    `json:"port,omitempty"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	Count        int    `json:"count,omitempty"`
}
type StatusInput struct {
	AfterSeq uint64 `json:"after_seq,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}
type Status struct {
	Enabled           bool     `json:"enabled"`
	State             string   `json:"state"`
	AllowHosts        []string `json:"allow_hosts"`
	ActiveConnections int      `json:"active_connections"`
	Events            []Event  `json:"events"`
	NextSeq           uint64   `json:"next_seq"`
	Truncated         bool     `json:"truncated"`
}

// Gateway is the bridge side of the HTTP proxy: it checks destinations and connects on the client network.
type Gateway struct {
	rules     policy.NameRules
	history   channel.History[Event]
	mu        sync.Mutex
	mux       *channel.Conn
	proxy     func(*http.Request) (*url.URL, error)
	tlsConfig *tls.Config
}

func NewGateway(rules []string) (*Gateway, error) {
	if len(rules) == 0 {
		return nil, errors.New("--proxy requires --proxy-allow-host")
	}
	parsed, e := policy.ParseNameRules("--proxy-allow-host", rules)
	if e != nil {
		return nil, e
	}
	return &Gateway{rules: parsed, proxy: snapshotProxy(), tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, nil
}

// Register enables the HTTP proxy on the bridge's channel options.
func (g *Gateway) Register(o *channel.Options) {
	o.Enable(channel.HTTPProxy)
	o.Handle(channel.KindHTTP, g.open)
	o.Handle(channel.KindConnect, g.open)
	o.OnReport(g.report)
	o.OnAttach(func(c *channel.Conn) { g.mu.Lock(); g.mux = c; g.mu.Unlock() })
}
func (g *Gateway) allowed(host string, port int) bool { return g.rules.Allow(host, port) }
func (g *Gateway) report(kind string, f channel.Frame) {
	if f.Forward != 0 {
		return
	}
	switch kind {
	case "limit":
		g.record(0, f.Host, f.Port, "denied", "connection limit reached")
	case "auth_failed":
		g.record(0, "", 0, "failed", "authentication")
	}
}
func (g *Gateway) record(id uint64, host string, port int, status, reason string) {
	// Reasons are fixed strings, never raw network errors containing credentials.
	if status == "failed" && reason == "authentication" && g.history.Merge(func(last *Event) bool {
		if last.Reason != reason {
			return false
		}
		last.Count++
		return true
	}) {
		return
	}
	g.history.Add(func(seq uint64) Event {
		return Event{Seq: seq, Time: time.Now().Format(time.RFC3339), ConnectionID: id, Host: host, Port: port, Status: status, Reason: reason, Count: 1}
	})
	log.Printf("proxy: id=%d host=%q port=%d status=%s reason=%s", id, host, port, status, reason)
}
func (g *Gateway) Status(in StatusInput) (Status, error) {
	events, next, truncated, e := g.history.Page(in.AfterSeq, in.Limit)
	if e != nil {
		return Status{}, e
	}
	o := Status{Enabled: true, State: "disconnected", AllowHosts: append([]string{}, g.rules...), Events: events, NextSeq: next, Truncated: truncated}
	g.mu.Lock()
	mux := g.mux
	g.mu.Unlock()
	if mux != nil {
		select {
		case <-mux.Done():
		default:
			o.State = "ready"
			o.ActiveConnections = mux.Active()
		}
	}
	return o, nil
}
func validDestination(host string, port int) bool {
	return host != "" && len(host) <= 253 && !strings.ContainsAny(host, "/\\\r\n\t @") && port > 0 && port <= 65535
}
func (g *Gateway) open(ctx context.Context, in *channel.Incoming) {
	if !validDestination(in.Host, in.Port) || !g.allowed(in.Host, in.Port) {
		g.record(in.ID, in.Host, in.Port, "denied", "destination not allowed")
		in.Reject(channel.CodeNotAllowed)
		return
	}
	g.record(in.ID, in.Host, in.Port, "allowed", "destination allowed")
	if in.Kind == channel.KindHTTP {
		conn, e := in.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		g.serveHTTP(ctx, conn, in)
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	external, e := g.connect(dialCtx, in.Host, in.Port)
	if e != nil {
		g.record(in.ID, in.Host, in.Port, "failed", "upstream connection failed")
		in.Reject(channel.CodeFailed)
		return
	}
	defer external.Close()
	stopClose := context.AfterFunc(ctx, func() { external.Close() })
	defer stopClose()
	conn, e := in.Accept()
	if e != nil {
		return
	}
	defer conn.Close()
	tunnel(conn, external)
}
func (g *Gateway) serveHTTP(ctx context.Context, c net.Conn, f *channel.Incoming) {
	tr := &http.Transport{Proxy: g.proxy, DisableKeepAlives: true, TLSClientConfig: g.tlsConfig.Clone(), ResponseHeaderTimeout: 30 * time.Second}
	defer tr.CloseIdleConnections()
	headerLimit := &io.LimitedReader{R: c, N: 32*1024 + 1}
	br := bufio.NewReader(headerLimit)
	for {
		headerLimit.N = 32*1024 + 1
		c.SetReadDeadline(time.Now().Add(30 * time.Second))
		req, e := http.ReadRequest(br)
		if e != nil || headerLimit.N == 0 {
			return
		}
		headerLimit.N = 1<<63 - 1
		c.SetReadDeadline(time.Time{})
		req.Body = &idleBody{ReadCloser: req.Body, c: c}
		req.RequestURI = ""
		req.URL.Scheme = "http"
		req.URL.Host = net.JoinHostPort(f.Host, strconv.Itoa(f.Port))
		req.Host = req.URL.Host
		req = req.WithContext(ctx)
		stripHop(req.Header)
		resp, e := tr.RoundTrip(req)
		if e != nil {
			g.record(f.ID, f.Host, f.Port, "failed", "HTTP upstream failed")
			io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			return
		}
		stripHop(resp.Header)
		e = resp.Write(&idleWriter{c})
		resp.Body.Close()
		if e != nil || req.Close || resp.Close {
			return
		}
	}
}
func (g *Gateway) connect(ctx context.Context, host string, port int) (net.Conn, error) {
	target := net.JoinHostPort(host, strconv.Itoa(port))
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: target}}
	pu, e := g.proxy(req)
	if e != nil {
		return nil, e
	}
	d := &net.Dialer{Timeout: 30 * time.Second}
	if pu == nil {
		return d.DialContext(ctx, "tcp", target)
	}
	if pu.Scheme != "http" && pu.Scheme != "https" {
		return nil, errors.New("unsupported upstream proxy")
	}
	addr := pu.Host
	if pu.Port() == "" {
		p := "80"
		if pu.Scheme == "https" {
			p = "443"
		}
		addr = net.JoinHostPort(pu.Hostname(), p)
	}
	c, e := d.DialContext(ctx, "tcp", addr)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	c.SetDeadline(time.Now().Add(30 * time.Second))
	if pu.Scheme == "https" {
		conf := g.tlsConfig.Clone()
		conf.ServerName = pu.Hostname()
		tc := tls.Client(c, conf)
		if e = tc.HandshakeContext(ctx); e != nil {
			return nil, e
		}
		c = tc
	}
	r := &http.Request{Method: "CONNECT", URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if pu.User != nil {
		pass, _ := pu.User.Password()
		r.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(pu.User.Username()+":"+pass)))
	}
	if e = r.Write(c); e != nil {
		return nil, e
	}
	br := bufio.NewReader(c)
	resp, e := http.ReadResponse(br, r)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("upstream CONNECT rejected")
	}
	c.SetDeadline(time.Time{})
	ok = true
	return &bufferedConn{Conn: c, r: br}, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }
func (c *bufferedConn) CloseWrite() error {
	if x, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return x.CloseWrite()
	}
	return nil
}
func tunnel(a, b net.Conn) {
	touch := func() { deadline := time.Now().Add(5 * time.Minute); a.SetDeadline(deadline); b.SetDeadline(deadline) }
	touch()
	done := make(chan struct{}, 2)
	copyOne := func(dst, src net.Conn) {
		io.Copy(&activityWriter{Conn: dst, touch: touch}, &activityReader{Conn: src, touch: touch})
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyOne(a, b)
	go copyOne(b, a)
	<-done
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		a.Close()
		b.Close()
		<-done
	}
}

type idleWriter struct{ net.Conn }

func (w *idleWriter) Write(b []byte) (int, error) {
	w.SetWriteDeadline(time.Now().Add(5 * time.Minute))
	return w.Conn.Write(b)
}

type idleReader struct{ net.Conn }

func (r *idleReader) Read(b []byte) (int, error) {
	r.SetReadDeadline(time.Now().Add(5 * time.Minute))
	return r.Conn.Read(b)
}
func stripHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, k := range strings.Split(v, ",") {
			h.Del(strings.TrimSpace(k))
		}
	}
	for _, k := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}

// snapshotProxy avoids the process-wide cache in http.ProxyFromEnvironment.
func snapshotProxy() func(*http.Request) (*url.URL, error) {
	hp, sp, np := firstEnv("HTTP_PROXY", "http_proxy"), firstEnv("HTTPS_PROXY", "https_proxy"), firstEnv("NO_PROXY", "no_proxy")
	return func(r *http.Request) (*url.URL, error) {
		if bypass(r.URL, np) {
			return nil, nil
		}
		raw := hp
		if r.URL.Scheme == "https" {
			raw = sp
		}
		if raw == "" {
			return nil, nil
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		u, e := url.Parse(raw)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("invalid upstream proxy configuration")
		}
		return u, nil
	}
}

type idleBody struct {
	io.ReadCloser
	c net.Conn
}

func (b *idleBody) Read(p []byte) (int, error) {
	b.c.SetReadDeadline(time.Now().Add(5 * time.Minute))
	return b.ReadCloser.Read(p)
}

// Successful traffic in either direction refreshes both halves of a tunnel.
type activityReader struct {
	net.Conn
	touch func()
}

func (r *activityReader) Read(b []byte) (int, error) {
	n, e := r.Conn.Read(b)
	if n > 0 {
		r.touch()
	}
	return n, e
}

type activityWriter struct {
	net.Conn
	touch func()
}

func (w *activityWriter) Write(b []byte) (int, error) {
	n, e := w.Conn.Write(b)
	if n > 0 {
		w.touch()
	}
	return n, e
}
