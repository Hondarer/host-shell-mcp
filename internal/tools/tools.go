// Package tools defines the MCP tools host-shell provides.
package tools

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/jobs"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// Deps are the dependencies shared by the tool implementations.
type Deps struct {
	Cfg          config.Config
	Audit        *audit.Logger
	Jobs         *jobs.Manager
	DefaultShell string
	// Policy holds the restrictions the user specified at startup. nil means no restriction.
	Policy *policy.Policy

	// enabled maps the exposed tool names to their groups. Used for help and the instructions.
	enabled map[string]string
	// catalog is used only when Catalog builds the list. Nothing is registered then.
	catalog map[string]string

	// confirmKey signs the binding between the user's confirmation answer and the call's arguments.
	keyOnce    sync.Once
	confirmKey []byte
}

// Tool groups. --tools selects which groups are exposed.
const (
	groupCore   = "core"   // help_get, sys_get_info (always exposed)
	groupFiles  = "files"  // reading, writing and managing files
	groupSearch = "search" // searching files and their contents
	groupExec   = "exec"   // running commands and jobs
	groupNet    = "net"    // network
	groupAdmin  = "admin"  // processes, services, logs, disks, registry
)

// Groups are the group names and descriptions.
var Groups = [][2]string{
	{groupFiles, "fs_* file reading, writing and file operations"},
	{groupSearch, "search_grep, search_find"},
	{groupExec, "exec_run, exec_sudo and job_* (commands, background jobs, pseudo terminals)"},
	{groupNet, "net_* HTTP requests, TCP connections, reachability, listening ports, interfaces"},
	{groupAdmin, "processes, services, system logs, disks, Windows registry"},
}

// readOnlyCapable are tools exposed under --read-only too, limited to uses that change nothing.
var readOnlyCapable = map[string]bool{"net_request_http": true, "net_exchange": true}

// Catalog returns the names and groups of all tools, including OS-specific ones.
func Catalog() map[string]string {
	d := &Deps{catalog: map[string]string{}}
	Register(nil, d)
	return d.catalog
}

// Enabled reports whether the tool is exposed.
func (d *Deps) Enabled(name string) bool {
	_, ok := d.enabled[name]
	return ok
}

// groupEnabled reports whether any tool of the group is exposed.
func (d *Deps) groupEnabled(group string) bool {
	for _, g := range d.enabled {
		if g == group {
			return true
		}
	}
	return false
}

// onOS reports whether to register an OS-specific tool. Building the catalog includes all of them.
func (d *Deps) onOS(goos string) bool {
	return d.catalog != nil || runtime.GOOS == goos
}

