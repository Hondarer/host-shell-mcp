// Package bridge implements a local MCP server that relays to a remote host-shell over SSH.
//
// It forwards all remote tools, resources, progress notifications, cancellations,
// elicitations, and log messages, and adds transfer_push and transfer_pull to copy
// files and directories directly between the local machine and the target without
// passing file contents through the agent's context.
package bridge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/hondarer/host-shell-mcp/internal/clientproxy"
	"github.com/hondarer/host-shell-mcp/internal/forward"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/remotedns"
	"github.com/hondarer/host-shell-mcp/internal/sshpass"
	"github.com/hondarer/host-shell-mcp/internal/version"
	"github.com/hondarer/host-shell-mcp/internal/wire"
)

const usage = `usage: host-shell bridge [options] -- COMMAND [ARGS...]

Run locally as the MCP server registered in the agent. COMMAND starts host-shell
on the remote host, for example:

  host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp

All remote tools are passed through. transfer_push and transfer_pull copy files and
directories directly between this machine and the remote host without passing their
contents through the agent's context. --forward-* options forward TCP ports over the same
session; the target must permit them with --allow-forward-local or --allow-forward-remote.

options:
`

// Options configures the bridge.
type Options struct {
	RemoteDNS             bool
	Proxy                 bool
	ProxyAllowHosts       []string
	ProxyDenyHosts        []string
	ProxyAllowRuleUpdates bool
	// Forwards are persistent forwards; ForwardAllowLocal and ForwardAllowRemote bound forward_open and SOCKS.
	Forwards           []forward.Spec
	ForwardAllowLocal  []string
	ForwardAllowRemote []string
	// Command starts host-shell on the target.
	Command []string
	// AllowLocalPaths restricts local read/write access to these directories (empty means unrestricted).
	AllowLocalPaths []string
	// AllowLocalReadPaths allows read-only local access under these directories.
	AllowLocalReadPaths []string
	// NoTransfer disables the transfer_push and transfer_pull tools.
	NoTransfer bool
}

