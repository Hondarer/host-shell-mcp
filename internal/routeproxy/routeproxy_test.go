package routeproxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// proxyRequest is a CONNECT request as the test proxy received it.
type proxyRequest struct {
	line    string
	headers map[string]string
}

// testProxy is a minimal CONNECT proxy. handle receives each parsed request with the connection
// positioned after the request headers.
type testProxy struct {
	host  string
	port  int
	conns atomic.Int32
	mu    sync.Mutex
	reqs  []proxyRequest
}

func startProxy(t *testing.T, handle func(req proxyRequest, br *bufio.Reader, c net.Conn)) *testProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	p := &testProxy{host: "127.0.0.1", port: ln.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			p.conns.Add(1)
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				req := proxyRequest{line: strings.TrimRight(line, "\r\n"), headers: map[string]string{}}
				for {
					h, err := br.ReadString('\n')
					if err != nil {
						return
					}
					h = strings.TrimRight(h, "\r\n")
					if h == "" {
						break
					}
					k, v, _ := strings.Cut(h, ":")
					req.headers[strings.ToLower(k)] = strings.TrimSpace(v)
				}
				p.mu.Lock()
				p.reqs = append(p.reqs, req)
				p.mu.Unlock()
				handle(req, br, c)
			}()
		}
	}()
	return p
}

// writePasswordFile writes a password file that passes the permission check on every OS: the
// test's temporary directory may inherit ACEs that let other trustees write (seen on Windows).
func writePasswordFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, path)
}

// firstRequest returns the first request the proxy received, failing with runErr when none arrived.
func (p *testProxy) firstRequest(t *testing.T, runErr error) proxyRequest {
	t.Helper()
	reqs := p.requests()
	if len(reqs) == 0 {
		t.Fatalf("the proxy received no request (run: %v)", runErr)
	}
	return reqs[0]
}

func (p *testProxy) requests() []proxyRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]proxyRequest(nil), p.reqs...)
}

// reply returns a handler that answers every request with the given status line and headers.
func reply(status string, headers ...string) func(proxyRequest, *bufio.Reader, net.Conn) {
	return func(_ proxyRequest, _ *bufio.Reader, c net.Conn) {
		fmt.Fprintf(c, "HTTP/1.1 %s\r\n%s\r\n", status, strings.Join(append(headers, ""), "\r\n"))
	}
}

func testEnv(vars map[string]string) environment {
	return environment{
		lookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
		prompt:    func(string) (string, bool, error) { return "", false, nil },
	}
}

