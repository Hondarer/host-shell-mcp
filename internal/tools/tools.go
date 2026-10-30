// Package tools は host-shell が提供する MCP ツールを定義します。
package tools

import (
	"context"
	"runtime"
	"strings"
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
//
// 説明 (Description) はエージェントが読むものです。何をするか、いつ使うか、何と組み合わせるかを書きます。
func Register(s *mcp.Server, d *Deps) {
	// 使い方
	add(s, d, &mcp.Tool{
		Name:  "help",
		Title: "Usage guide",
		Description: "Explain how to use host-shell: workflow, tool choice, arguments and examples. " +
			"Call it when unsure which tool to use or how an argument works. Topics: " + strings.Join(helpTopics, ", ") + ", all.",
		Annotations: readOnly(),
	}, d.help)

	// コマンド実行
	add(s, d, &mcp.Tool{
		Name:  "exec_run",
		Title: "Run command",
		Description: "Run a command on the remote host and wait for it (up to timeout_sec). Returns exit_code, stdout, stderr. " +
			"Use command for a shell command line (default shell in sys_info) or argv to run a program with exact arguments and no shell. " +
			"Each call is a new process: pass cwd and env; cd/export do not persist. stdin is empty unless given, so prompts fail instead of hanging. " +
			"For commands that may run longer than a minute or need input while running, use job_start. To read or change files prefer fs_read/fs_edit/fs_write.",
		Annotations: destructive(),
	}, d.execRun)

	// 長時間の実行
	add(s, d, &mcp.Tool{
		Name:  "job_start",
		Title: "Start background job",
		Description: "Start a command in the background and return its id at once. Same arguments as exec_run; no time limit unless timeout_sec. " +
			"Use for builds, test suites, servers, tail -f and other long or streaming commands. Read output with job_read (wait_sec to wait). " +
			"Set stdin_open to send input later with job_write.",
		Annotations: destructive(),
	}, d.jobStart)
	add(s, d, &mcp.Tool{
		Name:  "job_read",
		Title: "Read job output",
		Description: "Read a background job's output (stdout and stderr interleaved) from a byte offset, with its status (running, exit_code). " +
			"Start at offset 0, then pass next_offset to get only new output. Set wait_sec (max 30) to wait for new output or job exit instead of polling.",
		Annotations: readOnly(),
	}, d.jobRead)
	add(s, d, &mcp.Tool{
		Name:  "job_write",
		Title: "Send input to job",
		Description: "Send text to the standard input of a job started with stdin_open, and/or close it (close: true, like Ctrl-D). " +
			"Include \\n to submit a line. There is no terminal (PTY), so programs that require one may not prompt.",
		Annotations: destructive(),
	}, d.jobWrite)
	add(s, d, &mcp.Tool{
		Name:        "job_stop",
		Title:       "Stop job",
		Description: "Stop a background job, killing its whole process tree, and return its final status.",
		Annotations: destructive(),
	}, d.jobStop)
	add(s, d, &mcp.Tool{
		Name:        "job_list",
		Title:       "List jobs",
		Description: "List background jobs of this session with their status. Jobs end when the session ends.",
		Annotations: readOnly(),
	}, d.jobList)

	// ファイル操作
	add(s, d, &mcp.Tool{
		Name:        "fs_list",
		Title:       "List directory",
		Description: "List the entries of one directory (name, type, size, mode, modification time). Not recursive: use search_files to find files in a tree.",
		Annotations: readOnly(),
	}, d.fsList)
	add(s, d, &mcp.Tool{
		Name:        "fs_stat",
		Title:       "File attributes",
		Description: "Get type, size, mode and modification time of a file or directory, and the target of a symlink (a final symlink is not followed). Use it to check existence or size before reading.",
		Annotations: readOnly(),
	}, d.fsStat)
	add(s, d, &mcp.Tool{
		Name:  "fs_read",
		Title: "Read file",
		Description: "Read a file on the remote host. Text by lines: line_start (1-based) and line_count, with line_numbers for cat -n style prefixes; continue with next_line. " +
			"Text by bytes: offset and length; continue with next_offset (for huge files and logs). Binary: encoding base64 with offset and length. " +
			"Text is converted to UTF-8 (charset auto) and the result reports charset, bom and newline.",
		Annotations: readOnly(),
	}, d.fsRead)
	add(s, d, &mcp.Tool{
		Name:  "fs_edit",
		Title: "Edit file",
		Description: "Replace exact text in an existing file: old_string must occur exactly once (or set replace_all); edits applies several replacements atomically. " +
			"The file keeps its encoding, BOM and line endings (LF in old_string/new_string matches CRLF files). Returns a numbered snippet of the changed region. " +
			"Copy old_string from fs_read output without line number prefixes. Prefer this over fs_write for changing existing files.",
		Annotations: destructive(),
	}, d.fsEdit)
	add(s, d, &mcp.Tool{
		Name:  "fs_write",
		Title: "Write file",
		Description: "Create or overwrite a file with text or base64 content. mode: overwrite (default), append, create (fail if it exists). " +
			"offset writes at a byte position without truncating (chunked or resumed transfers). make_dirs creates parents. " +
			"charset, newline and bom control the text format; by default UTF-8 is written as given. Use fs_edit to change part of an existing file.",
		Annotations: destructive(),
	}, d.fsWrite)
	add(s, d, &mcp.Tool{
		Name:        "fs_mkdir",
		Title:       "Create directory",
		Description: "Create a directory and any missing parents. An existing directory is not an error.",
		Annotations: additive(true),
	}, d.fsMkdir)
	add(s, d, &mcp.Tool{
		Name:  "fs_move",
		Title: "Move or rename",
		Description: "Move or rename a file or directory. destination is the full new path. Fails if destination exists unless overwrite (files only). " +
			"Works across file systems by copying and then deleting.",
		Annotations: destructive(),
	}, d.fsMove)
	add(s, d, &mcp.Tool{
		Name:  "fs_copy",
		Title: "Copy",
		Description: "Copy a file, or a directory tree with recursive. destination is the full path of the copy. Keeps permissions and modification times; symlinks are recreated, not followed. " +
			"Fails if destination exists unless overwrite (replaces files, merges directories).",
		Annotations: destructive(),
	}, d.fsCopy)
	add(s, d, &mcp.Tool{
		Name:        "fs_remove",
		Title:       "Delete",
		Description: "Delete a file, symlink or empty directory; recursive deletes a directory with everything under it. Refuses file system roots and the home directory. missing_ok ignores a missing path.",
		Annotations: destructive(),
	}, d.fsRemove)

	// 検索
	add(s, d, &mcp.Tool{
		Name:  "search_grep",
		Title: "Search file contents",
		Description: "Search file contents recursively with a regular expression (Go RE2: no lookaround or backreferences; literal for plain strings), like ripgrep. " +
			"output_mode: content (lines, with context/before/after lines), files (paths only) or count. include filters files by glob (*.go, src/**/*.ts). " +
			"Skips binary files and .git/.hg/.svn. Files in Shift_JIS, EUC-JP etc. are decoded automatically.",
		Annotations: readOnly(),
	}, d.searchGrep)
	add(s, d, &mcp.Tool{
		Name:  "search_files",
		Title: "Find files",
		Description: "Find files or directories recursively by glob: base name without a slash (*.log, *.{yml,yaml}) or relative path with a slash (**/test/*.py). " +
			"type limits to file or dir; sort mtime lists the most recently modified first.",
		Annotations: readOnly(),
	}, d.searchFiles)
	add(s, d, &mcp.Tool{
		Name:        "fs_manifest",
		Title:       "File hashes",
		Description: "List files under a directory with relative path, size and SHA-256. Compare with a local copy to transfer only files that differ, or verify a transfer.",
		Annotations: readOnly(),
	}, d.fsManifest)

	// 特権実行 (Linux のみ)
	if runtime.GOOS != "windows" {
		add(s, d, &mcp.Tool{
			Name:  "sudo_run",
			Title: "Run as root",
			Description: "Run a shell command with sudo -n (never prompts) as root or user. Needs a NOPASSWD sudoers rule for sh; " +
				"if only specific commands are allowed, use exec_run with argv [\"sudo\", \"-n\", ...] instead.",
			Annotations: destructive(),
		}, d.sudoRun)
	}

	// 環境情報
	add(s, d, &mcp.Tool{
		Name:  "sys_info",
		Title: "Host information",
		Description: "Get OS, architecture, hostname, user, home and working directory, path separator, default shell and default encodings of the remote host. " +
			"Call it first to decide paths, shell syntax and encodings.",
		Annotations: readOnly(),
	}, d.sysInfo)
	add(s, d, &mcp.Tool{
		Name:        "sys_processes",
		Title:       "List processes",
		Description: "List running processes (ps on Linux, tasklist on Windows) as text, optionally filtered by a case-insensitive substring. The header line is always kept.",
		Annotations: readOnly(),
	}, d.sysProcesses)

	registerHelp(s, d)
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

// additive は接続先に追加するだけで、既存の内容を壊さないツールの注釈です。
func additive(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: idempotent, OpenWorldHint: ptr(false)}
}

// destructive は接続先を変更しうるツールの注釈です。エージェント側で承認を必須にする目安になります。
func destructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
}

func ptr[T any](v T) *T { return &v }
