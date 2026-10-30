// Package server assembles host-shell's MCP server.
package server

import (
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/tools"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// New creates the MCP server with all enabled tools registered.
// The returned close function stops any running background jobs when the session ends.
func New(cfg config.Config, al *audit.Logger) (s *mcp.Server, close func(), err error) {
	pol, err := policy.New(cfg.Policy, tools.Catalog())
	if err != nil {
		return nil, nil, err
	}
	shell := proc.DefaultShell(cfg.WindowsShell)
	jm := jobs.NewManager(shell, cfg.MaxOutputBytes*4)

	d := &tools.Deps{
		Cfg:          cfg,
		Audit:        al,
		Jobs:         jm,
		DefaultShell: shell,
		Policy:       pol,
	}
	impl := &mcp.Implementation{Name: "host-shell", Title: "host-shell", Version: version.Version}
	// Dry-register on a temporary server first so d.Exposed is populated before building Instructions.
	tools.Register(mcp.NewServer(impl, nil), d)
	s = mcp.NewServer(impl, &mcp.ServerOptions{Instructions: tools.Instructions(d)})
	tools.Register(s, d)
	return s, func() { jm.StopAll(5 * time.Second) }, nil
}
