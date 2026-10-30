package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

func registerNet(s *mcp.Server, d *Deps) {
	add(s, d, groupNet, &mcp.Tool{
		Name:  "net_http",
		Title: "HTTP request",
		Description: "Send an HTTP(S) request from the remote host and return status, headers and body. Reaches services that are only accessible from that host " +
			"(localhost:8080, internal APIs, health checks, metrics) without port forwarding. Any method, headers and body (text, base64 or a remote file); " +
			"unix_socket talks to Docker and other Unix-socket APIs. save_to writes the body to a remote file for downloads of any size. " +
			"insecure skips TLS verification; client_cert/client_key/ca_cert use files on the remote host.",
		Annotations: network(false),
	}, d.netHTTP)
	add(s, d, groupNet, &mcp.Tool{
		Name:  "net_tcp",
		Title: "TCP exchange",
		Description: "Open a TCP (optionally TLS) or Unix socket connection from the remote host, send data and return what comes back. " +
			"For non-HTTP protocols: Redis PING, SMTP/FTP banners, memcached stats, raw protocol checks. " +
			"Reading stops at until (regex), after idle_ms of silence, when the peer closes, or at max_bytes.",
		Annotations: network(false),
	}, d.netTCP)
	add(s, d, groupNet, &mcp.Tool{
		Name:  "net_check",
		Title: "Check reachability",
		Description: "Check from the remote host whether hosts resolve and ports accept connections, with timings; tls adds the certificate (subject, issuer, expiry, verification). " +
			"Targets are host:port, host (DNS only) or URLs. Checks run in parallel. Use it to diagnose firewalls, DNS and certificates.",
		Annotations: network(true),
	}, d.netCheck)
	add(s, d, groupNet, &mcp.Tool{
		Name:        "net_listen",
		Title:       "Listening ports",
		Description: "List TCP and UDP ports listening on the remote host with the owning process (pid and name; other users' processes may need root to be shown). Filter by port or process.",
		Annotations: readOnly(),
	}, d.netListen)
	add(s, d, groupNet, &mcp.Tool{
		Name:        "net_interfaces",
		Title:       "Network interfaces",
		Description: "List network interfaces of the remote host with IP addresses, MAC address, MTU and flags.",
		Annotations: readOnly(),
	}, d.netInterfaces)
}

// ---- net_http ----

// NetHTTPInput は net_http の入力です。
type NetHTTPInput struct {
	URL              string            `json:"url" jsonschema:"request URL, e.g. http://localhost:8080/health. With unix_socket the host part is ignored (use http://localhost/path)"`
	Method           string            `json:"method,omitempty" jsonschema:"HTTP method (default GET)"`
	Headers          map[string]string `json:"headers,omitempty" jsonschema:"request headers, e.g. {\"Authorization\": \"Bearer ...\", \"Content-Type\": \"application/json\"}"`
	Body             string            `json:"body,omitempty" jsonschema:"request body"`
	BodyEncoding     string            `json:"body_encoding,omitempty" jsonschema:"text (default, sent as UTF-8) or base64 for binary bodies"`
	BodyFile         string            `json:"body_file,omitempty" jsonschema:"send the contents of this remote file as the body (instead of body)"`
	UnixSocket       string            `json:"unix_socket,omitempty" jsonschema:"connect to this Unix domain socket instead of the URL host, e.g. /var/run/docker.sock"`
	TimeoutSec       int               `json:"timeout_sec,omitempty" jsonschema:"overall timeout in seconds (default: server default command timeout)"`
	NoRedirects      bool              `json:"no_redirects,omitempty" jsonschema:"return 3xx responses instead of following redirects"`
	Insecure         bool              `json:"insecure,omitempty" jsonschema:"skip TLS certificate verification (self-signed certificates)"`
	CACert           string            `json:"ca_cert,omitempty" jsonschema:"remote path of a PEM file with CA certificates to trust in addition to the system ones"`
	ClientCert       string            `json:"client_cert,omitempty" jsonschema:"remote path of a PEM client certificate for mutual TLS (with client_key)"`
	ClientKey        string            `json:"client_key,omitempty" jsonschema:"remote path of the PEM private key for client_cert"`
	Proxy            string            `json:"proxy,omitempty" jsonschema:"proxy URL, or none to connect directly. Default: the remote host's HTTP_PROXY/HTTPS_PROXY/NO_PROXY environment"`
	ResponseEncoding string            `json:"response_encoding,omitempty" jsonschema:"auto (default: text for textual content types and valid UTF-8, otherwise base64), text or base64"`
	MaxBytes         int               `json:"max_bytes,omitempty" jsonschema:"maximum body bytes to return (default and cap: the server output limit). Ignored with save_to"`
	SaveTo           string            `json:"save_to,omitempty" jsonschema:"write the response body to this remote file (any size) instead of returning it"`
}