func testConfig(p *testProxy, user string) config {
	return config{proxyHost: p.host, proxyPort: p.port, user: user, destHost: "example.test", destPort: 22, timeout: 5 * time.Second}
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// TestRelay checks the request, preemptive Basic authentication, the bytes that follow the
// response headers, an 8-bit clean round trip of every byte value, and the half-close that
// lets the proxy see the end of standard input.
func TestRelay(t *testing.T) {
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	const early = "SSH-2.0-early\r\n"
	p := startProxy(t, func(_ proxyRequest, br *bufio.Reader, c net.Conn) {
		fmt.Fprintf(c, "HTTP/1.1 200 Connection established\r\nX-Test: 1\r\n\r\n%s", early)
		io.Copy(c, br) // echo until the client half-closes
	})
	var stdout, stderr bytes.Buffer
	cfg := testConfig(p, "alice")
	cfg.debug = 2
	err := run(cfg, testEnv(map[string]string{"HTTP_PROXY_PASSWORD": "s3cret"}), bytes.NewReader(payload), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	if got := stdout.Bytes(); !bytes.Equal(got, append([]byte(early), payload...)) {
		t.Fatalf("relayed %d bytes, want %d", len(got), len(early)+len(payload))
	}
	reqs := p.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.line != "CONNECT example.test:22 HTTP/1.1" || r.headers["host"] != "example.test:22" ||
		r.headers["proxy-authorization"] != basic("alice", "s3cret") || !strings.HasPrefix(r.headers["user-agent"], "host-shell/") {
		t.Fatalf("request %+v", r)
	}
	if s := stderr.String(); strings.Contains(s, "s3cret") || strings.Contains(s, basic("alice", "s3cret")[6:]) {
		t.Fatalf("diagnostics leak the credentials:\n%s", s)
	}
	if !strings.Contains(stderr.String(), "response header: X-Test: 1") {
		t.Fatalf("-dd did not print the response headers:\n%s", stderr.String())
	}
}

func TestNoAuthWithoutUser(t *testing.T) {
	p := startProxy(t, func(_ proxyRequest, br *bufio.Reader, c net.Conn) {
		fmt.Fprint(c, "HTTP/1.0 200 OK\r\n\r\n")
		io.Copy(c, br)
	})
	var stdout bytes.Buffer
	err := run(testConfig(p, ""), testEnv(nil), strings.NewReader("ping"), &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "ping" {
		t.Fatalf("stdout %q", stdout.String())
	}
	if h, ok := p.firstRequest(t, err).headers["proxy-authorization"]; ok {
		t.Fatalf("sent Proxy-Authorization %q without a user", h)
	}
}

func TestFailures(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		vars    map[string]string
		handle  func(proxyRequest, *bufio.Reader, net.Conn)
		want    string
		connect int32
	}{
		{"wrong credentials", "alice", map[string]string{"HTTP_PROXY_PASSWORD": "bad"},
			reply("407 Proxy Authentication Required", `Proxy-Authenticate: Basic realm="proxy"`),
			"rejected the credentials of alice (407)", 1},
		{"authentication required", "", nil,
			reply("407 Proxy Authentication Required", `Proxy-Authenticate: Basic realm="proxy"`),
			"requires authentication (407); give USER@ in -H or HTTP_PROXY_USER", 1},
		{"forbidden", "", nil, reply("403 Forbidden"), "refused CONNECT example.test:22 (403 Forbidden)", 1},
		{"bad gateway", "", nil, reply("502 Bad Gateway"), "refused CONNECT example.test:22 (502 Bad Gateway)", 1},
		{"no password", "alice", nil, reply("200 OK"), "no password for alice@127.0.0.1:", 0},
		{"closed without response", "", nil, func(proxyRequest, *bufio.Reader, net.Conn) {},
			"closed the connection without a complete response", 1},
		{"not HTTP", "", nil, func(_ proxyRequest, _ *bufio.Reader, c net.Conn) { fmt.Fprint(c, "SSH-2.0-x\r\n\r\n") },
			`unexpected status line "SSH-2.0-x"`, 1},
		{"headers too large", "", nil, func(_ proxyRequest, _ *bufio.Reader, c net.Conn) {
			fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nX: %s\r\n\r\n", strings.Repeat("a", maxHeaderBytes))
		}, "response headers exceed", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := startProxy(t, tt.handle)
			err := run(testConfig(p, tt.user), testEnv(tt.vars), strings.NewReader(""), io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			// The listener counts connections asynchronously.
			time.Sleep(50 * time.Millisecond)
			if n := p.conns.Load(); n != tt.connect {
				t.Fatalf("%d connections to the proxy, want %d (407 must not be retried)", n, tt.connect)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	p := startProxy(t, func(proxyRequest, *bufio.Reader, net.Conn) { <-hold })
	cfg := testConfig(p, "")
	cfg.timeout = 300 * time.Millisecond
	start := time.Now()
	err := run(cfg, testEnv(nil), strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "timed out after 300ms waiting for SSH route proxy") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestConnectFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := config{proxyHost: "127.0.0.1", proxyPort: port, destHost: "example.test", destPort: 22, timeout: 5 * time.Second}
	err = run(cfg, testEnv(nil), strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cannot connect to SSH route proxy 127.0.0.1:"+strconv.Itoa(port)) {
		t.Fatalf("err = %v", err)
	}
}

func TestIPv6Destination(t *testing.T) {
	p := startProxy(t, reply("403 Forbidden"))
	cfg := testConfig(p, "")
	cfg.destHost = "2001:db8::1"
	err := run(cfg, testEnv(nil), strings.NewReader(""), io.Discard, io.Discard)
	if r := p.firstRequest(t, err); r.line != "CONNECT [2001:db8::1]:22 HTTP/1.1" || r.headers["host"] != "[2001:db8::1]:22" {
		t.Fatalf("request %+v", r)
	}
}

// TestCredentialSources checks the order of the user name and password sources.
func TestCredentialSources(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proxy.pass")
	writePasswordFile(t, file, []byte("from-file\n"))
	tests := []struct {
		name     string
		specUser string
		file     string
		vars     map[string]string
		prompt   string
		want     string
	}{
		{"user from -H wins", "alice", "", map[string]string{"HTTP_PROXY_USER": "bob", "CONNECT_USER": "carol", "CONNECT_PASSWORD": "p"}, "", basic("alice", "p")},
		{"HTTP_PROXY_USER before CONNECT_USER", "", "", map[string]string{"HTTP_PROXY_USER": "bob", "CONNECT_USER": "carol", "CONNECT_PASSWORD": "p"}, "", basic("bob", "p")},
		{"CONNECT_USER", "", "", map[string]string{"CONNECT_USER": "carol", "CONNECT_PASSWORD": "p"}, "", basic("carol", "p")},
		{"password file wins", "alice", file, map[string]string{"HTTP_PROXY_PASSWORD": "env1", "CONNECT_PASSWORD": "env2"}, "typed", basic("alice", "from-file")},
		{"HTTP_PROXY_PASSWORD before CONNECT_PASSWORD", "alice", "", map[string]string{"HTTP_PROXY_PASSWORD": "env1", "CONNECT_PASSWORD": "env2"}, "typed", basic("alice", "env1")},
		{"empty HTTP_PROXY_PASSWORD is a password", "alice", "", map[string]string{"HTTP_PROXY_PASSWORD": ""}, "typed", basic("alice", "")},
		{"prompt last", "alice", "", nil, "typed", basic("alice", "typed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := startProxy(t, reply("403 Forbidden"))
			cfg := testConfig(p, tt.specUser)
			cfg.passwordFile = tt.file
			env := testEnv(tt.vars)
			env.prompt = func(string) (string, bool, error) { return tt.prompt, true, nil }
			err := run(cfg, env, strings.NewReader(""), io.Discard, io.Discard)
			if got := p.firstRequest(t, err).headers["proxy-authorization"]; got != tt.want {
				t.Fatalf("Proxy-Authorization %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadPasswordFile(t *testing.T) {
	utf16le := func(s string) []byte {
		b := []byte{0xFF, 0xFE}
		for _, r := range s {
			b = append(b, byte(r), 0)
		}
		return b
	}
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{"LF", []byte("pw\n"), "pw"},
		{"no newline", []byte("pw"), "pw"},
		{"CRLF and more lines", []byte("p w\r\nsecond\r\n"), "p w"},
		{"UTF-8 BOM", []byte("\xEF\xBB\xBFpw\r\n"), "pw"},
		{"UTF-16LE BOM", utf16le("pw\r\n"), "pw"},
		{"non-ASCII", []byte("pässwörd\n"), "pässwörd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p")
			writePasswordFile(t, path, tt.content)
			got, err := readPasswordFile(path)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "empty")
	writePasswordFile(t, path, nil)
	if _, err := readPasswordFile(path); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("empty file: %v", err)
	}
	if _, err := readPasswordFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args []string
		want config
	}{
		{[]string{"-H", "proxy.example.com:8080", "target", "22"},
			config{proxyHost: "proxy.example.com", proxyPort: 8080, destHost: "target", destPort: 22, timeout: defaultTimeout}},
		{[]string{"-Halice@proxy.example.com", "-w", "5", "-dd", "target", "2222"},
			config{proxyHost: "proxy.example.com", proxyPort: 80, user: "alice", destHost: "target", destPort: 2222, timeout: 5 * time.Second, debug: 2}},
		{[]string{"-H", "http://al@ice@proxy.example.com:3128/path", "-w0", "--password-file", "/p", "target", "22"},
			config{proxyHost: "proxy.example.com", proxyPort: 3128, user: "al@ice", destHost: "target", destPort: 22, passwordFile: "/p"}},
		{[]string{"--password-file=/p", "-H", "[2001:db8::2]:8080", "[2001:db8::1]", "22"},
			config{proxyHost: "2001:db8::2", proxyPort: 8080, destHost: "2001:db8::1", destPort: 22, timeout: defaultTimeout, passwordFile: "/p"}},
		{[]string{"-dH", "proxy", "--", "-target", "22"},
			config{proxyHost: "proxy", proxyPort: 80, destHost: "-target", destPort: 22, timeout: defaultTimeout, debug: 1}},
		{[]string{"-V"}, config{timeout: defaultTimeout, version: true}},
	}
	for _, tt := range tests {
		got, err := parseArgs(tt.args)
		if err != nil || got != tt.want {
			t.Errorf("parseArgs(%q) = %+v, %v; want %+v", tt.args, got, err, tt.want)
		}
	}
	errs := []struct {
		args []string
		want string
	}{
		{[]string{"-h", "target", "22"}, "option -h is not supported: give the SSH route proxy explicitly with -H"},
		{[]string{"-S", "socks:1080", "target", "22"}, "option -S is not supported (only HTTP CONNECT through -H is supported)"},
		{[]string{"-4", "-H", "p", "target", "22"}, "option -4 is not supported"},
		{[]string{"-x", "-H", "p", "target", "22"}, "unknown option -x"},
		{[]string{"--proxy", "p"}, "unknown option --proxy"},
		{[]string{"target", "22"}, "-H is required"},
		{[]string{"-H", "p", "target"}, "HOST and PORT are required"},
		{[]string{"-H", "p", "target", "0"}, `invalid destination port "0"`},
		{[]string{"-H", "p:99999", "target", "22"}, "invalid proxy port"},
		{[]string{"-H", "https://p", "target", "22"}, `proxy scheme "https" is not supported`},
		{[]string{"-H", "socks5://p", "target", "22"}, `proxy scheme "socks5" is not supported`},
		{[]string{"-H", "alice:pw@p", "target", "22"}, "must not contain ':'"},
		{[]string{"-H", "2001:db8::2", "target", "22"}, "enclose the IPv6 address"},
		{[]string{"-H", "@p", "target", "22"}, "empty user name"},
		{[]string{"-H", "p", "-w", "x", "target", "22"}, `invalid -w value "x"`},
		{[]string{"-H"}, "option -H requires an argument"},
		{[]string{"-H", "p", "--password-file"}, "option --password-file requires a path"},
	}
	for _, tt := range errs {
		if _, err := parseArgs(tt.args); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("parseArgs(%q) error = %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestMainExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main([]string{"-V"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(errOut.String(), "Version ") || out.Len() != 0 {
		t.Fatalf("-V: code %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	errOut.Reset()
	if code := Main([]string{"--help"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(errOut.String(), "usage: host-shell connect") {
		t.Fatalf("--help: code %d, stderr %q", code, errOut.String())
	}
	errOut.Reset()
	if code := Main([]string{"target", "22"}, strings.NewReader(""), &out, &errOut); code != 1 ||
		!strings.HasPrefix(errOut.String(), "host-shell connect: -H is required") || !strings.Contains(errOut.String(), "usage:") {
		t.Fatalf("missing -H: code %d, stderr %q", code, errOut.String())
	}
	errOut.Reset()
	if code := Main([]string{"-S", "s", "target", "22"}, strings.NewReader(""), &out, &errOut); code != 1 || strings.Contains(errOut.String(), "usage:") {
		t.Fatalf("-S: code %d, stderr %q", code, errOut.String())
	}
}
