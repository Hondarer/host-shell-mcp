package channel

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type transport struct {
	base   mcp.Transport
	opt    Options
	bridge bool
}

// Client wraps the bridge's connection to the target. Use it only when at least one feature is requested.
func Client(base mcp.Transport, opt Options) mcp.Transport {
	return &transport{base: base, opt: opt, bridge: true}
}

// Target wraps the target's stdio connection. It inspects the first message: ordinary MCP passes
// through unchanged unless the HTTP proxy is enabled, which requires the bridge's hello.
func Target(base mcp.Transport, opt Options) mcp.Transport {
	return &transport{base: base, opt: opt}
}
