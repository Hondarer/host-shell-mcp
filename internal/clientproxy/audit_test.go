package clientproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// auditRecords reads the records with message msg from the daily audit files written for base.
func auditRecords(t *testing.T, base, msg string) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob(strings.TrimSuffix(base, ".log") + "*")
	var out []map[string]any
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var r map[string]any
			if json.Unmarshal(sc.Bytes(), &r) == nil && r["msg"] == msg {
				out = append(out, r)
			}
		}
		f.Close()
	}
	return out
}

func auditTarget(t *testing.T, rules []string) (*Target, string) {
	t.Helper()
	target, _, _, _ := pair(t, rules)
	log := filepath.Join(t.TempDir(), "audit.log")
	al, err := audit.Open(log)
	if err != nil {
		t.Fatal(err)
	}
	al.SetMask(target.Mask)
	target.SetAudit(al)
	return target, log
}

func TestAuditProxyConnect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer secure.Close()
	closed, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	target, log := auditTarget(t, []string{"127.0.0.1:*"})
	c := proxyClient(t, target)
	for _, u := range []string{plain.URL + "/private/path?token=query-secret", secure.URL + "/", "http://denied.example/", "http://127.0.0.1:" + strconv.Itoa(closedPort) + "/", "https://127.0.0.1:" + strconv.Itoa(closedPort) + "/"} {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Authorization", "Bearer header-secret")
		if r, err := c.Do(req); err == nil {
			r.Body.Close()
		}
	}
	plainURL, _ := url.Parse(plain.URL)
	secureURL, _ := url.Parse(secure.URL)
	want := []string{
		"http 127.0.0.1 " + plainURL.Port() + " allowed connection established",
		"connect 127.0.0.1 " + secureURL.Port() + " allowed connection established",
		"http denied.example 80 denied destination not allowed",
		// The bridge accepts an HTTP stream before it connects upstream, so its later 502 is not seen here.
		"http 127.0.0.1 " + strconv.Itoa(closedPort) + " allowed connection established",
		"connect 127.0.0.1 " + strconv.Itoa(closedPort) + " failed upstream connection failed",
	}
	var got []string
	for _, r := range auditRecords(t, log, "proxy_connect") {
		got = append(got, strings.Join([]string{r["kind"].(string), r["host"].(string), strconv.Itoa(int(r["port"].(float64))), r["result"].(string), r["reason"].(string)}, " "))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("proxy_connect records:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	b, _ := os.ReadFile(auditFileName(t, log))
	for _, secret := range []string{"private", "query-secret", "header-secret", target.Secret()} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("audit recorded %q: %s", secret, b)
		}
	}
	// Limits and timeouts end the same way whether the bridge or the target reports them.
	for err, want := range map[error]string{
		channel.ErrLimit:                  "denied connection limit reached",
		&channel.Error{Code: "limit"}:     "denied connection limit reached",
		context.DeadlineExceeded:          "failed connection timeout",
		&channel.Error{Code: "timeout"}:   "failed connection timeout",
		&channel.Error{Code: "refused"}:   "failed upstream connection failed",
		errors.New("stream setup failed"): "failed upstream connection failed",
	} {
		target.connect(channel.KindHTTP, "limit.example", 80, err)
		records := auditRecords(t, log, "proxy_connect")
		last := records[len(records)-1]
		if got := last["result"].(string) + " " + last["reason"].(string); got != want {
			t.Errorf("%v: %s, want %s", err, got, want)
		}
	}
}

func auditFileName(t *testing.T, base string) string {
	t.Helper()
	m, _ := filepath.Glob(strings.TrimSuffix(base, ".log") + "*")
	if len(m) == 0 {
		t.Fatal("no audit file")
	}
	return m[0]
}

func TestAuditAuthFailures(t *testing.T) {
	target, log := auditTarget(t, []string{"127.0.0.1:*"})
	// The test ends each one-minute window itself.
	var tick func()
	target.authFailed.after = func(_ time.Duration, f func()) func() bool {
		tick = f
		return func() bool { return true }
	}
	u, _ := url.Parse(target.URL)
	u.User = nil
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	defer tr.CloseIdleConnections()
	fail := func(n int) {
		t.Helper()
		for range n {
			r, err := tr.RoundTrip(mustRequest(t, "http://secret-destination.example/"))
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if r.StatusCode != 407 {
				t.Fatalf("status %d", r.StatusCode)
			}
		}
	}
	counts := func() []int {
		var out []int
		for _, r := range auditRecords(t, log, "proxy_auth_failed") {
			out = append(out, int(r["count"].(float64)))
		}
		return out
	}
	check := func(want ...int) {
		t.Helper()
		if got := counts(); !slices.Equal(got, want) {
			t.Fatalf("counts %v, want %v", got, want)
		}
	}
	fail(3)
	check(1) // the first failure at once, the other two counted
	tick()
	check(1, 2)
	tick() // a window without failures ends counting
	check(1, 2)
	fail(2)
	check(1, 2, 1)
	target.Close() // the session end records what is still counted
	check(1, 2, 1, 1)
	b, _ := os.ReadFile(auditFileName(t, log))
	if strings.Contains(string(b), "secret-destination") || len(auditRecords(t, log, "proxy_connect")) != 0 {
		t.Fatalf("unauthenticated requests recorded: %s", b)
	}
}