// AuditArgs は本文と認証のヘッダーをログに残さないようにします。
func (in NetHTTPInput) AuditArgs() any {
	in.Body = redactedLen(in.Body)
	h := map[string]string{}
	for k, v := range in.Headers {
		switch strings.ToLower(k) {
		case "authorization", "proxy-authorization", "cookie", "x-api-key":
			v = redactedLen(v)
		}
		h[k] = v
	}
	in.Headers = h
	return in
}

// PathArgs は読み書きする接続先のファイルを方針の検査に渡します。
func (in NetHTTPInput) PathArgs() (read, write []string) {
	for _, p := range []string{in.BodyFile, in.CACert, in.ClientCert, in.ClientKey} {
		if p != "" {
			read = append(read, p)
		}
	}
	if in.SaveTo != "" {
		write = []string{in.SaveTo}
	}
	return read, write
}

// NetHTTPOutput は net_http の出力です。
type NetHTTPOutput struct {
	Status        int               `json:"status"`
	StatusText    string            `json:"status_text"`
	Proto         string            `json:"proto"`
	Headers       map[string]string `json:"headers" jsonschema:"response headers (multiple values joined with , )"`
	Body          string            `json:"body,omitempty"`
	BodyEncoding  string            `json:"body_encoding,omitempty" jsonschema:"text or base64"`
	BodyCharset   string            `json:"body_charset,omitempty" jsonschema:"set when the text body was converted to UTF-8 from this charset"`
	BodyBytes     int64             `json:"body_bytes" jsonschema:"bytes of body received"`
	BodyTruncated bool              `json:"body_truncated,omitempty" jsonschema:"only the first max_bytes were returned"`
	SavedTo       string            `json:"saved_to,omitempty"`
	FinalURL      string            `json:"final_url,omitempty" jsonschema:"set when redirects were followed"`
	Redirects     []string          `json:"redirects,omitempty"`
	TLS           *TLSInfo          `json:"tls,omitempty"`
	DurationMs    int64             `json:"duration_ms"`
}

