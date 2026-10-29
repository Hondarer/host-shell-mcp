// Package server は host-shell の MCP サーバーを組み立てます。
package server

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/tools"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// New はツールを登録した MCP サーバーを作ります。
// 戻り値の close は、セッションの終了後に実行中のジョブを停止するために呼びます。
func New(cfg config.Config, al *audit.Logger) (s *mcp.Server, close func()) {
	shell := proc.DefaultShell(cfg.WindowsShell)
	jm := jobs.NewManager(shell, cfg.MaxOutputBytes*4)

	s = mcp.NewServer(&mcp.Implementation{
		Name:    "host-shell",
		Title:   "host-shell",
		Version: version.Version,
	}, &mcp.ServerOptions{
		Instructions: instructions(shell),
	})
	tools.Register(s, &tools.Deps{
		Cfg:          cfg,
		Audit:        al,
		Jobs:         jm,
		DefaultShell: shell,
	})
	return s, func() { jm.StopAll(5 * time.Second) }
}

func instructions(shell string) string {
	host, _ := os.Hostname()
	return fmt.Sprintf(
		"host-shell runs on the remote host %q (%s/%s) and executes operations there. "+
			"Paths are interpreted on that host. The default shell for exec_run is %q. "+
			"Outputs larger than the server limit are truncated, keeping the head and tail.",
		host, runtime.GOOS, runtime.GOARCH, shell)
}
