package clientproxy

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

type Target struct {
	URL         string
	File        string
	token, auth string
	listener    net.Listener
	server      *http.Server
	mux         *channel.Conn
	dir         string
	once        sync.Once
}

func NewTarget() (*Target, error) {
	secret := make([]byte, 32)
	if _, e := rand.Read(secret); e != nil {
		return nil, e
	}
	t := &Target{token: hex.EncodeToString(secret)}
	t.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte("host-shell:"+t.token))
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	t.listener = &limitedListener{Listener: l, reject: func() {
		if t.mux != nil {
			t.mux.Report("limit", channel.Frame{})
		}
	}}
	t.URL = "http://host-shell:" + t.token + "@" + l.Addr().String()
	t.dir, e = os.MkdirTemp("", "host-shell-proxy-")
	if e != nil {
		l.Close()
		return nil, e
	}
	if e = secureDir(t.dir); e != nil {
		t.Close()
		return nil, e
	}
	t.File = filepath.Join(t.dir, "proxy-url")
	if e = os.WriteFile(t.File, []byte(t.URL+"\n"), 0600); e != nil {
		t.Close()
		return nil, e
	}
	return t, nil
}

// Register permits the HTTP proxy on the target's channel options.
func (t *Target) Register(o *channel.Options) {
	o.Enable(channel.HTTPProxy)
	o.OnAttach(t.attach)
}
func (t *Target) attach(c *channel.Conn) {
	t.mux = c
	t.server = &http.Server{Handler: t, ReadHeaderTimeout: 30 * time.Second, IdleTimeout: 5 * time.Minute, MaxHeaderBytes: 32 * 1024}
	go func() { t.server.Serve(t.listener) }()
	go func() { <-c.Done(); t.Close() }()
}
func (t *Target) Close() {
	t.once.Do(func() {
		if t.listener != nil {
			t.listener.Close()
		}
		if t.server != nil {
			t.server.Close()
		}
		if t.dir != "" {
			os.RemoveAll(t.dir)
		}
	})
}
func (t *Target) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Proxy-Authorization")), []byte(t.auth)) != 1 {
		// Authentication failures do not reveal the requested host or credentials.
		t.mux.Report("auth_failed", channel.Frame{})
		w.Header().Set("Proxy-Authenticate", `Basic realm="host-shell"`)
		http.Error(w, "proxy authentication required", 407)
		return
	}
	host := r.URL.Host
	port := 80
	if r.Method == "CONNECT" {
		host = r.Host
		port = 443
	}
	h, p, e := net.SplitHostPort(host)
	if e == nil {
		host = h
		port, e = strconv.Atoi(p)
		if e != nil {
			http.Error(w, "invalid port", 400)
			return
		}
	} else if strings.Contains(host, ":") {
		http.Error(w, "invalid destination", 400)
		return
	}
	if !validDestination(host, port) {
		http.Error(w, "invalid destination", 400)
		return
	}
	if r.Method != "CONNECT" && (r.URL.Scheme != "http" || !strings.EqualFold(r.Host, r.URL.Host)) {
		http.Error(w, "invalid HTTP proxy destination", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	kind := channel.KindHTTP
	if r.Method == "CONNECT" {
		kind = channel.KindConnect
	}
	c, e := t.mux.Dial(ctx, kind, host, port, 0)
	cancel()
	if e != nil {
		code := 502
		switch channel.Code(e) {
		case channel.CodeNotAllowed:
			code = 403
		case channel.CodeLimit:
			if e == channel.ErrLimit {
				// A limit reached on the bridge is already recorded there.
				t.mux.Report("limit", channel.Frame{Host: host, Port: port})
			}
			code = 503
		case channel.CodeTimeout:
			code = 504
		}
		http.Error(w, http.StatusText(code), code)
		return
	}
	defer c.Close()
	if r.Method == "CONNECT" {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking unavailable", 500)
			return
		}
		local, buf, e := hj.Hijack()
		if e != nil {
			return
		}
		defer local.Close()
		if _, e = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); e != nil {
			return
		}
		if buf.Flush() != nil {
			return
		}
		tunnel(&bufferedConn{Conn: local, r: buf.Reader}, c)
		return
	}
	rc := http.NewResponseController(w)
	r.Body = &requestBody{ReadCloser: r.Body, rc: rc}
	stripHop(r.Header)
	r.RequestURI = ""
	r.URL = &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(context.Context, string, string) (net.Conn, error) { return c, nil }}
	defer tr.CloseIdleConnections()
	resp, e := tr.RoundTrip(r)
	if e != nil {
		http.Error(w, "proxy upstream failed", 502)
		return
	}
	defer resp.Body.Close()
	stripHop(resp.Header)
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(&responseWriter{ResponseWriter: w, rc: rc}, &idleBody{ReadCloser: resp.Body, c: c})
}

func (t *Target) Secret() string { return t.token }

// Mask removes this session's credentials even when optional output redaction is off.
func (t *Target) Mask(s string) string {
	for _, v := range []string{base64.StdEncoding.EncodeToString([]byte(t.URL + "\n")), base64.StdEncoding.EncodeToString([]byte(t.URL)), t.URL, t.auth, strings.TrimPrefix(t.auth, "Basic "), t.token} {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}

// Environment overlays inherited proxy variables; callers apply their policy filter afterwards.
func (t *Target) Environment(base []string) []string {
	out := make([]string, 0, len(base)+4)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "http_proxy") || strings.EqualFold(k, "https_proxy") {
			continue
		}
		out = append(out, kv)
	}
	keys := []string{"HTTP_PROXY", "HTTPS_PROXY"}
	if runtime.GOOS != "windows" {
		keys = append(keys, "http_proxy", "https_proxy")
	}
	for _, k := range keys {
		out = append(out, k+"="+t.URL)
	}
	return out
}

// Proxy uses the per-session route without the standard library's global environment cache.
func (t *Target) Proxy(r *http.Request) (*url.URL, error) {
	if bypass(r.URL, firstEnv("NO_PROXY", "no_proxy")) {
		return nil, nil
	}
	return url.Parse(t.URL)
}
func firstEnv(a, b string) string {
	if v := os.Getenv(a); v != "" {
		return v
	}
	return os.Getenv(b)
}
func bypass(u *url.URL, list string) bool {
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback() {
		return true
	}
	for _, r := range strings.Split(list, ",") {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "*" {
			return true
		}
		if r == "" {
			continue
		}
		if _, n, e := net.ParseCIDR(r); e == nil {
			if n.Contains(net.ParseIP(h)) {
				return true
			}
			continue
		}
		rh, rp := policy.SplitNameRule(r)
		port := u.Port()
		if port == "" {
			port = "80"
			if u.Scheme == "https" {
				port = "443"
			}
		}
		if rp != "" && rp != port {
			continue
		}
		subOnly := strings.HasPrefix(rh, "*.") || strings.HasPrefix(rh, ".")
		rh = strings.TrimPrefix(strings.TrimPrefix(rh, "*."), ".")
		if (!subOnly && h == rh) || strings.HasSuffix(h, "."+rh) {
			return true
		}
	}
	return false
}

type requestBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b *requestBody) Read(p []byte) (int, error) {
	b.rc.SetReadDeadline(time.Now().Add(5 * time.Minute))
	return b.ReadCloser.Read(p)
}

type responseWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))
	n, e := w.ResponseWriter.Write(p)
	if e == nil {
		e = w.rc.Flush()
	}
	return n, e
}
