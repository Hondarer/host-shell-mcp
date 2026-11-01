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
	"errors"
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
	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/forward"
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

	cfg, showVersion, err := parseFlags(flag.CommandLine, os.Args[1:])
	switch {
	case showVersion:
		fmt.Println("host-shell", version.Version)
		return
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, "usage: host-shell --mcp [options]          (on the remote host)")
		fmt.Fprintln(os.Stderr, "       host-shell bridge [options] -- COMMAND...  (on the local machine; see host-shell bridge -h)")
		flag.PrintDefaults()
		os.Exit(2)
	case err != nil:
		log.Fatal(err)
	}

	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// errUsage reports that host-shell was started without --mcp.
var errUsage = errors.New("usage")

// parseFlags builds the server configuration from the target's command line.
func parseFlags(fs *flag.FlagSet, args []string) (cfg config.Config, showVersion bool, err error) {
	cfg = config.Default()
	var (
		allowPaths, allowReadPaths, allowCommands, denyCommands, allowHosts, unsetEnv, redactPatterns, remotePorts listFlag

		proxyMode = fs.Bool("client-proxy", false, "route HTTP/HTTPS through an explicitly enabled local bridge")
		fwdLocal  = fs.Bool("allow-forward-local", false, "permit the bridge's local forwards and SOCKS5 to connect from this host (takes no value; destinations follow --allow-host)")
		fwdRemote = fs.Bool("allow-forward-remote", false, "permit the bridge's remote forwards to listen on this host's loopback (takes no value)")
		mcpMode   = fs.Bool("mcp", false, "serve MCP over stdin/stdout")
		showVer   = fs.Bool("version", false, "print version and exit")
		noLog     = fs.Bool("no-log", false, "disable the audit log")
		logFile   = fs.String("log-file", cfg.LogFile, "audit log file (JSON Lines)")
		maxOut    = fs.Int("max-output", cfg.MaxOutputBytes, "maximum bytes per output stream")
		timeout   = fs.Duration("timeout", cfg.DefaultTimeout, "default command timeout")
		maxTime   = fs.Duration("max-timeout", cfg.MaxTimeout, "maximum client-requested command timeout")
		winShell  = fs.String("windows-shell", cfg.WindowsShell, "default Windows shell: cmd, powershell, or pwsh")

		toolSel  = fs.String("tools", "all", "tools to expose: comma-separated groups (files, search, exec, net, admin) or names, applied left to right; -name excludes (e.g. all,-admin,-exec_sudo)")
		readOnly = fs.Bool("read-only", false, "expose only non-modifying tools (limits net_request_http/net_exchange to read-only use)")
		confirm  = fs.String("confirm", "", "tools requiring user confirmation (MCP elicitation) per call: groups, tool names, or destructive (e.g. destructive or exec,fs_remove)")
		redact   = fs.Bool("redact", false, "replace secrets (private keys, tokens, passwords) in tool output with [REDACTED]")
	)
	fs.Var(&remotePorts, "allow-forward-remote-port", "limit the ports remote forwards listen on to PORT or PORT-PORT (comma-separated or repeatable; requires --allow-forward-remote)")
	fs.Var(&allowPaths, "allow-path", "restrict file read/write access to this directory (repeatable)")
	fs.Var(&allowReadPaths, "allow-read-path", "allow read-only file access under this directory (repeatable)")
	fs.Var(&allowCommands, "allow-command", "allow only matching program names (comma-separated globs or repeatable); shell commands require the shell itself")
	fs.Var(&denyCommands, "deny-command", "reject command lines matching this regular expression (repeatable)")
	fs.Var(&allowHosts, "allow-host", "restrict network tools to matching destinations: host glob or CIDR with optional :port, or unix:/path glob (comma-separated or repeatable)")
	fs.Var(&unsetEnv, "unset-env", "exclude matching environment variables from child processes (comma-separated globs or repeatable)")
	fs.Var(&redactPatterns, "redact-pattern", "redact output matching this regular expression (repeatable)")
	if err := fs.Parse(args); err != nil {
		return cfg, false, err
	}
	if *showVer {
		return cfg, true, nil
	}
	if !*mcpMode {
		return cfg, false, errUsage
	}

	cfg.ClientProxy = *proxyMode
	cfg.ForwardLocal = *fwdLocal
	cfg.ForwardRemote = *fwdRemote
	ports, err := policy.ParsePorts(remotePorts)
	if err != nil {
		return cfg, false, err
	}
	cfg.ForwardRemotePorts = ports
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
	return cfg, false, cfg.Validate()
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

	// The channel transport passes ordinary MCP through and answers a bridge's channel hello,
	// so a bridge asking for features this host does not permit fails with a precise error.
	var channelOptions channel.Options
	sess := server.Session{Audit: al}
	if cfg.ClientProxy {
		target, err := clientproxy.NewTarget()
		if err != nil {
			return err
		}
		defer target.Close()
		sess.Proxy = target
		target.SetAudit(al)
		target.Register(&channelOptions)
	}
	// One policy serves the tools and port forwarding, so both see the proxy secret.
	if sess.Policy, err = server.NewPolicy(cfg, sess.Proxy, al); err != nil {
		return err
	}
	if cfg.ForwardLocal || cfg.ForwardRemote {
		sess.Forward = forward.NewTarget(cfg.ForwardLocal, cfg.ForwardRemote, sess.Policy, al)
		sess.Forward.RestrictRemotePorts(cfg.ForwardRemotePorts)
		defer sess.Forward.Close()
		sess.Forward.Register(&channelOptions)
	}
	transport := channel.Target(&mcp.StdioTransport{}, channelOptions)
	s, closeServer := server.New(cfg, sess)
	defer closeServer()

	al.Event("server_start", "version", version.Version, "pid", os.Getpid())
	// Run returns when stdin closes (e.g., on SSH disconnect).
	err = s.Run(ctx, transport)
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
