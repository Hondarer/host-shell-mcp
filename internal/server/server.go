// Package server assembles host-shell's MCP server.
package server

import (
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/forward"
	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/tools"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// Session holds what a server uses besides its configuration.
type Session struct {
	Policy  *policy.Policy // see NewPolicy
	Audit   *audit.Logger
	Proxy   *clientproxy.Target // nil unless --client-proxy
	Forward *forward.Target     // nil unless port forwarding is permitted
}

// NewPolicy builds the session policy from cfg. When the client proxy is enabled its
// credential is added as a secret and the audit log masks it, so every consumer of the
// returned policy (tools and port forwarding) redacts the same way.
func NewPolicy(cfg config.Config, proxy *clientproxy.Target, al *audit.Logger) (*policy.Policy, error) {
	pol, err := policy.New(cfg.Policy, tools.Catalog())
	if err != nil {
		return nil, err
	}
	if proxy != nil {
		pol.AddSecret(proxy.Secret())
		al.SetMask(proxy.Mask)
	}
	return pol, nil
}

// New creates the MCP server with all enabled tools registered.
// The returned stop function stops any running background jobs when the session ends.
func New(cfg config.Config, sess Session) (s *mcp.Server, stop func()) {
	shell := proc.DefaultShell(cfg.WindowsShell)
	jm := jobs.NewManager(shell, cfg.MaxOutputBytes*4)

	d := &tools.Deps{
		Cfg:          cfg,
		Audit:        sess.Audit,
		Jobs:         jm,
		DefaultShell: shell,
		Policy:       sess.Policy,
		Proxy:        sess.Proxy,
		Forward:      sess.Forward,
	}
	impl := &mcp.Implementation{Name: "host-shell", Title: "host-shell", Version: version.Version}
	registry := tools.Prepare(d)
	s = mcp.NewServer(impl, &mcp.ServerOptions{Instructions: tools.Instructions(d)})
	registry.Register(s)
	return s, func() { jm.StopAll(5 * time.Second) }
}
