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

// helpFS embeds the per-topic agent usage guides.
//
//go:embed help/*.md
var helpFS embed.FS

// helpTopics lists the help topics in display order.
var helpTopics = []string{"overview", "files", "edit", "search", "commands", "jobs", "network", "admin", "transfer", "encoding", "windows", "policy", "limits"}

// helpResourcePrefix is the MCP resource URI prefix for help topics.
const helpResourcePrefix = "host-shell://help/"

// HelpInput defines arguments for the help tool.
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

// registerHelp registers each help topic as an MCP resource.
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

// hostSummary formats the target host and server configuration as Markdown.
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
		sb.WriteString("\nNo policy restrictions are set; tools operate with the full permissions of the host user.\n")
	}
	return strings.TrimSpace(sb.String())
}

// Instructions builds the MCP initialize instructions for the enabled tools.
func Instructions(d *Deps) string {
	host, _ := os.Hostname()
	cwd, _ := os.Getwd()
	var sb strings.Builder
	fmt.Fprintf(&sb, "host-shell executes all operations on remote host %q (%s/%s; working directory %q). Paths, programs, network, and environment belong to that host; local files are not accessible.\n",
		host, runtime.GOOS, runtime.GOARCH, cwd)
	fmt.Fprintf(&sb, "Defaults: exec_run shell %q; console encoding %s; file encoding %s; output limit %s per call; command timeout %s (max %s).\n",
		d.DefaultShell, textenc.Console().Name, textenc.File().Name,
		formatBytes(d.Cfg.MaxOutputBytes), d.Cfg.DefaultTimeout, d.Cfg.MaxTimeout)
	sb.WriteString("\nWorkflow:\n- Call sys_info first.\n")
	for _, l := range []struct {
		tool, text string
	}{
		{"search_grep", "Locate files with search_files (globs such as **/*.py) and search content with search_grep (regex, context lines, output_mode files/count) instead of shell find/grep."},
		{"fs_read", "Read files with fs_read: line_start + line_numbers for source code, offset/length for large files (negative offset in job_read for tails), and encoding base64 for binary data."},
		{"fs_edit", "Edit files with fs_edit (exact old_string -> new_string; preserves encoding, BOM, and line endings). Use fs_write for new files or full rewrites, and fs_mkdir/fs_move/fs_copy/fs_remove instead of shell commands."},
		{"exec_run", "Run commands with exec_run (pass argv to avoid shell quoting). Each call spawns a new process: pass cwd and env instead of cd/export. stdin is closed unless provided, so interactive prompts fail immediately."},
		{"job_start", "Use job_start/job_read (wait_sec with until or idle_ms) for long-running commands; stdin_open + job_write for piped input; pty for terminal-dependent programs (password prompts, REPLs, ssh)."},
		{"net_http", "Access host-reachable services directly with net_http (e.g. http://localhost:8080; unix_socket for Docker), net_tcp for raw TCP/TLS, and net_check for DNS/ports/certificates without port forwarding."},
		{"svc_control", "Manage services with svc_list/svc_control, query logs with log_query, inspect or signal processes with sys_processes/proc_kill, and check disk usage with sys_disks."},
	} {
		if d.Enabled(l.tool) {
			sb.WriteString("- " + l.text + "\n")
		}
	}
	sb.WriteString("- All text is UTF-8; host encodings are converted automatically. Results exceeding the output limit are truncated and flagged; narrow the request.\n")
	if r := d.Policy.Summary(); len(r) > 0 {
		sb.WriteString("\nActive policy restrictions (disallowed calls fail with a policy: error; do not attempt workarounds):\n")
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
