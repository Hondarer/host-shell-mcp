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
	groupCore   = "core"   // help, sys_info (always exposed)
	groupFiles  = "files"  // reading, writing and managing files
	groupSearch = "search" // searching files and their contents
	groupExec   = "exec"   // running commands and jobs
	groupNet    = "net"    // network
	groupAdmin  = "admin"  // processes, services, logs, disks, registry
)

// Groups are the group names and descriptions.
var Groups = [][2]string{
	{groupFiles, "fs_* file reading, writing and file operations"},
	{groupSearch, "search_grep, search_files"},
	{groupExec, "exec_run, sudo_run and job_* (commands, background jobs, pseudo terminals)"},
	{groupNet, "net_* HTTP requests, TCP connections, reachability, listening ports, interfaces"},
	{groupAdmin, "processes, services, system logs, disks, Windows registry"},
}

// readOnlyCapable are tools exposed under --read-only too, limited to uses that change nothing.
var readOnlyCapable = map[string]bool{"net_http": true, "net_tcp": true}

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
		Name:  "help",
		Title: "Usage guide",
		Description: "Explain how to use host-shell: workflow, tool choice, arguments and examples, and the restrictions in effect. " +
			"Call it when unsure which tool to use or how an argument works. Topics: " + strings.Join(helpTopics, ", ") + ", all.",
		Annotations: readOnly(),
	}, d.help)

	// Running commands
	add(s, d, groupExec, &mcp.Tool{
		Name:  "exec_run",
		Title: "Run command",
		Description: "Run a command on the remote host and wait for it (up to timeout_sec). Returns exit_code, stdout, stderr. " +
			"Use command for a shell command line (default shell in sys_info) or argv to run a program with exact arguments and no shell. " +
			"Each call is a new process: pass cwd and env; cd/export do not persist. stdin is empty unless given, so prompts fail instead of hanging. " +
			"Sends progress notifications while running and kills the process tree if the request is cancelled. " +
			"For commands that may run longer than a minute, need input while running or need a terminal, use job_start. To read or change files prefer fs_read/fs_edit/fs_write.",
		Annotations: destructive(),
	}, d.execRun)

	// Long-running commands
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_start",
		Title: "Start background job",
		Description: "Start a command in the background and return its id at once. Same arguments as exec_run; no time limit unless timeout_sec. " +
			"Use for builds, test suites, servers, tail -f and other long or streaming commands. Read output with job_read (wait_sec to wait). " +
			"Set stdin_open to send input later with job_write, or pty to run it in a pseudo terminal (password prompts, REPLs, ssh, programs that need a terminal).",
		Annotations: destructive(),
	}, d.jobStart)
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_read",
		Title: "Read job output",
		Description: "Read a background job's output (stdout and stderr interleaved) from a byte offset, with its status (running, exit_code). " +
			"Start at offset 0, then pass next_offset to get only new output; a negative offset reads the tail. " +
			"Set wait_sec to wait for output instead of polling, with until (regex, e.g. a prompt) or idle_ms (output went quiet) to wait for the right moment. " +
			"Terminal escape sequences of pty jobs are removed unless raw.",
		Annotations: readOnly(),
	}, d.jobRead)
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_write",
		Title: "Send input to job",
		Description: "Send text to the input of a job started with stdin_open or pty, and/or close it (close: true, like Ctrl-D). " +
			"Include \\n to submit a line (\\r in some pty programs). In a pty job control keys work: \\u0003 Ctrl-C, \\u0004 Ctrl-D, \\u001b Esc. " +
			"cols and rows resize a pty. Follow with job_read (wait_sec with until or idle_ms) to see the response.",
		Annotations: destructive(),
	}, d.jobWrite)
	add(s, d, groupExec, &mcp.Tool{
		Name:  "job_stop",
		Title: "Stop job",
		Description: "Stop a background job: kill (default) kills its whole process tree; on Linux interrupt, terminate, hangup and other signals go to its process group; " +
			"in a pty job interrupt sends Ctrl-C. Returns the status after waiting up to wait_sec.",
		Annotations: destructive(),
	}, d.jobStop)
	add(s, d, groupExec, &mcp.Tool{
		Name:        "job_list",
		Title:       "List jobs",
		Description: "List background jobs of this session with their status. Jobs end when the session ends; the 50 most recent finished jobs are kept.",
		Annotations: readOnly(),
	}, d.jobList)

	// Running as root (not on Windows)
	if d.catalog != nil || runtime.GOOS != "windows" {
		add(s, d, groupExec, &mcp.Tool{
			Name:  "sudo_run",
			Title: "Run as root",
			Description: "Run a shell command with sudo -n (never prompts) as root or user. Needs a NOPASSWD sudoers rule for sh; " +
				"if only specific commands are allowed, use exec_run with argv [\"sudo\", \"-n\", ...] instead. " +
				"If sudo needs a password, run sudo in a pty job (job_start pty) and type it with job_write.",
			Annotations: destructive(),
		}, d.sudoRun)
	}

	// File operations
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_list",
		Title:       "List directory",
		Description: "List the entries of one directory (name, type, size, mode, modification time). Not recursive: use search_files to find files in a tree.",
		Annotations: readOnly(),
	}, d.fsList)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_stat",
		Title:       "File attributes",
		Description: "Get type, size, mode and modification time of a file or directory, and the target of a symlink (a final symlink is not followed). Use it to check existence or size before reading.",
		Annotations: readOnly(),
	}, d.fsStat)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_read",
		Title: "Read file",
		Description: "Read a file on the remote host. Text by lines: line_start (1-based) and line_count, with line_numbers for cat -n style prefixes; continue with next_line. " +
			"Text by bytes: offset and length; continue with next_offset (for huge files and logs). Binary: encoding base64 with offset and length. " +
			"Text is converted to UTF-8 (charset auto) and the result reports charset, bom and newline.",
		Annotations: readOnly(),
	}, d.fsRead)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_edit",
		Title: "Edit file",
		Description: "Replace exact text in an existing file: old_string must occur exactly once (or set replace_all); edits applies several replacements atomically. " +
			"The file keeps its encoding, BOM and line endings (LF in old_string/new_string matches CRLF files). Returns a numbered snippet of the changed region. " +
			"Copy old_string from fs_read output without line number prefixes. Prefer this over fs_write for changing existing files.",
		Annotations: destructive(),
	}, d.fsEdit)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_write",
		Title: "Write file",
		Description: "Create or overwrite a file with text or base64 content. mode: overwrite (default), append, create (fail if it exists). " +
			"offset writes at a byte position without truncating (chunked or resumed transfers). make_dirs creates parents; perm sets permissions such as 0755. " +
			"charset, newline and bom control the text format; by default UTF-8 is written as given. Use fs_edit to change part of an existing file.",
		Annotations: destructive(),
	}, d.fsWrite)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_mkdir",
		Title:       "Create directory",
		Description: "Create a directory and any missing parents. An existing directory is not an error.",
		Annotations: additive(true),
	}, d.fsMkdir)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_move",
		Title: "Move or rename",
		Description: "Move or rename a file or directory. destination is the full new path. Fails if destination exists unless overwrite (files only). " +
			"Works across file systems by copying and then deleting.",
		Annotations: destructive(),
	}, d.fsMove)
	add(s, d, groupFiles, &mcp.Tool{
		Name:  "fs_copy",
		Title: "Copy",
		Description: "Copy a file, or a directory tree with recursive. destination is the full path of the copy. Keeps permissions and modification times; symlinks are recreated, not followed. " +
			"Fails if destination exists unless overwrite (replaces files, merges directories).",
		Annotations: destructive(),
	}, d.fsCopy)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_remove",
		Title:       "Delete",
		Description: "Delete a file, symlink or empty directory; recursive deletes a directory with everything under it. Refuses file system roots and the home directory. missing_ok ignores a missing path.",
		Annotations: destructive(),
	}, d.fsRemove)
	add(s, d, groupFiles, &mcp.Tool{
		Name:        "fs_manifest",
		Title:       "File hashes",
		Description: "List files under a directory (or one file) with relative path, size, permissions and SHA-256. Compare with a local copy to transfer only files that differ, or verify a transfer.",
		Annotations: readOnly(),
	}, d.fsManifest)

	// Search
	add(s, d, groupSearch, &mcp.Tool{
		Name:  "search_grep",
		Title: "Search file contents",
		Description: "Search file contents recursively with a regular expression (Go RE2: no lookaround or backreferences; literal for plain strings), like ripgrep. " +
			"output_mode: content (lines, with context/before/after lines), files (paths only) or count. include filters files by glob (*.go, src/**/*.ts). " +
			"Skips binary files and .git/.hg/.svn. Files in Shift_JIS, EUC-JP etc. are decoded automatically.",
		Annotations: readOnly(),
	}, d.searchGrep)
	add(s, d, groupSearch, &mcp.Tool{
		Name:  "search_files",
		Title: "Find files",
		Description: "Find files or directories recursively by glob: base name without a slash (*.log, *.{yml,yaml}) or relative path with a slash (**/test/*.py). " +
			"type limits to file or dir; sort mtime lists the most recently modified first.",
		Annotations: readOnly(),
	}, d.searchFiles)

	registerNet(s, d)
	registerAdmin(s, d)

	// Environment information
	add(s, d, groupCore, &mcp.Tool{
		Name:  "sys_info",
		Title: "Host information",
		Description: "Get OS, architecture, hostname, user, home and working directory, path separator, default shell, default encodings, " +
			"enabled tool groups and restrictions of the remote host. Call it first to decide paths, shell syntax and encodings.",
		Annotations: readOnly(),
	}, d.sysInfo)

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