// TLSInfo は TLS の接続と相手の証明書の情報です。
type TLSInfo struct {
	Version     string   `json:"version"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	DNSNames    []string `json:"dns_names,omitempty"`
	NotBefore   string   `json:"not_before,omitempty"`
	NotAfter    string   `json:"not_after,omitempty"`
	DaysLeft    int      `json:"days_left,omitempty"`
	Verified    bool     `json:"verified"`
	VerifyError string   `json:"verify_error,omitempty"`
}

func (d *Deps) netHTTP(ctx context.Context, req *mcp.CallToolRequest, in NetHTTPInput) (*mcp.CallToolResult, NetHTTPOutput, error) {
	method := strings.ToUpper(orDefault(in.Method, http.MethodGet))
	if d.Policy.ReadOnly && (!slices.Contains([]string{"GET", "HEAD", "OPTIONS"}, method) || in.SaveTo != "") {
		return nil, NetHTTPOutput{}, errors.New("policy: read-only mode allows only GET, HEAD and OPTIONS without save_to")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Host == "" && in.UnixSocket == "") {
		return nil, NetHTTPOutput{}, fmt.Errorf("url must be an absolute http or https URL: %q", in.URL)
	}
	if err := d.checkURL(u, in.UnixSocket); err != nil {
		return nil, NetHTTPOutput{}, err
	}

	tlsConf, err := clientTLS(in.Insecure, in.CACert, in.ClientCert, in.ClientKey)
	if err != nil {
		return nil, NetHTTPOutput{}, err
	}
	tr := &http.Transport{TLSClientConfig: tlsConf, Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
	switch in.Proxy {
	case "":
	case "none":
		tr.Proxy = nil
	default:
		pu, err := url.Parse(in.Proxy)
		if err != nil {
			return nil, NetHTTPOutput{}, fmt.Errorf("proxy: %w", err)
		}
		tr.Proxy = http.ProxyURL(pu)
	}
	if in.UnixSocket != "" {
		tr.Proxy = nil
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dl net.Dialer
			return dl.DialContext(ctx, "unix", in.UnixSocket)
		}
	}
	defer tr.CloseIdleConnections()

	var out NetHTTPOutput
	client := &http.Client{Transport: tr, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if in.NoRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if err := d.checkURL(r.URL, in.UnixSocket); err != nil {
			return err
		}
		out.Redirects = append(out.Redirects, r.URL.String())
		return nil
	}}

	timeout := d.Cfg.ClampTimeout(in.TimeoutSec)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := requestBody(in)
	if err != nil {
		return nil, NetHTTPOutput{}, err
	}
	if c, ok := body.(io.Closer); ok {
		defer c.Close()
	}
	hreq, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, NetHTTPOutput{}, err
	}
	for k, v := range in.Headers {
		if strings.EqualFold(k, "Host") {
			hreq.Host = v
			continue
		}
		hreq.Header.Set(k, v)
	}
	if f, ok := body.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			hreq.ContentLength = st.Size()
		}
	}

	start := time.Now()
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, NetHTTPOutput{}, err
	}
	defer resp.Body.Close()
	out.Status, out.StatusText, out.Proto = resp.StatusCode, http.StatusText(resp.StatusCode), resp.Proto
	out.Headers = map[string]string{}
	for k, v := range resp.Header {
		out.Headers[k] = strings.Join(v, ", ")
	}
	if len(out.Redirects) > 0 {
		out.FinalURL = resp.Request.URL.String()
	}
	if resp.TLS != nil {
		out.TLS = tlsInfo(resp.TLS, resp.Request.URL.Hostname(), tlsConf)
	}

	pr := newProgress(ctx, req)
	if in.SaveTo != "" {
		err = d.saveBody(resp, in.SaveTo, pr, &out)
	} else {
		err = d.readBody(resp, in, &out)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		return nil, NetHTTPOutput{}, err
	}
	return nil, out, nil
}

// checkURL は URL の宛先が方針で許されるかを調べます。
func (d *Deps) checkURL(u *url.URL, unixSocket string) error {
	if unixSocket != "" {
		return d.Policy.CheckUnixSocket(unixSocket)
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	n, _ := strconv.Atoi(port)
	return d.Policy.CheckHost(u.Hostname(), n)
}

func requestBody(in NetHTTPInput) (io.Reader, error) {
	switch {
	case in.BodyFile != "" && in.Body != "":
		return nil, errors.New("body and body_file are mutually exclusive")
	case in.BodyFile != "":
		return os.Open(in.BodyFile)
	case in.Body == "":
		return nil, nil
	}
	switch orDefault(in.BodyEncoding, "text") {
	case "text":
		return strings.NewReader(in.Body), nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(in.Body)
		if err != nil {
			return nil, fmt.Errorf("body: %w", err)
		}
		return bytes.NewReader(b), nil
	default:
		return nil, fmt.Errorf("unknown body_encoding %q", in.BodyEncoding)
	}
}

func clientTLS(insecure bool, caFile, certFile, keyFile string) (*tls.Config, error) {
	c := &tls.Config{InsecureSkipVerify: insecure}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("ca_cert: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_cert: no certificates found in %s", caFile)
		}
		c.RootCAs = pool
	}
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("client_cert and client_key must be given together")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		c.Certificates = []tls.Certificate{cert}
	}
	return c, nil
}

// tlsInfo は TLS の接続の情報をまとめます。検証を省いた接続でも、検証した場合の結果を示します。
func tlsInfo(cs *tls.ConnectionState, host string, conf *tls.Config) *TLSInfo {
	info := &TLSInfo{Version: tls.VersionName(cs.Version)}
	if len(cs.PeerCertificates) == 0 {
		return info
	}
	leaf := cs.PeerCertificates[0]
	info.Subject = leaf.Subject.String()
	info.Issuer = leaf.Issuer.String()
	info.DNSNames = leaf.DNSNames
	info.NotBefore = leaf.NotBefore.Format(time.RFC3339)
	info.NotAfter = leaf.NotAfter.Format(time.RFC3339)
	info.DaysLeft = int(time.Until(leaf.NotAfter).Hours() / 24)
	opts := x509.VerifyOptions{DNSName: host, Intermediates: x509.NewCertPool()}
	if conf != nil {
		opts.Roots = conf.RootCAs
	}
	for _, c := range cs.PeerCertificates[1:] {
		opts.Intermediates.AddCert(c)
	}
	if _, err := leaf.Verify(opts); err != nil {
		info.VerifyError = err.Error()
	} else {
		info.Verified = true
	}
	return info
}

func (d *Deps) readBody(resp *http.Response, in NetHTTPInput, out *NetHTTPOutput) error {
	max := d.Cfg.MaxOutputBytes
	enc := orDefault(in.ResponseEncoding, "auto")
	if enc == "base64" || enc == "auto" {
		max = d.Cfg.MaxOutputBytes / 4 * 3
	}
	if in.MaxBytes > 0 && in.MaxBytes < max {
		max = in.MaxBytes
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, int64(max)))
	if err != nil {
		return err
	}
	// 残りは数えるだけにする。
	rest, _ := io.Copy(io.Discard, resp.Body)
	out.BodyBytes = int64(len(buf)) + rest
	out.BodyTruncated = rest > 0

	ct := resp.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(ct)
	charset := strings.ToLower(params["charset"])
	if enc == "auto" {
		enc = "base64"
		if textual(mediaType) || (mediaType == "" && utf8.Valid(buf[:len(buf)-textenc.IncompleteTail(buf)])) {
			enc = "text"
		}
		if charset != "" && charset != "utf-8" && charset != "us-ascii" {
			enc = "text"
		}
	}
	switch enc {
	case "text":
		out.BodyEncoding = "text"
		if out.BodyTruncated {
			buf = buf[:len(buf)-textenc.IncompleteTail(buf)]
		}
		if charset != "" && charset != "utf-8" && charset != "us-ascii" {
			if e, err := textenc.Lookup(charset); err == nil && !e.IsUTF8() {
				out.Body = e.Decode(buf)
				out.BodyCharset = e.Name
				return nil
			}
		}
		out.Body = strings.ToValidUTF8(string(buf), "�")
	case "base64":
		out.BodyEncoding = "base64"
		out.Body = base64.StdEncoding.EncodeToString(buf)
	default:
		return fmt.Errorf("unknown response_encoding %q", in.ResponseEncoding)
	}
	return nil
}

// textual は本文をテキストとして返すべき種類かを返します。
func textual(mediaType string) bool {
	switch {
	case strings.HasPrefix(mediaType, "text/"):
		return true
	case strings.HasSuffix(mediaType, "+json"), strings.HasSuffix(mediaType, "+xml"):
		return true
	}
	return slices.Contains([]string{"application/json", "application/xml", "application/javascript",
		"application/x-www-form-urlencoded", "application/yaml", "application/x-yaml", "application/x-ndjson",
		"application/problem+json", "application/graphql"}, mediaType)
}

func (d *Deps) saveBody(resp *http.Response, path string, pr *progress, out *NetHTTPOutput) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	total := float64(max(resp.ContentLength, 0))
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := f.Write(buf[:k]); err != nil {
				f.Close()
				return err
			}
			n += int64(k)
			pr.report(float64(n), total, fmt.Sprintf("%d bytes downloaded", n), false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	out.BodyBytes = n
	out.SavedTo = absPath(path)
	return nil
}

// ---- net_tcp ----

// NetTCPInput は net_tcp の入力です。
type NetTCPInput struct {
	Host            string `json:"host,omitempty" jsonschema:"host name or IP address (default localhost)"`
	Port            int    `json:"port,omitempty" jsonschema:"TCP port (required unless unix_socket)"`
	UnixSocket      string `json:"unix_socket,omitempty" jsonschema:"connect to this Unix domain socket instead of host:port"`
	TLS             bool   `json:"tls,omitempty" jsonschema:"use TLS"`
	Insecure        bool   `json:"insecure,omitempty" jsonschema:"with tls: skip certificate verification"`
	Send            string `json:"send,omitempty" jsonschema:"data to send after connecting, e.g. \"PING\\r\\n\" (escape sequences are JSON escapes)"`
	SendEncoding    string `json:"send_encoding,omitempty" jsonschema:"text (default) or base64"`
	CloseWrite      bool   `json:"close_write,omitempty" jsonschema:"half-close the connection after sending (tells the peer no more data comes)"`
	Until           string `json:"until,omitempty" jsonschema:"stop reading once the received text matches this regular expression"`
	IdleMs          int    `json:"idle_ms,omitempty" jsonschema:"stop reading after this many milliseconds without data (default 1000)"`
	TimeoutSec      int    `json:"timeout_sec,omitempty" jsonschema:"overall timeout in seconds (default 10)"`
	MaxBytes        int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes to receive (default and cap: the server output limit)"`
	ReceiveEncoding string `json:"receive_encoding,omitempty" jsonschema:"auto (default: text if valid UTF-8, otherwise base64), text or base64"`
}

