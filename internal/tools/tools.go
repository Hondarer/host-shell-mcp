// Package tools defines the MCP tools host-shell provides.
package tools

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/forward"
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
	// Proxy is the client proxy target when --client-proxy is set, else nil.
	Proxy *clientproxy.Target
	// Forward is the port forwarding target when forwarding is permitted, else nil.
	Forward *forward.Target

	// enabled maps the exposed tool names to their groups. Used for help and the instructions.
	enabled map[string]string

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

// groups are the group names and descriptions.
var groups = [][2]string{
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
	r := definitions(&Deps{}, true)
	catalog := make(map[string]string, len(r.tools))
	for _, def := range r.tools {
		catalog[def.tool.Name] = def.group
	}
	return catalog
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

// Registry is the selected set of tools, ready to register after Instructions is built.
type Registry struct {
	d     *Deps
	allOS bool
	tools []toolDefinition
}

type toolDefinition struct {
	tool     *mcp.Tool
	group    string
	register func(*mcp.Server)
}

func (r *Registry) onOS(goos string) bool {
	return r.allOS || runtime.GOOS == goos
}

// Prepare selects tools and populates the metadata used by help and Instructions.
// It does not register tools or resources with an MCP server.
func Prepare(d *Deps) *Registry {
	if d.Policy == nil {
		d.Policy = policy.Unrestricted()
	}
	r := definitions(d, false)
	d.enabled = make(map[string]string)
	selected := r.tools[:0]
	for _, def := range r.tools {
		ro := def.tool.Annotations != nil && def.tool.Annotations.ReadOnlyHint
		if def.group != groupCore {
			if !d.Policy.Enabled(policy.Tool{Name: def.tool.Name, Group: def.group, ReadOnly: ro}) ||
				(d.Policy.ReadOnly && !ro && !readOnlyCapable[def.tool.Name]) {
				continue
			}
		}
		d.enabled[def.tool.Name] = def.group
		selected = append(selected, def)
	}
	r.tools = selected
	return r
}

// Register installs the prepared tools and help resources.
func (r *Registry) Register(s *mcp.Server) {
	for _, def := range r.tools {
		def.register(s)
	}
	registerHelp(s, r.d)
}

// definitions collects tool descriptions and their typed handlers in listing order; each group
// registers its tools in the file that implements them.
func definitions(d *Deps, allOS bool) *Registry {
	r := &Registry{d: d, allOS: allOS}
	// Usage
	add(r, groupCore, &mcp.Tool{
		Name:  "help_get",
		Title: "Usage guide",
		Description: "Return usage guidance for host-shell: tool selection, argument details, examples, and active policy restrictions. " +
			"Topics: " + strings.Join(helpTopics, ", ") + ", all.",
		Annotations: readOnly(),
	}, d.helpGet)

	registerExec(r, d)
	registerFiles(r, d)
	registerSearch(r, d)
	registerNet(r, d)
	registerAdmin(r, d)

	// Environment information
	add(r, groupCore, &mcp.Tool{
		Name:  "sys_get_info",
		Title: "Host information",
		Description: "Get remote host OS, architecture, hostname, user, home/working directories, path separator, default shell, default encodings, " +
			"enabled tool groups and active policy restrictions. Call first to determine paths, shell syntax and encodings.",
		Annotations: readOnly(),
	}, d.sysGetInfo)

	return r
}

// add collects a typed tool definition without registering it with the SDK.
func add[In, Out any](r *Registry, group string, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	r.tools = append(r.tools, toolDefinition{tool: t, group: group, register: func(s *mcp.Server) {
		registerTool(s, r.d, group, t, h)
	}})
}

// registerTool wraps a handler with policy, confirmation, redaction and audit logging.
func registerTool[In, Out any](s *mcp.Server, d *Deps, group string, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	ro := t.Annotations != nil && t.Annotations.ReadOnlyHint
	confirm := d.Policy.NeedsConfirm(policy.Tool{Name: t.Name, Group: group, ReadOnly: ro})
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
		if err != nil && d.Proxy != nil {
			err = errors.New(d.mask(err.Error()))
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
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
}

// additive annotates tools that only add to the target without destroying existing content.
func additive(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: idempotent, OpenWorldHint: new(false)}
}

// destructive annotates tools that may change the target. Agents can use it to require approval.
func destructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(false)}
}

// network annotates tools that may act beyond the target (across the network).
func network(readOnlyHint bool) *mcp.ToolAnnotations {
	a := &mcp.ToolAnnotations{ReadOnlyHint: readOnlyHint, OpenWorldHint: new(true)}
	if !readOnlyHint {
		a.DestructiveHint = new(true)
	}
	return a
}
