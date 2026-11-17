package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/session"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

// Exit codes of host-shell call and host-shell tools.
const (
	exitOK          = 0
	exitToolError   = 1   // the tool reported an error
	exitUsage       = 2   // wrong usage, including an unknown tool or argument
	exitSession     = 3   // no session, or the session or target connection was lost
	exitInterrupted = 130 // interrupted; the call was cancelled on the target
)

const callUsage = `usage: host-shell call TOOL [--session NAME] [--json FILE] [--quiet] [KEY=VALUE ...]

Call a tool of a running host-shell session (see host-shell session --help) and print the
tool's JSON result on one line. TOOL comes first, so that agent permission rules can allow
"host-shell call fs_read" separately from other tools. List the tools with host-shell tools.

Arguments:
  KEY=VALUE     converted to the type of KEY in the tool's input schema: strings as they are,
                integers, numbers and true/false to their types, arrays and objects as JSON.
                For an array of strings, repeat KEY=VALUE to add one element each time.
  KEY=@FILE     the contents of a local file (JSON unless KEY takes a string); write @@ for a
                value that starts with @
  --json FILE   a JSON object with the arguments; "-" reads standard input. KEY=VALUE
                replaces its keys

Progress goes to standard error unless --quiet. Ctrl+C cancels the call on the target.

Exit status: 0 success, 1 the tool reported an error (printed to standard error), 2 wrong
usage, 3 no session or the connection was lost, 130 interrupted.

Example:
  host-shell call exec_run command='uname -a'
  host-shell call fs_write path=/tmp/a.txt content=@a.txt
  host-shell call exec_run --json - <<'EOF'
  {"argv": ["sh", "-c", "echo \"$HOME\""]}
  EOF

options:
`

const toolsUsage = `usage: host-shell tools [--session NAME] [--schema] [--instructions] [TOOL ...]

List the tools of a running host-shell session: one line per tool with its name and the
first sentence of its description. --schema prints the full descriptions and input schemas
as JSON; --instructions prints the usage notes an MCP client would receive. For detailed
help on the target, run: host-shell call help_get

options:
`

// Call runs host-shell call and returns the exit code.
func Call(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		if len(args) > 0 && isHelp(args[0]) {
			fmt.Fprint(stdout, callUsage)
			return exitOK
		}
		fmt.Fprint(stderr, callUsage)
		fmt.Fprintln(stderr, "\nhost-shell call: TOOL must come first")
		return exitUsage
	}
	tool := args[0]
	fs := flag.NewFlagSet("host-shell call", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, callUsage); fs.PrintDefaults() }
	name := fs.String("session", "", "session name (default: $HOST_SHELL_SESSION or \"default\")")
	jsonFile := fs.String("json", "", "read the arguments as a JSON object from FILE (\"-\": standard input)")
	quiet := fs.Bool("quiet", false, "do not print progress and log messages")
	pairs, err := parseInterleaved(fs, args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cs, code := connect(ctx, session.Name(*name), *quiet, stderr)
	if cs == nil {
		return code
	}
	defer cs.Close()

	t, err := findTool(ctx, cs, tool)
	if err != nil {
		return report(stderr, err)
	}
	s, err := parseSchema(t.InputSchema)
	if err != nil {
		fmt.Fprintf(stderr, "host-shell call: input schema of %s: %v\n", tool, err)
		return exitSession
	}
	var base map[string]any
	if *jsonFile != "" {
		if base, err = readArgsFile(*jsonFile, func() ([]byte, error) { return io.ReadAll(stdin) }); err != nil {
			fmt.Fprintf(stderr, "host-shell call: %v\n", err)
			return exitUsage
		}
	}
	callArgs, err := buildArgs(s, base, pairs, os.ReadFile)
	if err != nil {
		fmt.Fprintf(stderr, "host-shell call %s: %v\n", tool, err)
		return exitUsage
	}

	params := &mcp.CallToolParams{Name: tool, Arguments: callArgs}
	if !*quiet {
		params.SetProgressToken("host-shell-call")
	}
	res, err := cs.CallTool(ctx, params)
	if ctx.Err() != nil {
		fmt.Fprintf(stderr, "host-shell call: interrupted; %s was cancelled on the target\n", tool)
		return exitInterrupted
	}
	if err != nil {
		return report(stderr, err)
	}
	if res.IsError {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				fmt.Fprintln(stderr, tc.Text)
			}
		}
		return exitToolError
	}
	if err := printResult(stdout, res); err != nil {
		fmt.Fprintf(stderr, "host-shell call: %v\n", err)
		return exitToolError
	}
	return exitOK
}