// AuditArgs は送る内容をログに残さないようにします。
func (in NetTCPInput) AuditArgs() any {
	in.Send = redactedLen(in.Send)
	return in
}

// NetTCPOutput は net_tcp の出力です。
type NetTCPOutput struct {
	RemoteAddr      string   `json:"remote_addr"`
	Sent            int      `json:"sent_bytes"`
	Received        string   `json:"received"`
	ReceiveEncoding string   `json:"receive_encoding"`
	ReceivedBytes   int      `json:"received_bytes"`
	Matched         bool     `json:"matched,omitempty" jsonschema:"until matched"`
	ClosedByPeer    bool     `json:"closed_by_peer,omitempty"`
	Truncated       bool     `json:"truncated,omitempty" jsonschema:"stopped at max_bytes"`
	TLS             *TLSInfo `json:"tls,omitempty"`
	DurationMs      int64    `json:"duration_ms"`
}

func (d *Deps) netTCP(ctx context.Context, _ *mcp.CallToolRequest, in NetTCPInput) (*mcp.CallToolResult, NetTCPOutput, error) {
	if d.Policy.ReadOnly && in.Send != "" {
		return nil, NetTCPOutput{}, errors.New("policy: read-only mode does not allow sending data (only reading what the server sends first)")
	}
	var until *regexp.Regexp
	if in.Until != "" {
		var err error
		if until, err = regexp.Compile(in.Until); err != nil {
			return nil, NetTCPOutput{}, fmt.Errorf("until: %w", err)
		}
	}
	send := []byte(in.Send)
	switch orDefault(in.SendEncoding, "text") {
	case "text":
	case "base64":
		b, err := base64.StdEncoding.DecodeString(in.Send)
		if err != nil {
			return nil, NetTCPOutput{}, fmt.Errorf("send: %w", err)
		}
		send = b
	default:
		return nil, NetTCPOutput{}, fmt.Errorf("unknown send_encoding %q", in.SendEncoding)
	}

	timeout := time.Duration(orDefaultInt(in.TimeoutSec, 10)) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	network, addr, host := "tcp", "", orDefault(in.Host, "localhost")
	if in.UnixSocket != "" {
		network, addr = "unix", in.UnixSocket
		if err := d.Policy.CheckUnixSocket(addr); err != nil {
			return nil, NetTCPOutput{}, err
		}
	} else {
		if in.Port <= 0 || in.Port > 65535 {
			return nil, NetTCPOutput{}, errors.New("port is required (1-65535) unless unix_socket")
		}
		if err := d.Policy.CheckHost(host, in.Port); err != nil {
			return nil, NetTCPOutput{}, err
		}
		addr = net.JoinHostPort(host, strconv.Itoa(in.Port))
	}

	start := time.Now()
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, network, addr)
	if err != nil {
		return nil, NetTCPOutput{}, err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.SetDeadline(time.Now())
	}()
	out := NetTCPOutput{RemoteAddr: conn.RemoteAddr().String()}
	if in.TLS {
		conf := &tls.Config{InsecureSkipVerify: in.Insecure, ServerName: host}
		tc := tls.Client(conn, conf)
		if err := tc.HandshakeContext(ctx); err != nil {
			return nil, NetTCPOutput{}, fmt.Errorf("tls handshake: %w", err)
		}
		st := tc.ConnectionState()
		out.TLS = tlsInfo(&st, host, nil)
		conn = tc
	}
	if len(send) > 0 {
		n, err := conn.Write(send)
		out.Sent = n
		if err != nil {
			return nil, NetTCPOutput{}, fmt.Errorf("send: %w", err)
		}
	}
	if in.CloseWrite {
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
	}

	max := d.Cfg.MaxOutputBytes / 4 * 3
	if in.MaxBytes > 0 && in.MaxBytes < max {
		max = in.MaxBytes
	}
	idle := time.Duration(orDefaultInt(in.IdleMs, 1000)) * time.Millisecond
	var recv []byte
	buf := make([]byte, 32<<10)
	for len(recv) < max {
		conn.SetReadDeadline(time.Now().Add(idle))
		n, err := conn.Read(buf[:min(len(buf), max-len(recv))])
		recv = append(recv, buf[:n]...)
		if until != nil && until.Match(recv) {
			out.Matched = true
			break
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				out.ClosedByPeer = true
			}
			break // idle の時間切れ、または全体の時間切れ
		}
	}
	out.Truncated = len(recv) >= max
	out.ReceivedBytes = len(recv)
	enc := orDefault(in.ReceiveEncoding, "auto")
	if enc == "auto" {
		enc = "base64"
		if utf8.Valid(recv[:len(recv)-textenc.IncompleteTail(recv)]) {
			enc = "text"
		}
	}
	switch enc {
	case "text":
		out.Received = strings.ToValidUTF8(string(recv), "�")
	case "base64":
		out.Received = base64.StdEncoding.EncodeToString(recv)
	default:
		return nil, NetTCPOutput{}, fmt.Errorf("unknown receive_encoding %q", in.ReceiveEncoding)
	}
	out.ReceiveEncoding = enc
	out.DurationMs = time.Since(start).Milliseconds()
	return nil, out, nil
}

