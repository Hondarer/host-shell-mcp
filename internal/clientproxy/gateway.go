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
	"sync/atomic"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// Timeouts of proxied connections.
const (
	dialTimeout = 30 * time.Second // connecting, reading request headers and awaiting responses
	idleTimeout = 5 * time.Minute  // a connection without traffic in either direction is closed
)

// reasonUpstream records a proxy connection that failed beyond the bridge's checks.
const reasonUpstream = "upstream connection failed"

// Event is a connection result in proxy_status.
type Event struct {
	channel.EventHead
	ConnectionID uint64 `json:"connection_id"`
	channel.Outcome
	Count int `json:"count,omitempty"`
}

// StatusInput is the input of proxy_status.
type StatusInput = channel.PageInput

// Status is the output of proxy_status.
type Status struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
	Rules
	channel.Recent[Event]
}

// Gateway is the bridge side of the HTTP proxy: it checks destinations and connects on the client network.
type Gateway struct {
	rules     atomic.Pointer[ruleSet]
	history   channel.History[Event]
	peer      channel.Peer
	proxy     func(*http.Request) (*url.URL, error)
	tlsConfig *tls.Config
}

// NewGateway initializes proxy destination rules; runtime changes are session-local.
func NewGateway(allow, deny []string) (*Gateway, error) {
	if len(allow) == 0 {
		return nil, errors.New("--proxy requires --proxy-allow-host")
	}
	parsed, err := parseRules(allow, deny)
	if err != nil {
		return nil, err
	}
	parsed.revision = 1
	gateway := &Gateway{proxy: snapshotProxy(), tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	gateway.rules.Store(parsed)
	return gateway, nil
}

// Register enables the HTTP proxy on the bridge's channel options.
func (g *Gateway) Register(opt *channel.Options) {
	opt.Enable(channel.HTTPProxy)
	opt.Handle(channel.KindHTTP, g.open)
	opt.Handle(channel.KindConnect, g.open)
	opt.OnReport(g.report)
	opt.OnAttach(g.peer.Attach)
}
func (g *Gateway) allowed(host string, port int) bool {
	rules := g.rules.Load()
	return !rules.deny.Allow(host, port) && rules.allow.Allow(host, port)
}
func (g *Gateway) report(kind string, f channel.Frame) {
	if f.Forward != 0 {
		return
	}
	switch kind {
	case "limit":
		g.record(0, f.Host, f.Port, channel.Denied, channel.ReasonLimit)
	case "auth_failed":
		g.record(0, "", 0, channel.Failed, "authentication")
	}
}
func (g *Gateway) record(connID uint64, host string, port int, result, reason string) {
	// Reasons are fixed strings, never raw network errors containing credentials.
	if result == channel.Failed && reason == "authentication" && g.history.Merge(func(last *Event) bool {
		if last.Reason != reason {
			return false
		}
		last.Count++
		return true
	}) {
		return
	}
	g.history.Add(func(seq uint64) Event {
		return Event{EventHead: channel.NewEventHead(seq), ConnectionID: connID, Outcome: channel.Outcome{Host: host, Port: port, Result: result, Reason: reason}, Count: 1}
	})
	log.Printf("proxy: connection=%d host=%q port=%d result=%s reason=%s", connID, host, port, result, reason)
}

// Status returns the proxy state, destination rules and recent connection results.
func (g *Gateway) Status(in StatusInput) (Status, error) {
	state, recent, err := channel.Snapshot(&g.history, &g.peer, in)
	if err != nil {
		return Status{}, err
	}
	return Status{Enabled: true, State: state, Rules: g.rules.Load().snapshot(), Recent: recent}, nil
}
func (g *Gateway) open(ctx context.Context, in *channel.Incoming) {
	if !channel.ValidDestination(in.Host, in.Port) || !g.allowed(in.Host, in.Port) {
		g.record(in.ID, in.Host, in.Port, channel.Denied, channel.ReasonNotAllowed)
		in.Reject(channel.CodeNotAllowed)
		return
	}
	g.record(in.ID, in.Host, in.Port, channel.Allowed, "destination allowed")
	if in.Kind == channel.KindHTTP {
		conn, err := in.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		g.serveHTTP(ctx, conn, in)
		return
	}
	in.Answer(ctx, channel.Answer{
		Timeout: dialTimeout,
		Dial: func(ctx context.Context) (net.Conn, error) {
			return g.connect(ctx, in.Host, in.Port)
		},
		Failed: func(error) string {
			g.record(in.ID, in.Host, in.Port, channel.Failed, reasonUpstream)
			return channel.CodeFailed
		},
		Hold: func(external net.Conn) (func(), string) {
			stop := context.AfterFunc(ctx, func() { external.Close() })
			return func() { stop() }, ""
		},
		Relay: tunnel,
	})
}
func (g *Gateway) serveHTTP(ctx context.Context, c net.Conn, f *channel.Incoming) {
	tr := &http.Transport{Proxy: g.proxy, DisableKeepAlives: true, TLSClientConfig: g.tlsConfig.Clone(), ResponseHeaderTimeout: dialTimeout}
	defer tr.CloseIdleConnections()
	headerLimit := &io.LimitedReader{R: c, N: 32*1024 + 1}
	br := bufio.NewReader(headerLimit)
	for {
		headerLimit.N = 32*1024 + 1
		c.SetReadDeadline(time.Now().Add(dialTimeout))
		req, err := http.ReadRequest(br)
		if err != nil || headerLimit.N == 0 {
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
		resp, err := tr.RoundTrip(req)
		if err != nil {
			g.record(f.ID, f.Host, f.Port, channel.Failed, "HTTP upstream failed")
			io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			return
		}
		stripHop(resp.Header)
		err = resp.Write(&idleWriter{c})
		resp.Body.Close()
		if err != nil || req.Close || resp.Close {
			return
		}
	}
}
func (g *Gateway) connect(ctx context.Context, host string, port int) (net.Conn, error) {
	target := net.JoinHostPort(host, strconv.Itoa(port))
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: target}}
	pu, err := g.proxy(req)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: dialTimeout}
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
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	c.SetDeadline(time.Now().Add(dialTimeout))
	if pu.Scheme == "https" {
		conf := g.tlsConfig.Clone()
		conf.ServerName = pu.Hostname()
		tc := tls.Client(c, conf)
		if err = tc.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		c = tc
	}
	r := &http.Request{Method: "CONNECT", URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if pu.User != nil {
		pass, _ := pu.User.Password()
		r.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(pu.User.Username()+":"+pass)))
	}
	if err = r.Write(c); err != nil {
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		return nil, err
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
	channel.CloseWrite(c.Conn)
	return nil
}

// tunnel relays a proxied connection. Traffic in either direction keeps both halves open for
// idleTimeout, and once one direction ends the other has 5 seconds to finish.
func tunnel(a, b net.Conn) {
	channel.Relay(a, b, channel.RelayOptions{Idle: idleTimeout, Linger: 5 * time.Second})
}

type idleWriter struct{ net.Conn }

func (w *idleWriter) Write(b []byte) (int, error) {
	w.SetWriteDeadline(time.Now().Add(idleTimeout))
	return w.Conn.Write(b)
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
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
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
	b.c.SetReadDeadline(time.Now().Add(idleTimeout))
	return b.ReadCloser.Read(p)
}