// Register registers all tools with the server.
//
// The descriptions are read by the agent. They say what a tool does, when to use it and what to combine it with.
func Register(s *mcp.Server, d *Deps) {
	if d.Policy == nil {
		d.Policy = policy.Unrestricted()
	}
	d.enabled = map[string]string{}

	// Usage
	add(s, d, groupCore, &mcp.Tool{
		Name:  "help_get",
		Title: "Usage guide",
		Description: "Return usage guidance for host-shell: tool selection, argument details, examples, and active policy restrictions. " +
			"Topics: " + strings.Join(helpTopics, ", ") + ", all.",
		Annotations: readOnly(),
	}, d.helpGet)

	// Running commands
	add(s, d, groupExec, &mcp.Tool{
		Name:  "exec_run",
		Title: "Run command",
		Description: "Run a command on the remote host and wait for completion (up to timeout_sec), returning exit_code, stdout and stderr. " +
			"Pass command for a shell command line (default_shell in sys_get_info) or argv to run a program directly without a shell. " +
			"Each call is a separate process (pass cwd and env; cd/export do not persist) with closed stdin unless stdin is set. " +
			"Streams progress notifications and kills the process tree on cancellation. " +
			"Use job_start for long-running, interactive, or TTY commands; prefer fs_read/fs_edit/fs_write for file operations.",
		Annotations: destructive(),
	}, d.execRun)

	// Long-running commands
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_start",
		Title: "Start background job",
		Description: "Start a background command and return its job id immediately (same arguments as exec_run; no timeout unless timeout_sec is set). " +
			"Use for builds, test suites, servers, or streaming commands; read output with job_read (with wait_sec). " +
			"Set stdin_open to send input via job_write, or pty to run in a pseudo terminal (for password prompts, REPLs, ssh, top).",
		Annotations: destructive(),
	}, d.jobStart)
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_read",
		Title: "Read job output",
		Description: "Read interleaved stdout/stderr of a background job from a byte offset and return its status (running, exit_code). " +
			"Start at offset 0 and pass next_offset on subsequent calls (or a negative offset for the tail). " +
			"Set wait_sec (with optional until regex or idle_ms) to wait for output instead of polling. " +
			"Strips ANSI escape sequences from pty jobs unless raw is set.",
		Annotations: readOnly(),
	}, d.jobRead)
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_write",
		Title: "Send input to job",
		Description: "Write to stdin of a job started with stdin_open or pty, close stdin (close: true), or resize a pty (cols, rows). " +
			"Include \\n (or \\r in pty mode) to submit a line; pty jobs accept control sequences (\\u0003 Ctrl-C, \\u0004 Ctrl-D, \\u001b Esc). " +
			"Follow with job_read (using wait_sec and until/idle_ms) to read the response.",
		Annotations: destructive(),
	}, d.jobWrite)
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_stop",
		Title: "Stop job",
		Description: "Stop or signal a background job: kill (default) terminates the process tree; Linux also supports process-group signals (interrupt, terminate, hangup, ...); " +
			"in pty jobs interrupt sends Ctrl-C. Waits up to wait_sec and returns job status.",
		Annotations: destructive(),
	}, d.jobStop)
	add(s, d, groupExec, &mcp.Tool{
		Name:        "job_list",
		Title:       "List jobs",
		Description: "List background jobs in this session with their status (retains the 50 most recent finished jobs; all jobs terminate when the session ends).",
		Annotations: readOnly(),
	}, d.jobList)

	// Running as root (not on Windows)
	if d.catalog != nil || runtime.GOOS != "windows" {
		add(s, d, groupExec, &mcp.Tool{
			Name:  "exec_sudo",
			Title: "Run as root",
			Description: "Run a shell command via sudo as root or user. Without password, uses sudo -n (requires NOPASSWD for sh; for command-specific sudoers rules, use exec_run with argv [\"sudo\", \"-n\", ...]). " +
				"When password is provided by the user, passes it to sudo via askpass without exposing it to the command, logs, or output.",
			Annotations: destructive(),
		}, d.execSudo)
	}

	// File operations
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_list",
		Title:       "List directory",
		Description: "List entries in a single directory (name, type, size, mode, mod_time). Non-recursive; use search_find to search directory trees.",
		Annotations: readOnly(),
	}, d.fsList)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_stat",
		Title:       "File attributes",
		Description: "Get type, size, mode, mod_time and symlink target of a path without following a final symlink.",
		Annotations: readOnly(),
	}, d.fsStat)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_read",
		Title: "Read file",
		Description: "Read a file on the remote host. Line mode: line_start (1-based), line_count and optional line_numbers (continue with next_line). " +
			"Byte mode: offset and length (continue with next_offset). Binary mode: encoding base64 with offset and length. " +
			"Text mode decodes to UTF-8 (charset auto by default) and reports charset, bom and newline.",
		Annotations: readOnly(),
	}, d.fsRead)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_edit",
		Title: "Edit file",
		Description: "Replace exact text in an existing file (prefer over fs_write for partial edits). old_string must match once unless replace_all is set; edits applies multiple replacements atomically. " +
			"Preserves file encoding, BOM and line endings (LF in inputs matches CRLF files) and returns a numbered diff snippet. " +
			"Copy old_string from fs_read without line number prefixes.",
		Annotations: destructive(),
	}, d.fsEdit)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_write",
		Title: "Write file",
		Description: "Create, overwrite, or update a file with text or base64 content. mode: overwrite (default), append, create. " +
			"offset writes at a byte position without truncating; make_dirs creates parent directories; perm sets octal permissions; atomic renames a temp file over the target. " +
			"charset, newline and bom control text encoding (default UTF-8). Use fs_edit for partial edits to existing files.",
		Annotations: destructive(),
	}, d.fsWrite)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_mkdir",
		Title:       "Create directory",
		Description: "Create a directory and any missing parent directories (no-op if the directory already exists).",
		Annotations: additive(true),
	}, d.fsMkdir)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_move",
		Title: "Move or rename",
		Description: "Move or rename a file or directory to destination (full target path). Fails if destination exists unless overwrite is set (files only). " +
			"Falls back to copy-and-delete across file systems.",
		Annotations: destructive(),
	}, d.fsMove)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_copy",
		Title: "Copy",
		Description: "Copy a file or directory tree (with recursive) to destination (full target path), preserving permissions, mtimes and symlinks. " +
			"Fails if destination exists unless overwrite is set (replaces files, merges directories).",
		Annotations: destructive(),
	}, d.fsCopy)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_remove",
		Title:       "Delete",
		Description: "Delete a file, symlink, empty directory, or directory tree (with recursive). Refuses filesystem roots and home directory; missing_ok ignores nonexistent paths.",
		Annotations: destructive(),
	}, d.fsRemove)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_manifest",
		Title:       "File hashes",
		Description: "List regular files under a path with relative path, size, octal mode and SHA-256 hash (for incremental sync or verification).",
		Annotations: readOnly(),
	}, d.fsManifest)

	// Search
	add(s, d, groupSearch, &mcp.Tool{
		Name:  "search_grep",
		Title: "Search file contents",
		Description: "Search file contents recursively using a Go RE2 regex (or literal string). " +
			"output_mode: content (matching lines with optional context/before/after), files (paths only) or count. " +
			"Filters by include glob and gitignore, skips binary files and .git/.hg/.svn, and auto-decodes non-UTF-8 text encodings.",
		Annotations: readOnly(),
	}, d.searchGrep)
	add(s, d, groupSearch, &mcp.Tool{
		Name:  "search_find",
		Title: "Find files",
		Description: "Find files or directories recursively by glob (base name without '/', or relative path with '/'). " +
			"Filter by type (file, dir) and sort by path or mtime (newest first).",
		Annotations: readOnly(),
	}, d.searchFind)

	registerNet(s, d)
	registerAdmin(s, d)

	// Environment information
	add(s, d, groupCore, &mcp.Tool{
		Name:  "sys_get_info",
		Title: "Host information",
		Description: "Get remote host OS, architecture, hostname, user, home/working directories, path separator, default shell, default encodings, " +
			"enabled tool groups and active policy restrictions. Call first to determine paths, shell syntax and encodings.",
		Annotations: readOnly(),
	}, d.sysGetInfo)

	if s != nil {
		registerHelp(s, d)
	}
}

