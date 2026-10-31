package bridge

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/forward"
)

func forwardInstructions(m *forward.Manager) string {
	var b strings.Builder
	b.WriteString("Port forwarding enabled over this session: use forward_status to list forwards, destination rules and recent connection results.")
	if m.Tools() {
		b.WriteString(" forward_open and forward_close create and remove forwards within the bridge's --forward-allow-local/--forward-allow-remote rules.")
	}
	status, _ := m.Status(forward.StatusInput{})
	if len(status.Forwards) > 0 {
		b.WriteString(" Persistent forwards:")
		for i, f := range status.Forwards {
			if i > 0 {
				b.WriteString(";")
			}
			b.WriteString(" " + f.Describe())
		}
		b.WriteString(".")
	}
	return b.String()
}

func registerForwardTools(srv *mcp.Server, m *forward.Manager) {
	mcp.AddTool(srv, &mcp.Tool{Name: "forward_status", Title: "Port forward status", Description: "List port forwards over this session (local: listen here and connect from the target; remote: listen on the target and connect from here; socks: SOCKS5 here), the destination rules, and recent connection results. Use after_seq and limit to page through denied or failed connections. Forwarded data is never returned.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, in forward.StatusInput) (*mcp.CallToolResult, forward.Status, error) {
		out, err := m.Status(in)
		return nil, out, err
	})
	if !m.Tools() {
		return
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "forward_open", Title: "Open port forward", Description: "Create a TCP port forward over this session, listening on loopback only. local listens on this machine and connects from the target to host:port (for example a dev server on the target); remote listens on the target and connects from this machine to host:port; socks serves SOCKS5 on this machine (use socks5h:// so the target resolves names). Destinations must match the bridge's --forward-allow-local (local, socks) or --forward-allow-remote (remote) rules. listen_port 0 picks a free port; the result reports it."}, func(ctx context.Context, req *mcp.CallToolRequest, in forward.OpenInput) (*mcp.CallToolResult, forward.Info, error) {
		out, err := m.Open(ctx, in)
		return nil, out, err
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "forward_close", Title: "Close port forward", Description: "Close a forward created by forward_open and disconnect its connections. Forwards defined by bridge options cannot be closed."}, func(ctx context.Context, req *mcp.CallToolRequest, in forward.CloseInput) (*mcp.CallToolResult, forward.CloseInput, error) {
		return nil, in, m.CloseForward(in.ForwardID)
	})
}
