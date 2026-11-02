package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
	"github.com/hondarer/host-shell-mcp/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProxyRuleTools(t *testing.T) {
	for _, mutable := range []bool{false, true} {
		name := "immutable"
		if mutable {
			name = "mutable"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			g, err := clientproxy.NewGateway([]string{"initial.example:443"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "proxy-rules-test"}, nil)
			registerProxyTools(srv, g, mutable)
			st, ct := mcp.NewInMemoryTransports()
			ss, err := srv.Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			found := false
			for tool, err := range cs.Tools(ctx, nil) {
				if err != nil {
					t.Fatal(err)
				}
				if tool.Name == "proxy_rules_update" {
					found = true
				}
			}
			if found != mutable {
				t.Fatalf("update tool exposed=%v mutable=%v", found, mutable)
			}
			status := testutil.Call[clientproxy.Status](t, cs, "proxy_status", nil)
			if status.Revision != 1 {
				t.Fatal(status)
			}
			if !mutable {
				return
			}
			in := map[string]any{"expected_revision": status.Revision, "allow_hosts": []string{"*.example.com:443"}, "deny_hosts": []string{"blocked.example.com:443"}}
			rules := testutil.Call[clientproxy.Rules](t, cs, "proxy_rules_update", in)
			if rules.Revision != 2 || len(rules.DenyHosts) != 1 {
				t.Fatal(rules)
			}
			status = testutil.Call[clientproxy.Status](t, cs, "proxy_status", nil)
			if status.Revision != rules.Revision || status.AllowHosts[0] != "*.example.com:443" {
				t.Fatal(status)
			}
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "proxy_rules_update", Arguments: in})
			if err != nil || !result.IsError {
				t.Fatalf("stale update should fail: %+v %v", result, err)
			}
		})
	}
}

func TestProxyManagementRequiresProxy(t *testing.T) {
	for _, opt := range []Options{{ProxyAllowRuleUpdates: true}, {ProxyDenyHosts: []string{"denied.example"}}} {
		if _, err := newGateway(opt, &channel.Options{}); err == nil {
			t.Fatal("proxy-disabled configuration accepted")
		}
	}
}