// Tools runs host-shell tools and returns the exit code.
func Tools(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("host-shell tools", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, toolsUsage); fs.PrintDefaults() }
	name := fs.String("session", "", "session name (default: $HOST_SHELL_SESSION or \"default\")")
	withSchema := fs.Bool("schema", false, "print full descriptions and input schemas as JSON")
	instructions := fs.Bool("instructions", false, "print the usage notes of the session")
	names, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cs, code := connect(ctx, session.Name(*name), true, stderr)
	if cs == nil {
		return code
	}
	defer cs.Close()

	if *instructions {
		fmt.Fprintln(stdout, strings.TrimSpace(cs.InitializeResult().Instructions))
		return exitOK
	}
	var tools []*mcp.Tool
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return report(stderr, err)
		}
		if len(names) == 0 || slices.Contains(names, t.Name) {
			tools = append(tools, t)
		}
	}
	for _, n := range names {
		if !slices.ContainsFunc(tools, func(t *mcp.Tool) bool { return t.Name == n }) {
			fmt.Fprintf(stderr, "host-shell tools: unknown tool %q\n", n)
			return exitUsage
		}
	}
	if *withSchema {
		type entry struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		}
		out := make([]entry, 0, len(tools))
		for _, t := range tools {
			out = append(out, entry{t.Name, t.Description, t.InputSchema})
		}
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return exitOK
	}
	width := 0
	for _, t := range tools {
		width = max(width, len(t.Name))
	}
	for _, t := range tools {
		fmt.Fprintf(stdout, "%-*s  %s\n", width, t.Name, firstSentence(t.Description))
	}
	return exitOK
}

func isHelp(arg string) bool {
	return arg == "-h" || arg == "-help" || arg == "--help" || arg == "help"
}

// parseInterleaved parses options placed before, between or after positional arguments, and
// returns the positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if rest[0] == "--" {
			return append(positional, rest[1:]...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// connect opens an MCP session with the named host-shell session.
func connect(ctx context.Context, name string, quiet bool, stderr io.Writer) (*mcp.ClientSession, int) {
	conn, err := session.Dial(name)
	if err != nil {
		fmt.Fprintf(stderr, "host-shell: %v\n", err)
		return nil, exitSession
	}
	copt := &mcp.ClientOptions{}
	if !quiet {
		copt.ProgressNotificationHandler = func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			p := req.Params
			switch {
			case p.Message != "" && p.Total > 0:
				fmt.Fprintf(stderr, "progress: %s (%.0f/%.0f)\n", p.Message, p.Progress, p.Total)
			case p.Message != "":
				fmt.Fprintf(stderr, "progress: %s\n", p.Message)
			case p.Total > 0:
				fmt.Fprintf(stderr, "progress: %.0f/%.0f\n", p.Progress, p.Total)
			default:
				fmt.Fprintf(stderr, "progress: %.0f\n", p.Progress)
			}
		}
		copt.LoggingMessageHandler = func(_ context.Context, req *mcp.LoggingMessageRequest) {
			b, _ := json.Marshal(req.Params.Data)
			fmt.Fprintf(stderr, "log (%s): %s\n", req.Params.Level, b)
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "host-shell-cli", Version: version.Version}, copt)
	cs, err := client.Connect(ctx, &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		conn.Close()
		fmt.Fprintf(stderr, "host-shell: session %q: %v\n", name, err)
		return nil, exitSession
	}
	return cs, exitOK
}

// findTool returns the named tool of the session.
func findTool(ctx context.Context, cs *mcp.ClientSession, name string) (*mcp.Tool, error) {
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		if t.Name == name {
			return t, nil
		}
	}
	return nil, &usageError{fmt.Sprintf("unknown tool %q; list the tools with: host-shell tools", name)}
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// report prints err and returns its exit code: errors the session or the tool returned are
// tool errors; anything else means the connection failed.
func report(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "host-shell: %v\n", err)
	var ue *usageError
	var we *jsonrpc.Error
	switch {
	case errors.As(err, &ue):
		return exitUsage
	case errors.As(err, &we):
		return exitToolError
	}
	return exitSession
}

// printResult prints the structured result as one line of JSON, or the text content when the
// tool returns no structured result.
func printResult(w io.Writer, res *mcp.CallToolResult) error {
	if res.StructuredContent != nil {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		return enc.Encode(res.StructuredContent)
	}
	for _, c := range res.Content {
		switch c := c.(type) {
		case *mcp.TextContent:
			if _, err := fmt.Fprintln(w, c.Text); err != nil {
				return err
			}
		default:
			b, err := json.Marshal(c)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w, string(b)); err != nil {
				return err
			}
		}
	}
	return nil
}

// firstSentence returns the description up to the end of its first sentence.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}