// ---- net_check ----

// NetCheckInput は net_check の入力です。
type NetCheckInput struct {
	Targets   []string `json:"targets" jsonschema:"what to check: host:port (DNS and TCP connect), host (DNS only), or a URL (its host and port; https implies tls). IPv6 as [::1]:443"`
	TLS       bool     `json:"tls,omitempty" jsonschema:"also do a TLS handshake on each port and report the certificate"`
	TimeoutMs int      `json:"timeout_ms,omitempty" jsonschema:"timeout per target in milliseconds (default 5000)"`
}

// NetCheckResult は 1 つの宛先の結果です。
type NetCheckResult struct {
	Target    string   `json:"target"`
	Host      string   `json:"host"`
	Port      int      `json:"port,omitempty"`
	Addresses []string `json:"addresses,omitempty" jsonschema:"resolved IP addresses"`
	DNSMs     int64    `json:"dns_ms"`
	Connected *bool    `json:"connected,omitempty" jsonschema:"TCP connection succeeded (absent for DNS-only targets)"`
	ConnectMs int64    `json:"connect_ms,omitempty"`
	TLS       *TLSInfo `json:"tls,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// NetCheckOutput は net_check の出力です。
type NetCheckOutput struct {
	Results []NetCheckResult `json:"results"`
}

func (d *Deps) netCheck(ctx context.Context, _ *mcp.CallToolRequest, in NetCheckInput) (*mcp.CallToolResult, NetCheckOutput, error) {
	if len(in.Targets) == 0 {
		return nil, NetCheckOutput{}, errors.New("targets is required")
	}
	if len(in.Targets) > 100 {
		return nil, NetCheckOutput{}, errors.New("at most 100 targets")
	}
	timeout := time.Duration(orDefaultInt(in.TimeoutMs, 5000)) * time.Millisecond
	out := NetCheckOutput{Results: make([]NetCheckResult, len(in.Targets))}
	done := make(chan struct{})
	for i, t := range in.Targets {
		go func() {
			out.Results[i] = d.checkTarget(ctx, t, in.TLS, timeout)
			done <- struct{}{}
		}()
	}
	for range in.Targets {
		<-done
	}
	return nil, out, nil
}

func (d *Deps) checkTarget(ctx context.Context, target string, useTLS bool, timeout time.Duration) NetCheckResult {
	r := NetCheckResult{Target: target}
	host, port, isTLS, err := parseTarget(target)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Host, r.Port = host, port
	useTLS = useTLS || isTLS
	if err := d.Policy.CheckHost(host, port); err != nil {
		r.Error = err.Error()
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	r.DNSMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Addresses = addrs
	if port == 0 {
		return r
	}
	start = time.Now()
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	r.ConnectMs = time.Since(start).Milliseconds()
	ok := err == nil
	r.Connected = &ok
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer conn.Close()
	if useTLS {
		tc := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: host})
		if err := tc.HandshakeContext(ctx); err != nil {
			r.Error = "tls handshake: " + err.Error()
			return r
		}
		st := tc.ConnectionState()
		r.TLS = tlsInfo(&st, host, nil)
	}
	return r
}

// parseTarget は net_check の宛先を、ホスト、ポート (なければ 0)、TLS を使うかに分けます。
func parseTarget(t string) (host string, port int, isTLS bool, err error) {
	if strings.Contains(t, "://") {
		u, err := url.Parse(t)
		if err != nil {
			return "", 0, false, err
		}
		host = u.Hostname()
		ps := u.Port()
		switch {
		case ps != "":
		case u.Scheme == "https":
			ps = "443"
		case u.Scheme == "http":
			ps = "80"
		default:
			if p, err := net.LookupPort("tcp", u.Scheme); err == nil {
				ps = strconv.Itoa(p)
			}
		}
		port, _ = strconv.Atoi(ps)
		return host, port, u.Scheme == "https", nil
	}
	if h, p, err := net.SplitHostPort(t); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil {
			if n, err = net.LookupPort("tcp", p); err != nil {
				return "", 0, false, fmt.Errorf("bad port %q", p)
			}
		}
		return h, n, false, nil
	}
	return strings.Trim(t, "[]"), 0, false, nil
}

// ---- net_listen ----

// NetListenInput は net_listen の入力です。
type NetListenInput struct {
	Port     int    `json:"port,omitempty" jsonschema:"only this port"`
	Protocol string `json:"protocol,omitempty" jsonschema:"tcp or udp (default both)"`
	Process  string `json:"process,omitempty" jsonschema:"only sockets of processes whose name contains this (case-insensitive)"`
}

// Listener は待ち受け中のソケットです。
type Listener struct {
	Protocol string `json:"protocol" jsonschema:"tcp, tcp6, udp or udp6"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	PID      int    `json:"pid,omitempty"`
	Process  string `json:"process,omitempty"`
}

