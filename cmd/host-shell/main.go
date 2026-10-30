// host-shell is an MCP server that runs on the target machine, reached through the standard input
// and output of SSH or a similar relay.
//
//	ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
//
// Locally, the bridge subcommand relays to host-shell at the far end of ssh and serves it as an
// MCP server with extra tools that send files between the local machine and the target.
//
//	host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
//
// Standard output carries MCP JSON-RPC only. Diagnostics go to standard error and the audit record to a log file.
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

	// sudo_run starts host-shell itself as the askpass helper of sudo -A.
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
		maxOut   = flag.Int("max-output", cfg.MaxOutputBytes, "maximum bytes of each output")
		timeout  = flag.Duration("timeout", cfg.DefaultTimeout, "default command timeout")
		maxTime  = flag.Duration("max-timeout", cfg.MaxTimeout, "maximum command timeout a client may request")
		winShell = flag.String("windows-shell", cfg.WindowsShell, "default shell on Windows: cmd, powershell or pwsh")

		toolSel  = flag.String("tools", "all", "tools to expose: comma-separated groups (files, search, exec, net, admin) or tool names, applied left to right; -name removes (e.g. all,-admin,-sudo_run)")
		readOnly = flag.Bool("read-only", false, "expose only tools that do not change the host (net_http/net_tcp limited to non-modifying use)")
		confirm  = flag.String("confirm", "", "tools that need the user's confirmation (MCP elicitation) before each call: groups, tool names or destructive (e.g. destructive or exec,fs_remove)")
		redact   = flag.Bool("redact", false, "replace secrets (private keys, tokens, passwords) in tool output with [REDACTED]")
	)
	flag.Var(&allowPaths, "allow-path", "restrict file tools to this directory for reading and writing (repeatable)")
	flag.Var(&allowReadPaths, "allow-read-path", "also allow reading (not writing) under this directory (repeatable)")
	flag.Var(&allowCommands, "allow-command", "allow only these programs to be run (glob of the program name, comma-separated or repeatable); shell command lines need the shell allowed")
	flag.Var(&denyCommands, "deny-command", "refuse commands whose command line matches this regular expression (repeatable)")
	flag.Var(&allowHosts, "allow-host", "restrict network tools to these destinations: host glob or CIDR with optional :port, or unix:/path glob (comma-separated or repeatable)")
	flag.Var(&unsetEnv, "unset-env", "do not pass these environment variables to commands (glob, comma-separated or repeatable)")
	flag.Var(&redactPatterns, "redact-pattern", "also redact text matching this regular expression (repeatable)")
	flag.Parse()

	if *showVer {
		// --version is used only when not running as MCP, so writing to standard output is fine.
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
	// Commands must not outlive host-shell, even when it is killed.
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
	// Run returns when standard input is closed (SSH disconnects).
	err = s.Run(ctx, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		err = nil // Stopping on a signal is a normal exit.
	}
	al.Event("server_stop", "error", fmt.Sprint(err))
	return err
}

// listFlag is an option that may be given more than once.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// splitList splits a comma-separated value.
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
