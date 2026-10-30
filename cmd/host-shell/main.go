// host-shell is an MCP server that runs on a target host over stdin/stdout (typically via SSH):
//
//	ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
//
// Locally, the bridge subcommand relays to a remote host-shell over SSH and adds direct
// local-to-remote file transfer tools:
//
//	host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
//
// Standard output carries MCP JSON-RPC only; diagnostics go to standard error and audit records to a log file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/bridge"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/server"
	"github.com/hondarer/host-shell-mcp/internal/tools"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("host-shell: ")
	log.SetFlags(0)

	// exec_sudo re-invokes this binary as the sudo -A askpass helper.
	if ran, code := tools.Askpass(); ran {
		os.Exit(code)
	}

	if len(os.Args) > 1 && os.Args[1] == "bridge" {
		if err := bridge.Main(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg := config.Default()
	var (
		allowPaths, allowReadPaths, allowCommands, denyCommands, allowHosts, unsetEnv, redactPatterns listFlag

		mcpMode  = flag.Bool("mcp", false, "serve MCP over stdin/stdout")
		showVer  = flag.Bool("version", false, "print version and exit")
		noLog    = flag.Bool("no-log", false, "disable the audit log")
		logFile  = flag.String("log-file", cfg.LogFile, "audit log file (JSON Lines)")
		maxOut   = flag.Int("max-output", cfg.MaxOutputBytes, "maximum bytes per output stream")
		timeout  = flag.Duration("timeout", cfg.DefaultTimeout, "default command timeout")
		maxTime  = flag.Duration("max-timeout", cfg.MaxTimeout, "maximum client-requested command timeout")
		winShell = flag.String("windows-shell", cfg.WindowsShell, "default Windows shell: cmd, powershell, or pwsh")

		toolSel  = flag.String("tools", "all", "tools to expose: comma-separated groups (files, search, exec, net, admin) or names, applied left to right; -name excludes (e.g. all,-admin,-exec_sudo)")
		readOnly = flag.Bool("read-only", false, "expose only non-modifying tools (limits net_request_http/net_exchange to read-only use)")
		confirm  = flag.String("confirm", "", "tools requiring user confirmation (MCP elicitation) per call: groups, tool names, or destructive (e.g. destructive or exec,fs_remove)")
		redact   = flag.Bool("redact", false, "replace secrets (private keys, tokens, passwords) in tool output with [REDACTED]")
	)
	flag.Var(&allowPaths, "allow-path", "restrict file read/write access to this directory (repeatable)")
	flag.Var(&allowReadPaths, "allow-read-path", "allow read-only file access under this directory (repeatable)")
	flag.Var(&allowCommands, "allow-command", "allow only matching program names (comma-separated globs or repeatable); shell commands require the shell itself")
	flag.Var(&denyCommands, "deny-command", "reject command lines matching this regular expression (repeatable)")
	flag.Var(&allowHosts, "allow-host", "restrict network tools to matching destinations: host glob or CIDR with optional :port, or unix:/path glob (comma-separated or repeatable)")
	flag.Var(&unsetEnv, "unset-env", "exclude matching environment variables from child processes (comma-separated globs or repeatable)")
	flag.Var(&redactPatterns, "redact-pattern", "redact output matching this regular expression (repeatable)")
	flag.Parse()

	if *showVer {
		fmt.Println("host-shell", version.Version)
		return
	}
	if !*mcpMode {
		fmt.Fprintln(os.Stderr, "usage: host-shell --mcp [options]          (on the remote host)")
		fmt.Fprintln(os.Stderr, "       host-shell bridge [options] -- COMMAND...  (on the local machine; see host-shell bridge -h)")
		flag.PrintDefaults()
		os.Exit(2)
	}

	cfg.LogFile = *logFile
	if *noLog {
		cfg.LogFile = ""
	}
	cfg.MaxOutputBytes = *maxOut
	cfg.DefaultTimeout = *timeout
	cfg.MaxTimeout = *maxTime
	cfg.WindowsShell = *winShell
	cfg.Policy = policy.Options{
		Tools:          *toolSel,
		ReadOnly:       *readOnly,
		Confirm:        *confirm,
		AllowPaths:     allowPaths,
		AllowReadPaths: allowReadPaths,
		AllowCommands:  splitList(allowCommands),
		DenyCommands:   denyCommands,
		AllowHosts:     splitList(allowHosts),
		UnsetEnv:       splitList(unsetEnv),
		Redact:         *redact,
		RedactPatterns: redactPatterns,
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func run(cfg config.Config) error {
	if err := proc.KillChildrenOnExit(); err != nil {
		log.Printf("warning: commands may outlive host-shell if it is killed: %v", err)
	}
	al, err := audit.Open(cfg.LogFile)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer al.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, closeServer, err := server.New(cfg, al)
	if err != nil {
		return err
	}
	defer closeServer()

	al.Event("server_start", "version", version.Version, "pid", os.Getpid())
	// Run returns when stdin closes (e.g., on SSH disconnect).
	err = s.Run(ctx, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		err = nil
	}
	al.Event("server_stop", "error", fmt.Sprint(err))
	return err
}

// listFlag collects repeatable flag values.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	*l = append(*l, v)
	return nil
}

func splitList(l []string) []string {
	var out []string
	for _, v := range l {
		for f := range strings.SplitSeq(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				out = append(out, f)
			}
		}
	}
	return out
}
