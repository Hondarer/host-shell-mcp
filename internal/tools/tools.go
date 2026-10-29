// Package tools は host-shell が提供する MCP ツールを定義します。
package tools

import (
	"context"
	"runtime"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/jobs"
)

// Deps はツールの実装が共有する依存です。
type Deps struct {
	Cfg          config.Config
	Audit        *audit.Logger
	Jobs         *jobs.Manager
	DefaultShell string
}

// Register はすべてのツールをサーバーに登録します。
func Register(s *mcp.Server, d *Deps) {
	// コマンド実行
	add(s, d, &mcp.Tool{
		Name:        "exec_run",
		Description: "Run a command on the host and wait for it to finish. Use `command` for a shell command line, or `argv` to run a program without a shell.",
		Annotations: destructive(),
	}, d.execRun)

	// 長時間の実行
	add(s, d, &mcp.Tool{
		Name:        "job_start",
		Description: "Start a long-running command in the background and return its job id. Read its output later with job_read.",
		Annotations: destructive(),
	}, d.jobStart)
	add(s, d, &mcp.Tool{
		Name:        "job_read",
		Description: "Read the combined stdout/stderr of a background job from a byte offset, together with its status. Optionally wait for new output.",
		Annotations: readOnly(),
	}, d.jobRead)
	add(s, d, &mcp.Tool{
		Name:        "job_stop",
		Description: "Stop a background job (including its child processes).",
		Annotations: destructive(),
	}, d.jobStop)
	add(s, d, &mcp.Tool{
		Name:        "job_list",
		Description: "List background jobs started in this session.",
		Annotations: readOnly(),
	}, d.jobList)

	// ファイル操作
	add(s, d, &mcp.Tool{
		Name:        "fs_list",
		Description: "List entries of a directory.",
		Annotations: readOnly(),
	}, d.fsList)
	add(s, d, &mcp.Tool{
		Name:        "fs_stat",
		Description: "Get attributes of a file or directory (does not follow a final symlink).",
		Annotations: readOnly(),
	}, d.fsStat)
	add(s, d, &mcp.Tool{
		Name:        "fs_read",
		Description: "Read a file. Text mode reads by lines (line_start/line_count) or by bytes (offset/length); base64 mode reads a byte range of a binary file.",
		Annotations: readOnly(),
	}, d.fsRead)
	add(s, d, &mcp.Tool{
		Name:        "fs_write",
		Description: "Write a file. Content may be text or base64. Mode is overwrite (default), append or create (fail if exists).",
		Annotations: destructive(),
	}, d.fsWrite)

	// 検索
	add(s, d, &mcp.Tool{
		Name:        "search_grep",
		Description: "Search file contents recursively with a regular expression (Go RE2 syntax), like ripgrep. Binary files are skipped.",
		Annotations: readOnly(),
	}, d.searchGrep)
	add(s, d, &mcp.Tool{
		Name:        "search_files",
		Description: "Find files or directories recursively whose base name matches a glob pattern.",
		Annotations: readOnly(),
	}, d.searchFiles)

	// 差分の確認
	add(s, d, &mcp.Tool{
		Name:        "fs_manifest",
		Description: "List files under a directory with their size and SHA-256. Compare with a local copy and read only the files that differ.",
		Annotations: readOnly(),
	}, d.fsManifest)

	// 特権実行 (Linux のみ)
	if runtime.GOOS != "windows" {
		add(s, d, &mcp.Tool{
			Name:        "sudo_run",
			Description: "Run a shell command with sudo non-interactively (sudo -n). Requires a NOPASSWD sudoers rule on the host.",
			Annotations: destructive(),
		}, d.sudoRun)
	}

	// 環境情報
	add(s, d, &mcp.Tool{
		Name:        "sys_info",
		Description: "Get information about the host: OS, architecture, hostname, user, working directory, default shell.",
		Annotations: readOnly(),
	}, d.sysInfo)
	add(s, d, &mcp.Tool{
		Name:        "sys_processes",
		Description: "List running processes on the host (ps on Linux, tasklist on Windows), optionally filtered by a substring.",
		Annotations: readOnly(),
	}, d.sysProcesses)
}

// add は操作ログの記録を挟んでツールを登録します。
func add[In, Out any](s *mcp.Server, d *Deps, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		res, out, err := h(ctx, req, in)
		d.Audit.Tool(t.Name, in, time.Since(start), res != nil && res.IsError, err)
		return res, out, err
	})
}

// readOnly は接続先を変更しないツールの注釈です。
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
}

// destructive は接続先を変更しうるツールの注釈です。エージェント側で承認を必須にする目安になります。
func destructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
}

func ptr[T any](v T) *T { return &v }
