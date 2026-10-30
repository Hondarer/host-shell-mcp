// Package server は host-shell の MCP サーバーを組み立てます。
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

// New はツールを登録した MCP サーバーを作ります。
// 戻り値の close は、セッションの終了後に実行中のジョブを停止するために呼びます。
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
	// 案内には公開するツールを反映するため、一度、仮のサーバーに登録して公開するツールを決める。
	tools.Register(mcp.NewServer(impl, nil), d)
	s = mcp.NewServer(impl, &mcp.ServerOptions{Instructions: tools.Instructions(d)})
	tools.Register(s, d)
	return s, func() { jm.StopAll(5 * time.Second) }, nil
}
