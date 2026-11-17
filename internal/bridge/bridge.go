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
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
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

// AddFlags registers the bridge options on fs; host-shell session start shares them.
func (opt *Options) AddFlags(fs *flag.FlagSet) {
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
}

// Main parses arguments for the bridge subcommand and runs the bridge.
func Main(args []string) error {
	fs := flag.NewFlagSet("host-shell bridge", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	var opt Options
	opt.AddFlags(fs)
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
	err := Run(ctx, opt, Command(opt.Command), &mcp.StdioTransport{})
	if ctx.Err() != nil {
		err = nil
	}
	return err
}

// Command returns the transport that runs command to start host-shell on the target. The
// command's standard error goes to this process's standard error.
func Command(command []string) mcp.Transport {
	cmd := exec.Command(command[0], command[1:]...)
	// Supply stored SSH login passwords (host-shell ssh-pass) to ssh through SSH_ASKPASS. COMMAND
	// may be a script that runs ssh, so the ssh in PATH decides the mode then.
	if self, err := os.Executable(); err == nil {
		ssh := "ssh"
		if sshpass.IsSSH(command[0]) {
			ssh = command[0]
		}
		mode := sshpass.Mode(ssh)
		cmd.Env = sshpass.Env(os.Environ(), self, mode)
		if mode == sshpass.ModeLegacy {
			sshpass.Detach(cmd)
		}
	}
	cmd.Stderr = os.Stderr // forward SSH stderr to the agent's log
	return &mcp.CommandTransport{Command: cmd}
}

// Run connects to remote (the target host-shell) and serves local (the agent) until either session ends.
func Run(ctx context.Context, opt Options, remote, local mcp.Transport) error {
	r, err := Open(ctx, opt, remote, Local{})
	if err != nil {
		return err
	}
	defer r.Close()
	ss, err := r.Connect(ctx, local)
	if err != nil {
		return err
	}
	return waitSessions(ctx, ss, r.cs)
}

// Local configures how a Relay serves its local clients.
type Local struct {
	// Confirm answers the target's requests for user confirmation (--confirm on the target).
	// When nil, the requests go to the local client: an agent's MCP client asks its user.
	//
	// When set, the relay asks Confirm instead and drops the answers that local clients attach
	// to their calls. Clients of host-shell session are commands that the agent runs itself, so
	// an answer from them would be the agent approving its own call.
	Confirm func(ctx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error)
}

// Relay holds the session with the target and the MCP server that local clients connect to.
type Relay struct {
	cs       *mcp.ClientSession
	srv      *mcp.Server
	local    Local
	forwards *forward.Manager
	cancel   context.CancelFunc

	mu       sync.Mutex
	sessions map[*mcp.ServerSession]bool
	routes   map[string]progressRoute
	nextID   uint64
}

// progressRoute is the local session and progress token of a relayed call. Each relayed call
// gets a token of its own on the target, because tokens from different clients may collide.
type progressRoute struct {
	ss    *mcp.ServerSession
	token any
}

// Open connects to remote (the target host-shell) and prepares the tools for local clients.
func Open(ctx context.Context, opt Options, remote mcp.Transport, local Local) (_ *Relay, err error) {
	ctx, cancel := context.WithCancel(ctx)
	r := &Relay{local: local, cancel: cancel, sessions: map[*mcp.ServerSession]bool{}, routes: map[string]progressRoute{}}
	defer func() {
		if err != nil {
			r.Close()
		}
	}()

	var channelOptions channel.Options
	if opt.RemoteDNS {
		remotedns.RegisterGateway(&channelOptions)
	}
	gateway, err := newGateway(opt, &channelOptions)
	if err != nil {
		return nil, err
	}
	r.forwards, err = forward.NewManager(opt.Forwards, opt.ForwardAllowLocal, opt.ForwardAllowRemote)
	if err != nil {
		return nil, err
	}
	if r.forwards.Enabled() {
		// Local listeners are bound before the target starts so that a busy port fails fast.
		if err := r.forwards.Bind(); err != nil {
			return nil, err
		}
		r.forwards.Register(&channelOptions)
	}
	if len(channelOptions.Features) > 0 {
		remote = channel.Client(remote, channelOptions)
	}
	localPolicy, err := policy.New(policy.Options{AllowPaths: opt.AllowLocalPaths, AllowReadPaths: opt.AllowLocalReadPaths}, nil)
	if err != nil {
		return nil, err
	}

	// Local sessions connect after the remote session; no remote notifications arrive before tool calls.
	r.cs, err = r.newRemoteClient().Connect(ctx, remote, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to remote host-shell: %w", err)
	}

	init := r.cs.InitializeResult()
	r.srv = mcp.NewServer(&mcp.Implementation{Name: "host-shell", Title: "host-shell (bridge)", Version: remoteVersion(init)},
		&mcp.ServerOptions{Instructions: bridgeInstructions(init, opt, gateway != nil, r.forwards)})

	if err := r.relayTools(ctx); err != nil {
		return nil, err
	}
	registerProxyTools(r.srv, gateway, opt.ProxyAllowRuleUpdates)

	if r.forwards.Enabled() {
		registerForwardTools(r.srv, r.forwards)
	}
	if err := relayResources(ctx, r.cs, r.srv); err != nil {
		return nil, err
	}
	if !opt.NoTransfer {
		t := &transfer{remote: r.cs, local: localPolicy}
		if err := t.init(ctx); err != nil {
			return nil, err
		}
		t.register(r.srv)
	}
	return r, nil
}

// Connect serves one local client over t until the client or the relay ends.
func (r *Relay) Connect(ctx context.Context, t mcp.Transport) (*mcp.ServerSession, error) {
	ss, err := r.srv.Connect(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.sessions[ss] = true
	r.mu.Unlock()
	go func() {
		_ = ss.Wait()
		r.mu.Lock()
		delete(r.sessions, ss)
		r.mu.Unlock()
	}()
	return ss, nil
}

// Wait waits until the session with the target ends.
func (r *Relay) Wait() error {
	err := r.cs.Wait()
	if err == nil || errors.Is(err, io.EOF) {
		err = errors.New("remote host-shell disconnected")
	}
	return err
}

// RemoteVersion returns the version of host-shell on the target.
func (r *Relay) RemoteVersion() string { return remoteVersion(r.cs.InitializeResult()) }

// Close ends the local sessions and the session with the target, and closes the port forwards.
func (r *Relay) Close() {
	r.mu.Lock()
	sessions := make([]*mcp.ServerSession, 0, len(r.sessions))
	for ss := range r.sessions {
		sessions = append(sessions, ss)
	}
	r.mu.Unlock()
	for _, ss := range sessions {
		ss.Close()
	}
	if r.cs != nil {
		r.cs.Close()
	}
	if r.forwards != nil {
		r.forwards.Close()
	}
	r.cancel()
}

// anySession returns a connected local session; Run has exactly one.
func (r *Relay) anySession() *mcp.ServerSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ss := range r.sessions {
		return ss
	}
	return nil
}

// route gives a relayed call its own progress token on the target. The returned function
// removes the route when the call ends.
func (r *Relay) route(ss *mcp.ServerSession, token any) (string, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := fmt.Sprintf("relay-%d", r.nextID)
	r.routes[id] = progressRoute{ss: ss, token: token}
	return id, func() {
		r.mu.Lock()
		delete(r.routes, id)
		r.mu.Unlock()
	}
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

// newRemoteClient creates the client for the target. Progress notifications go to the local
// session of the call they belong to, and log messages to every local session.
func (r *Relay) newRemoteClient() *mcp.Client {
	copt := &mcp.ClientOptions{
		ProgressNotificationHandler: func(ctx context.Context, req *mcp.ProgressNotificationClientRequest) {
			id, _ := req.Params.ProgressToken.(string)
			r.mu.Lock()
			route, ok := r.routes[id]
			r.mu.Unlock()
			if ok {
				p := *req.Params
				p.ProgressToken = route.token
				_ = route.ss.NotifyProgress(ctx, &p)
			}
		},
		LoggingMessageHandler: func(ctx context.Context, req *mcp.LoggingMessageRequest) {
			r.mu.Lock()
			sessions := make([]*mcp.ServerSession, 0, len(r.sessions))
			for ss := range r.sessions {
				sessions = append(sessions, ss)
			}
			r.mu.Unlock()
			for _, ss := range sessions {
				_ = ss.Log(ctx, req.Params)
			}
		},
	}
	if r.local.Confirm != nil {
		// The SDK's multi-round-trip handling answers the target's input requests through
		// ElicitationHandler and retries the call itself.
		copt.ElicitationHandler = func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return r.local.Confirm(ctx, req.Params)
		}
		return mcp.NewClient(&mcp.Implementation{Name: wire.BridgeClientName, Version: version.Version}, copt)
	}
	// Return elicitation requests to the agent as-is and forward the retried call with answers to the target.
	copt.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	copt.ElicitationHandler = func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		ss := r.anySession()
		if ss == nil {
			return nil, errors.New("agent is not connected")
		}
		return ss.Elicit(ctx, req.Params)
	}
	return mcp.NewClient(&mcp.Implementation{Name: wire.BridgeClientName, Version: version.Version}, copt)
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

// relayTools registers the target's tools on the local server and forwards invocations.
func (r *Relay) relayTools(ctx context.Context) error {
	for tool, err := range r.cs.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list remote tools: %w", err)
		}
		r.srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Forward _meta and, for an agent, its answers to input requests; context cancellation propagates to the target.
			p := &mcp.CallToolParams{Meta: req.Params.Meta, Name: req.Params.Name, Arguments: req.Params.Arguments}
			if r.local.Confirm == nil {
				p.InputResponses, p.RequestState = req.Params.InputResponses, req.Params.RequestState
			}
			if token := req.Params.GetProgressToken(); token != nil {
				id, unroute := r.route(req.Session, token)
				defer unroute()
				p.Meta = maps.Clone(req.Params.Meta)
				p.SetProgressToken(id)
			}
			return r.cs.CallTool(ctx, p)
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