// add registers a tool wrapped with the policy check, user confirmation, redaction and the audit log.
// Tools the user chose not to expose are not registered.
func add[In, Out any](s *mcp.Server, d *Deps, group string, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if d.catalog != nil {
		d.catalog[t.Name] = group
		return
	}
	ro := t.Annotations != nil && t.Annotations.ReadOnlyHint
	if group != groupCore {
		if !d.Policy.Enabled(t.Name, group, ro) {
			return
		}
		if d.Policy.ReadOnly && !ro && !readOnlyCapable[t.Name] {
			return
		}
	}
	d.enabled[t.Name] = group
	confirm := d.Policy.NeedsConfirm(t.Name, group, ro)
	mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		var (
			res *mcp.CallToolResult
			out Out
			err = d.checkPolicy(in)
		)
		if err == nil && confirm {
			var ask *mcp.CallToolResult
			if ask, err = d.confirm(req, t.Name, in); ask != nil {
				// Ask the user to confirm. The retry carrying the answer comes back here.
				d.Audit.Tool(t.Name+" (confirmation requested)", in, time.Since(start), false, nil)
				return ask, out, nil
			}
		}
		if err == nil {
			res, out, err = h(ctx, req, in)
			if d.Policy.Redacting() {
				res, out = redactOutput(d, res, out)
			}
		}
		if err != nil && d.Cfg.Proxy != nil {
			err = fmt.Errorf("%s", d.Cfg.Proxy.Mask(err.Error()))
		}
		if err == nil && ctx.Err() != nil {
			err = fmt.Errorf("cancelled: %w", ctx.Err())
		}
		d.Audit.Tool(t.Name, in, time.Since(start), res != nil && res.IsError, err)
		return res, out, err
	})
}

// readOnly annotates tools that do not change the target.
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
}

// additive annotates tools that only add to the target without destroying existing content.
func additive(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: idempotent, OpenWorldHint: ptr(false)}
}

// destructive annotates tools that may change the target. Agents can use it to require approval.
func destructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
}

func ptr[T any](v T) *T { return &v }

// network annotates tools that may act beyond the target (across the network).
func network(readOnlyHint bool) *mcp.ToolAnnotations {
	a := &mcp.ToolAnnotations{ReadOnlyHint: readOnlyHint, OpenWorldHint: ptr(true)}
	if !readOnlyHint {
		a.DestructiveHint = ptr(true)
	}
	return a
}