// NetListenOutput は net_listen の出力です。
type NetListenOutput struct {
	Listeners []Listener `json:"listeners"`
	Note      string     `json:"note,omitempty"`
}

func (d *Deps) netListen(ctx context.Context, _ *mcp.CallToolRequest, in NetListenInput) (*mcp.CallToolResult, NetListenOutput, error) {
	all, note, err := listeners(ctx)
	if err != nil {
		return nil, NetListenOutput{}, err
	}
	out := NetListenOutput{Listeners: []Listener{}, Note: note}
	for _, l := range all {
		switch {
		case in.Port > 0 && l.Port != in.Port:
		case in.Protocol != "" && !strings.HasPrefix(l.Protocol, strings.ToLower(in.Protocol)):
		case in.Process != "" && !strings.Contains(strings.ToLower(l.Process), strings.ToLower(in.Process)):
		default:
			out.Listeners = append(out.Listeners, l)
		}
	}
	slices.SortFunc(out.Listeners, func(a, b Listener) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Protocol+a.Address, b.Protocol+b.Address)
	})
	return nil, out, nil
}

// ---- net_interfaces ----

// Interface はネットワーク インターフェースです。
type Interface struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MTU       int      `json:"mtu"`
	MAC       string   `json:"mac,omitempty"`
	Flags     string   `json:"flags"`
	Addresses []string `json:"addresses" jsonschema:"IP addresses with prefix length"`
}

// NetInterfacesOutput は net_interfaces の出力です。
type NetInterfacesOutput struct {
	Interfaces []Interface `json:"interfaces"`
}

func (d *Deps) netInterfaces(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, NetInterfacesOutput, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, NetInterfacesOutput{}, err
	}
	out := NetInterfacesOutput{Interfaces: []Interface{}}
	for _, i := range ifs {
		it := Interface{Name: i.Name, Index: i.Index, MTU: i.MTU, MAC: i.HardwareAddr.String(), Flags: i.Flags.String(), Addresses: []string{}}
		if addrs, err := i.Addrs(); err == nil {
			for _, a := range addrs {
				it.Addresses = append(it.Addresses, a.String())
			}
		}
		out.Interfaces = append(out.Interfaces, it)
	}
	return nil, out, nil
}
