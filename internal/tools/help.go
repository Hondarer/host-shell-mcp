package tools

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// helpFS holds the usage guides for agents, one file per topic.
//
//go:embed help/*.md
var helpFS embed.FS

// helpTopics lists the help topics in display order.
var helpTopics = []string{"overview", "files", "edit", "search", "commands", "jobs", "network", "admin", "transfer", "encoding", "windows", "policy", "limits"}

// helpResourcePrefix is the URI prefix under which help topics are exposed as MCP resources.
const helpResourcePrefix = "host-shell://help/"

// HelpInput is the input of help.
type HelpInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"overview (default), files, edit, search, commands, jobs, network, admin, transfer, encoding, windows, policy, limits, or all"`
}

func (d *Deps) help(_ context.Context, _ *mcp.CallToolRequest, in HelpInput) (*mcp.CallToolResult, any, error) {
	topic := orDefault(in.Topic, "overview")
	var text string
	switch {
	case topic == "all":
		parts := []string{d.hostSummary()}
		for _, t := range helpTopics {
			parts = append(parts, helpText(t))
		}
		text = strings.Join(parts, "\n\n")
	case slices.Contains(helpTopics, topic):
		text = helpText(topic)
		if topic == "overview" || topic == "limits" {
			text = d.hostSummary() + "\n\n" + text
		}
	default:
		return nil, nil, fmt.Errorf("unknown topic %q (use one of: %s, all)", topic, strings.Join(helpTopics, ", "))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func helpText(topic string) string {
	b, err := helpFS.ReadFile("help/" + topic + ".md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// registerHelp also exposes each help topic as a resource.
func registerHelp(s *mcp.Server, d *Deps) {
	for _, t := range helpTopics {
		uri := helpResourcePrefix + t
		title := strings.TrimPrefix(strings.SplitN(helpText(t), "\n", 2)[0], "# ")
		s.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        "help-" + t,
			Title:       title,
			Description: "host-shell usage guide: " + title,
			MIMEType:    "text/markdown",
		}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			text := helpText(t)
			if t == "overview" || t == "limits" {
				text = d.hostSummary() + "\n\n" + text
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: text}}}, nil
		})
	}
}

// hostSummary returns the target and server settings as Markdown.
func (d *Deps) hostSummary() string {
	host, _ := os.Hostname()
	cwd, _ := os.Getwd()
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	var sb strings.Builder
	sb.WriteString("# This host\n\n")
	fmt.Fprintf(&sb, "| Item | Value |\n|---|---|\n")
	for _, kv := range [][2]string{
		{"Host", fmt.Sprintf("%s (%s/%s)", host, runtime.GOOS, runtime.GOARCH)},
		{"User", name},
		{"Working directory", cwd},
		{"Default shell for exec_run", d.DefaultShell},
		{"Console encoding (commands)", textenc.Console().Name},
		{"File encoding (system)", textenc.File().Name},
		{"Output limit per call", formatBytes(d.Cfg.MaxOutputBytes)},
		{"Command timeout (default / max)", fmt.Sprintf("%s / %s", d.Cfg.DefaultTimeout, d.Cfg.MaxTimeout)},
		{"Tool groups", strings.Join(d.enabledGroups(), ", ")},
	} {
		fmt.Fprintf(&sb, "| %s | `%s` |\n", kv[0], kv[1])
	}
	if r := d.Policy.Summary(); len(r) > 0 {
		sb.WriteString("\n## Restrictions\n\n| Rule | Value |\n|---|---|\n")
		for _, kv := range r {
			fmt.Fprintf(&sb, "| %s | `%s` |\n", kv[0], kv[1])
		}
	} else {
		sb.WriteString("\nNo restrictions are set: every tool can do whatever the host user can.\n")
	}
	return strings.TrimSpace(sb.String())
}

// Instructions returns the guidance given to the agent in the initialize response, covering only the exposed tools.
func Instructions(d *Deps) string {
	host, _ := os.Hostname()
	cwd, _ := os.Getwd()
	var sb strings.Builder
	fmt.Fprintf(&sb, "host-shell executes every operation on the remote host %q (%s/%s), not on your local machine. Paths, programs, network and environment are those of that host; working directory %q.\n",
		host, runtime.GOOS, runtime.GOARCH, cwd)
	fmt.Fprintf(&sb, "Defaults: exec_run shell %q; console encoding %s; file encoding %s; output limit %s per call; command timeout %s (max %s).\n",
		d.DefaultShell, textenc.Console().Name, textenc.File().Name,
		formatBytes(d.Cfg.MaxOutputBytes), d.Cfg.DefaultTimeout, d.Cfg.MaxTimeout)
	sb.WriteString("\nHow to work:\n- Start with sys_info.\n")
	for _, l := range []struct {
		tool, text string
	}{
		{"search_grep", "Find files with search_files (globs such as **/*.py) and contents with search_grep (regex, context lines, output_mode files/count) rather than shell find/grep."},
		{"fs_read", "Read with fs_read: line_start + line_numbers for code, offset/length for large files (negative offset in job_read for tails), encoding base64 for binary."},
		{"fs_edit", "Change files with fs_edit (exact old_string -> new_string; keeps encoding, BOM and line endings). Use fs_write for new files or full rewrites, and fs_mkdir/fs_move/fs_copy/fs_remove instead of shell commands."},
		{"exec_run", "Run commands with exec_run (argv avoids quoting issues). Each call is a fresh process: pass cwd and env instead of cd/export. stdin is empty, so prompts fail rather than hang."},
		{"job_start", "Use job_start/job_read (wait_sec with until or idle_ms) for anything long-running; stdin_open + job_write for programs that need input; pty for programs that need a terminal (password prompts, REPLs, ssh)."},
		{"net_http", "Reach services from the host itself with net_http (e.g. http://localhost:8080; unix_socket for Docker), net_tcp for other protocols and net_check for DNS/ports/certificates. No port forwarding is needed."},
		{"svc_control", "Inspect and control services with svc_list/svc_control, logs with log_query, processes with sys_processes/proc_kill, disks with sys_disks."},
	} {
		if d.Enabled(l.tool) {
			sb.WriteString("- " + l.text + "\n")
		}
	}
	sb.WriteString("- All text is UTF-8; host encodings are converted automatically. Results larger than the limit are truncated and flagged; narrow the request.\n")
	if r := d.Policy.Summary(); len(r) > 0 {
		sb.WriteString("\nRestrictions set by the administrator (calls outside them fail with a policy: error; do not work around them):\n")
		for _, kv := range r {
			fmt.Fprintf(&sb, "- %s: %s\n", kv[0], kv[1])
		}
	}
	fmt.Fprintf(&sb, "\nCall the help tool (topics: %s) for details and examples.", strings.Join(helpTopics, ", "))
	return sb.String()
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
