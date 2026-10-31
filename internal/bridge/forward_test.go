package bridge

import (
	"context"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/forward"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/server"
)

// connectForward runs an agent, a bridge and a target that permits the given directions.
func connectForward(t *testing.T, local, remote bool, opt Options) (*mcp.ClientSession, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := config.Default()
	cfg.LogFile = ""
	al, _ := audit.Open("")
	pol, _ := policy.New(policy.Options{}, nil)
	var to channel.Options
	if local || remote {
		cfg.Forward = forward.NewTarget(local, remote, pol, al)
		t.Cleanup(cfg.Forward.Close)
		cfg.Forward.Register(&to)
	}
	srv, stop, err := server.New(cfg, al)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	rs, rc := mcp.NewInMemoryTransports()
	go srv.Run(ctx, channel.Target(rs, to))
	ls, lc := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt, rc, ls) }()
	type result struct {
		cs  *mcp.ClientSession
		err error
	}
	connected := make(chan result, 1)
	go func() {
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, nil).Connect(ctx, lc, nil)
		connected <- result{cs, err}
	}()
	select {
	case err := <-done:
		return nil, err
	case r := <-connected:
		if r.err != nil {
			t.Fatal(r.err)
		}
		t.Cleanup(func() { r.cs.Close() })
		return r.cs, nil
	}
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestForwardToolExposure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opt   Options
		tools []string
		none  []string
	}{
		{"none", Options{}, nil, []string{"forward_status", "forward_open", "forward_close"}},
		{"persistent only", Options{Forwards: []forward.Spec{{Direction: forward.Local, Bind: "127.0.0.1", Host: "localhost", HostPort: 1}}}, []string{"forward_status"}, []string{"forward_open", "forward_close"}},
		{"rules", Options{ForwardAllowRemote: []string{"localhost"}}, []string{"forward_status", "forward_open", "forward_close"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := connectForward(t, true, true, tc.opt)
			if err != nil {
				t.Fatal(err)
			}
			names := toolNames(t, cs)
			for _, n := range tc.tools {
				if !slices.Contains(names, n) {
					t.Errorf("%s missing", n)
				}
			}
			for _, n := range tc.none {
				if slices.Contains(names, n) {
					t.Errorf("%s exposed", n)
				}
			}
			if len(tc.tools) > 0 && !strings.Contains(cs.InitializeResult().Instructions, "forward_status") {
				t.Error("instructions do not mention forward_status")
			}
		})
	}
}

func TestForwardSession(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	cs, err := connectForward(t, true, true, Options{ForwardAllowLocal: []string{"127.0.0.1"}, ForwardAllowRemote: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	remote := call[forward.Info](t, cs, "forward_open", map[string]any{"direction": "remote", "host": "127.0.0.1", "port": port})
	info := call[struct {
		Local   bool                    `json:"forward_local"`
		Remote  bool                    `json:"forward_remote"`
		Forward []forward.RemoteForward `json:"remote_forwards"`
	}](t, cs, "sys_get_info", nil)
	if !info.Local || !info.Remote || len(info.Forward) != 1 || info.Forward[0].ForwardID != remote.ForwardID {
		t.Fatalf("sys_get_info %+v", info)
	}
	local := call[forward.Info](t, cs, "forward_open", map[string]any{"direction": "local", "host": "127.0.0.1", "port": port})
	for _, p := range []int{local.ListenPort, remote.ListenPort} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		c.Write([]byte("ping"))
		b := make([]byte, 4)
		if _, err = io.ReadFull(c, b); err != nil || string(b) != "ping" {
			t.Fatalf("%q %v", b, err)
		}
		c.Close()
	}
	status := call[forward.Status](t, cs, "forward_status", nil)
	if status.State != "ready" || len(status.Forwards) != 2 || len(status.Events) == 0 {
		t.Fatalf("status %+v", status)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "forward_open", Arguments: map[string]any{"direction": "local", "host": "denied.example", "port": 80}})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "--forward-allow-local") {
		t.Fatalf("denied open: %v %+v", err, res)
	}
	call[forward.CloseInput](t, cs, "forward_close", map[string]any{"forward_id": remote.ForwardID})
}

func TestForwardNotPermitted(t *testing.T) {
	_, err := connectForward(t, true, false, Options{Forwards: []forward.Spec{{Direction: forward.Remote, Bind: "127.0.0.1", Host: "localhost", HostPort: 1}}})
	if err == nil || !strings.Contains(err.Error(), "--allow-forward-remote") {
		t.Fatalf("remote forward without permission: %v", err)
	}
	// A target that permits forwarding still serves a bridge that requests nothing.
	cs, err := connectForward(t, true, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	info := call[struct {
		Local bool `json:"forward_local"`
	}](t, cs, "sys_get_info", nil)
	if !info.Local {
		t.Fatal("permission not reported")
	}
}
