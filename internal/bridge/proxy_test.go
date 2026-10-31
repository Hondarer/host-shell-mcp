package bridge

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProxySessionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	target, err := clientproxy.NewTarget()
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	cfg := config.Default()
	cfg.Proxy = target
	al, _ := audit.Open("")
	srv, stop, err := server.New(cfg, al)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	rs, rc := mcp.NewInMemoryTransports()
	remoteDone := make(chan error, 1)
	var targetOptions channel.Options
	target.Register(&targetOptions)
	go func() { remoteDone <- srv.Run(ctx, channel.Target(rs, targetOptions)) }()
	ls, lc := mcp.NewInMemoryTransports()
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- Run(ctx, Options{Proxy: true, ProxyAllowHosts: []string{"allowed.example:443"}}, rc, ls)
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "proxy-test"}, nil).Connect(ctx, lc, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	info := call[struct {
		ClientProxy bool   `json:"client_proxy"`
		File        string `json:"proxy_url_file"`
	}](t, cs, "sys_get_info", nil)
	if !info.ClientProxy || info.File != target.File {
		t.Fatalf("info %+v", info)
	}
	status := call[clientproxy.Status](t, cs, "proxy_status", nil)
	if status.State != "ready" {
		t.Fatalf("status %+v", status)
	}
	content := call[struct {
		Content string `json:"content"`
	}](t, cs, "fs_read", map[string]any{"path": target.File})
	if strings.Contains(content.Content, target.Secret()) || !strings.Contains(content.Content, "[REDACTED]") {
		t.Fatal("URL file secret leaked")
	}
	command := "printf '%s' \"$HTTP_PROXY\""
	if runtime.GOOS == "windows" {
		command = "$env:HTTP_PROXY"
	}
	out := call[struct {
		Stdout string `json:"stdout"`
	}](t, cs, "exec_run", map[string]any{"command": command})
	if strings.Contains(out.Stdout, target.Secret()) || !strings.Contains(out.Stdout, "[REDACTED]") {
		t.Fatal("environment missing or secret leaked")
	}
	// A denied request must be discoverable through the bridge's status tool.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "net_request_http", Arguments: map[string]any{"url": "http://denied.example", "proxy": target.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("HTTP tool error: %v", res.Content)
	}
	status = call[clientproxy.Status](t, cs, "proxy_status", nil)
	if len(status.Events) == 0 || status.Events[0].Status != "denied" {
		t.Fatalf("missing denial %+v", status)
	}
	cs.Close()
	cancel()
	for _, done := range []chan error{bridgeDone, remoteDone} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("session did not stop")
		}
	}
	target.Close()
	if _, err = os.Stat(target.File); !os.IsNotExist(err) {
		t.Fatal("credential file not removed")
	}
}
