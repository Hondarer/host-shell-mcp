package clientproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRuleReplacementAndConflicts(t *testing.T) {
	g, err := NewGateway([]string{"*.example.com:443"}, []string{"blocked.example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	if !g.allowed("ok.example.com", 443) || g.allowed("blocked.example.com", 443) || g.allowed("ok.example.com", 80) {
		t.Fatal("initial deny precedence or port matching")
	}
	in := UpdateRulesInput{ExpectedRevision: 1, AllowHosts: []string{"new.example:443"}, DenyHosts: []string{}}
	next, err := g.UpdateRules(in)
	if err != nil || next.Revision != 2 || !g.allowed("new.example", 443) || g.allowed("ok.example.com", 443) {
		t.Fatalf("replacement: %+v %v", next, err)
	}
	in.AllowHosts[0] = "*"
	next.AllowHosts[0] = "*"
	if g.allowed("unlisted.example", 443) {
		t.Fatal("caller modified live rules")
	}
	if _, err = g.UpdateRules(UpdateRulesInput{ExpectedRevision: 1, AllowHosts: []string{"*"}}); err == nil {
		t.Fatal("stale update accepted")
	}
	if _, err = g.UpdateRules(UpdateRulesInput{ExpectedRevision: 2, AllowHosts: []string{"["}}); err == nil {
		t.Fatal("invalid update accepted")
	}
	if g.rules.Load().revision != 2 || !g.allowed("new.example", 443) {
		t.Fatal("failed update changed rules")
	}
	if _, err = g.UpdateRules(UpdateRulesInput{ExpectedRevision: 2, AllowHosts: []string{}, DenyHosts: []string{}}); err != nil || g.allowed("new.example", 443) {
		t.Fatal("empty allowlist must deny all", err)
	}
}

func TestConcurrentRuleUpdates(t *testing.T) {
	g, _ := NewGateway([]string{"initial.example"}, nil)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := g.UpdateRules(UpdateRulesInput{ExpectedRevision: 1, AllowHosts: []string{"new.example"}}); err == nil {
				successes.Add(1)
			}
			for range 20 {
				g.allowed("new.example", 443)
				g.Status(StatusInput{})
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 || g.rules.Load().revision != 2 {
		t.Fatalf("successes=%d revision=%d", successes.Load(), g.rules.Load().revision)
	}
}

func TestRuleChangesOnLiveProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer server.Close()
	target, g, _, _ := pair(t, []string{"unlisted.example"})
	client := proxyClient(t, target)
	get := func(want int) {
		t.Helper()
		r, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		if r.StatusCode != want {
			t.Fatalf("status=%d want=%d", r.StatusCode, want)
		}
	}
	get(403)
	if _, err := g.UpdateRules(UpdateRulesInput{ExpectedRevision: 1, AllowHosts: []string{"127.0.0.1:*"}}); err != nil {
		t.Fatal(err)
	}
	get(200)
	if _, err := g.UpdateRules(UpdateRulesInput{ExpectedRevision: 2, AllowHosts: []string{"127.0.0.1:*"}, DenyHosts: []string{"127.0.0.1:*"}}); err != nil {
		t.Fatal(err)
	}
	get(403)
}

func TestRuleChangesPreserveExistingCONNECT(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer server.Close()
	target, g, _, _ := pair(t, []string{"127.0.0.1:*"})
	client := proxyClient(t, target)
	get := func() {
		t.Helper()
		r, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(r.StatusCode)
		}
	}
	get()
	_, err := g.UpdateRules(UpdateRulesInput{ExpectedRevision: 1, AllowHosts: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	get() // the established CONNECT tunnel remains usable
	client.CloseIdleConnections()
	r, err := client.Get(server.URL)
	if r != nil {
		r.Body.Close()
	}
	if err == nil {
		t.Fatalf("new CONNECT should be denied: %v", err)
	}
	status, err := g.Status(StatusInput{})
	if err != nil || len(status.Events) == 0 || status.Events[len(status.Events)-1].Result != "denied" {
		t.Fatalf("missing new CONNECT denial: %+v %v", status, err)
	}
}
