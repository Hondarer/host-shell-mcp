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

// helpFS はエージェント向けの使い方の説明です。topic ごとに 1 ファイルです。
//
//go:embed help/*.md
var helpFS embed.FS

// helpTopics は help の topic の一覧です。表示の順に並べます。
var helpTopics = []string{"overview", "files", "edit", "search", "commands", "jobs", "encoding", "windows", "limits"}

// helpResourcePrefix は help の topic を MCP のリソースとして公開するときの URI の接頭辞です。
const helpResourcePrefix = "host-shell://help/"

// HelpInput は help の入力です。
type HelpInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"overview (default), files, edit, search, commands, jobs, encoding, windows, limits, or all"`
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

// registerHelp は help の各 topic をリソースとしても公開します。
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

// hostSummary は接続先とサーバーの設定を Markdown で返します。
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
	} {
		fmt.Fprintf(&sb, "| %s | `%s` |\n", kv[0], kv[1])
	}
	return strings.TrimSpace(sb.String())
}

// Instructions は初期化の応答でエージェントに渡す案内を返します。
func Instructions(d *Deps) string {
	host, _ := os.Hostname()
	cwd, _ := os.Getwd()
	return fmt.Sprintf(`host-shell executes every operation on the remote host %q (%s/%s), not on your local machine. Paths, programs and environment are those of that host; working directory %q.
Defaults: exec_run shell %q; console encoding %s; file encoding %s; output limit %s per call; command timeout %s (max %s).

How to work:
- Start with sys_info. Find files with search_files (globs such as **/*.py) and contents with search_grep (regex, context lines, output_mode files/count) rather than shell find/grep.
- Read with fs_read: line_start + line_numbers for code, offset/length for large files, encoding base64 for binary.
- Change files with fs_edit (exact old_string -> new_string; keeps encoding, BOM and line endings). Use fs_write for new files or full rewrites, and fs_mkdir/fs_move/fs_copy/fs_remove instead of shell commands.
- Run commands with exec_run (argv avoids quoting issues). Each call is a fresh process: pass cwd and env instead of cd/export. stdin is empty, so prompts fail rather than hang.
- Use job_start/job_read (wait_sec) for anything long-running, and stdin_open + job_write for programs that need input.
- All text is UTF-8; host encodings are converted automatically. Results larger than the limit are truncated and flagged; narrow the request.
Call the help tool (topics: %s) for details and examples.`,
		host, runtime.GOOS, runtime.GOARCH, cwd,
		d.DefaultShell, textenc.Console().Name, textenc.File().Name,
		formatBytes(d.Cfg.MaxOutputBytes), d.Cfg.DefaultTimeout, d.Cfg.MaxTimeout,
		strings.Join(helpTopics, ", "))
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
