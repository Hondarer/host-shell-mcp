package bridge

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
)

func registerProxyTools(srv *mcp.Server, gateway *clientproxy.Gateway, mutable bool) {
	if gateway == nil {
		return
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "proxy_status", Title: "Client proxy status", Description: "Inspect client proxy state, destination rules and recent connection results. Use after_seq and limit to page through denied or failed destinations. No credentials or HTTP contents are returned.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, in clientproxy.StatusInput) (*mcp.CallToolResult, clientproxy.Status, error) {
		out, err := gateway.Status(in)
		return nil, out, err
	})
	if mutable {
		mcp.AddTool(srv, &mcp.Tool{Name: "proxy_rules_update", Title: "Update client proxy destination rules", Description: "Replace this session's complete allow_hosts and deny_hosts lists without reconnecting. Requires expected_revision from proxy_status; stale updates fail. Deny overrides allow; empty allow_hosts blocks all new destinations. Existing connections remain open. Changes are not persisted. Change destinations only when authorized by the user.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)}}, func(ctx context.Context, req *mcp.CallToolRequest, in clientproxy.UpdateRulesInput) (*mcp.CallToolResult, clientproxy.Rules, error) {
			out, err := gateway.UpdateRules(in)
			return nil, out, err
		})
	}
}