// Main parses arguments for the bridge subcommand and runs the bridge.
func Main(args []string) error {
	fs := flag.NewFlagSet("host-shell bridge", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	var opt Options
	fs.BoolVar(&opt.RemoteDNS, "remote-dns", false, "resolve target getaddrinfo requests on this machine (requires --remote-dns on target)")
	fs.BoolVar(&opt.Proxy, "proxy", false, "enable target HTTP/HTTPS proxy over this session")
	fs.Func("proxy-allow-host", "allow proxy destination host glob with optional :port (repeatable; required with --proxy)", func(v string) error { opt.ProxyAllowHosts = append(opt.ProxyAllowHosts, v); return nil })
	fs.Func("proxy-deny-host", "deny proxy destination host glob with optional :port (repeatable; overrides allow rules)", func(v string) error { opt.ProxyDenyHosts = append(opt.ProxyDenyHosts, v); return nil })
	fs.BoolVar(&opt.ProxyAllowRuleUpdates, "proxy-allow-rule-updates", false, "expose proxy_rules_update for session-local destination rule changes over MCP")
	spec := func(direction string) func(string) error {
		return func(v string) error {
			s, err := forward.ParseSpec(direction, v)
			opt.Forwards = append(opt.Forwards, s)
			return err
		}
	}
	fs.Func("forward-local", "listen on this machine and connect from the target: [BIND:]PORT:HOST:HOSTPORT, BIND 127.0.0.1 or [::1] (repeatable)", spec(forward.Local))
	fs.Func("forward-remote", "listen on the target and connect from this machine: [BIND:]PORT:HOST:HOSTPORT (repeatable)", spec(forward.Remote))
	fs.Func("forward-socks", "serve SOCKS5 on this machine, connecting from the target: [BIND:]PORT (repeatable; requires --forward-allow-local)", spec(forward.SOCKS))
	fs.Func("forward-allow-local", "allow forward_open local forwards and SOCKS destinations: host glob with optional :port (repeatable)", func(v string) error { opt.ForwardAllowLocal = append(opt.ForwardAllowLocal, v); return nil })
	fs.Func("forward-allow-remote", "allow forward_open remote forwards to this destination: host glob with optional :port (repeatable)", func(v string) error { opt.ForwardAllowRemote = append(opt.ForwardAllowRemote, v); return nil })
	fs.Func("allow-local-path", "restrict local file transfers (read/write) to this directory (repeatable)", func(v string) error {
		opt.AllowLocalPaths = append(opt.AllowLocalPaths, v)
		return nil
	})
	fs.Func("allow-local-read-path", "allow transfer_push to read (not transfer_pull to write) under this local directory (repeatable)", func(v string) error {
		opt.AllowLocalReadPaths = append(opt.AllowLocalReadPaths, v)
		return nil
	})
	fs.BoolVar(&opt.NoTransfer, "no-transfer", false, "disable transfer_push and transfer_pull (relay only)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	opt.Command = fs.Args()
	if len(opt.Command) == 0 {
		fs.Usage()
		return errors.New("COMMAND is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd := exec.Command(opt.Command[0], opt.Command[1:]...)
	// Supply stored SSH login passwords (host-shell ssh-pass) to ssh through SSH_ASKPASS. COMMAND
	// may be a script that runs ssh, so the ssh in PATH decides the mode then.
	if self, err := os.Executable(); err == nil {
		ssh := "ssh"
		if sshpass.IsSSH(opt.Command[0]) {
			ssh = opt.Command[0]
		}
		mode := sshpass.Mode(ssh)
		cmd.Env = sshpass.Env(os.Environ(), self, mode)
		if mode == sshpass.ModeLegacy {
			sshpass.Detach(cmd)
		}
	}
	cmd.Stderr = os.Stderr // forward SSH stderr to the agent's log
	err := Run(ctx, opt, &mcp.CommandTransport{Command: cmd}, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		err = nil
	}
	return err
}

// Run connects to remote (the target host-shell) and serves local (the agent) until either session ends.
func Run(ctx context.Context, opt Options, remote, local mcp.Transport) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var channelOptions channel.Options
	if opt.RemoteDNS {
		remotedns.RegisterGateway(&channelOptions)
	}
	gateway, err := newGateway(opt, &channelOptions)
	if err != nil {
		return err
	}
	forwards, err := forward.NewManager(opt.Forwards, opt.ForwardAllowLocal, opt.ForwardAllowRemote)
	if err != nil {
		return err
	}
	if forwards.Enabled() {
		// Local listeners are bound before the target starts so that a busy port fails fast.
		if err := forwards.Bind(); err != nil {
			return err
		}
		defer forwards.Close()
		forwards.Register(&channelOptions)
	}
	if len(channelOptions.Features) > 0 {
		remote = channel.Client(remote, channelOptions)
	}
	localPolicy, err := policy.New(policy.Options{AllowPaths: opt.AllowLocalPaths, AllowReadPaths: opt.AllowLocalReadPaths}, nil)
	if err != nil {
		return err
	}

	// The agent session is connected after the remote session; no remote notifications arrive before tool calls.
	var agent atomic.Pointer[mcp.ServerSession]
	cs, err := newRemoteClient(&agent).Connect(ctx, remote, nil)
	if err != nil {
		return fmt.Errorf("connect to remote host-shell: %w", err)
	}
	defer cs.Close()

	init := cs.InitializeResult()
	srv := mcp.NewServer(&mcp.Implementation{Name: "host-shell", Title: "host-shell (bridge)", Version: remoteVersion(init)},
		&mcp.ServerOptions{Instructions: bridgeInstructions(init, opt, gateway != nil, forwards)})

	if err := relayTools(ctx, cs, srv); err != nil {
		return err
	}
	registerProxyTools(srv, gateway, opt.ProxyAllowRuleUpdates)

	if forwards.Enabled() {
		registerForwardTools(srv, forwards)
	}
	if err := relayResources(ctx, cs, srv); err != nil {
		return err
	}
	if !opt.NoTransfer {
		t := &transfer{remote: cs, local: localPolicy}
		if err := t.init(ctx); err != nil {
			return err
		}
		t.register(srv)
	}

	ss, err := srv.Connect(ctx, local, nil)
	if err != nil {
		return err
	}
	agent.Store(ss)
	return waitSessions(ctx, ss, cs)
}

// newGateway creates the client proxy gateway when --proxy is set and registers it on o.
func newGateway(opt Options, channelOpt *channel.Options) (*clientproxy.Gateway, error) {
	if !opt.Proxy {
		if len(opt.ProxyAllowHosts) > 0 || len(opt.ProxyDenyHosts) > 0 || opt.ProxyAllowRuleUpdates {
			return nil, errors.New("proxy rules and --proxy-allow-rule-updates require --proxy")
		}
		return nil, nil
	}
	gateway, err := clientproxy.NewGateway(opt.ProxyAllowHosts, opt.ProxyDenyHosts)
	if err != nil {
		return nil, err
	}
	gateway.Register(channelOpt)
	return gateway, nil
}

// newRemoteClient creates the client for the target. Elicitation, progress and log messages
// from the target are relayed to the agent session once it is stored in agent.
func newRemoteClient(agent *atomic.Pointer[mcp.ServerSession]) *mcp.Client {
	return mcp.NewClient(&mcp.Implementation{Name: wire.BridgeClientName, Version: version.Version}, &mcp.ClientOptions{
		// Return elicitation requests to the agent as-is and forward the retried call with answers to the target.
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
		ElicitationHandler: func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			ss := agent.Load()
			if ss == nil {
				return nil, errors.New("agent is not connected")
			}
			return ss.Elicit(ctx, req.Params)
		},
		ProgressNotificationHandler: func(ctx context.Context, req *mcp.ProgressNotificationClientRequest) {
			if ss := agent.Load(); ss != nil {
				_ = ss.NotifyProgress(ctx, req.Params)
			}
		},
		LoggingMessageHandler: func(ctx context.Context, req *mcp.LoggingMessageRequest) {
			if ss := agent.Load(); ss != nil {
				_ = ss.Log(ctx, req.Params)
			}
		},
	})
}

// bridgeInstructions extends the target's instructions with the bridge's own tools.
func bridgeInstructions(init *mcp.InitializeResult, opt Options, proxy bool, forwards *forward.Manager) string {
	instructions := ""
	if init != nil {
		instructions = init.Instructions
	}
	if proxy {
		instructions += "\nClient proxy enabled: use proxy_status to inspect destination rules and recent denials."
		if opt.ProxyAllowRuleUpdates {
			instructions += " Use proxy_rules_update only for user-authorized rule changes; read rules_revision from proxy_status before replacing both lists. Changes apply to new connections and last only for this session."
		}
	}
	if forwards.Enabled() {
		instructions += "\n" + forwardInstructions(forwards)
	}
	if !opt.NoTransfer {
		instructions += "\n\n" + transferInstructions
	}
	return instructions
}

// waitSessions waits until the agent or the target session ends, then closes the agent session.
func waitSessions(ctx context.Context, ss *mcp.ServerSession, cs *mcp.ClientSession) error {
	done := make(chan error, 2)
	go func() { done <- ss.Wait() }()
	go func() {
		err := cs.Wait()
		if err == nil || errors.Is(err, io.EOF) {
			err = errors.New("remote host-shell disconnected")
		}
		done <- err
	}()
	err := <-done
	ss.Close()
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("bridge: %v", err)
	}
	return err
}

func remoteVersion(init *mcp.InitializeResult) string {
	if init != nil && init.ServerInfo != nil {
		return init.ServerInfo.Version
	}
	return version.Version
}

// relayTools registers the target's tools on srv and forwards invocations.
func relayTools(ctx context.Context, cs *mcp.ClientSession, srv *mcp.Server) error {
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list remote tools: %w", err)
		}
		srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Forward _meta (including progressToken) and request state; context cancellation propagates to the target.
			p := &mcp.CallToolParams{
				Meta: req.Params.Meta, Name: req.Params.Name, Arguments: req.Params.Arguments,
				InputResponses: req.Params.InputResponses, RequestState: req.Params.RequestState,
			}
			return cs.CallTool(ctx, p)
		})
	}
	return nil
}

// relayResources registers and forwards the target's resources (help topics).
func relayResources(ctx context.Context, cs *mcp.ClientSession, srv *mcp.Server) error {
	init := cs.InitializeResult()
	if init == nil || init.Capabilities == nil || init.Capabilities.Resources == nil {
		return nil
	}
	for r, err := range cs.Resources(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list remote resources: %w", err)
		}
		srv.AddResource(r, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: req.Params.URI})
		})
	}
	return nil
}

// callRemote calls a tool on the target and decodes the structured result into out.
func callRemote(ctx context.Context, cs *mcp.ClientSession, name string, args map[string]any, out any) error {
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return fmt.Errorf("remote %s: %w", name, err)
	}
	if res.IsError {
		var msg []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg = append(msg, tc.Text)
			}
		}
		return &remoteError{tool: name, msg: strings.Join(msg, " ")}
	}
	if out == nil {
		return nil
	}
	return remarshal(res.StructuredContent, out)
}

type remoteError struct {
	tool, msg string
}

func (e *remoteError) Error() string { return fmt.Sprintf("remote %s: %s", e.tool, e.msg) }
