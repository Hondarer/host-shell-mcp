// Package bridge は、手元のマシンで動き、ssh の先の host-shell を中継する MCP サーバーです。
//
// エージェントには、接続先の host-shell のツールをそのまま見せ、加えて、手元と接続先の間で
// ファイルやディレクトリを送るツール (transfer_push、transfer_pull) を提供します。
// 送る内容はエージェントの文脈を通らず、bridge と接続先の間で直接やり取りします。
//
// 進み具合の通知、要求の中止、利用者への確認 (elicitation)、ログの通知も中継します。
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

	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/tools"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

const usage = `usage: host-shell bridge [options] -- COMMAND [ARGS...]

Run on the local machine as the MCP server registered in the agent. COMMAND starts
host-shell on the remote host, for example:

  host-shell bridge -- ssh -T hop1 ssh -T target /opt/host-shell/host-shell --mcp

All tools of the remote host-shell are passed through. transfer_push and transfer_pull
copy files and directories between this machine and the remote host without passing
their contents through the agent's context.

options:
`

// Options は bridge の設定です。
type Options struct {
	// Command は接続先の host-shell を起動するコマンドです。
	Command []string
	// AllowLocalPaths は、手元で読み書きしてよい場所です。空の場合は制限しません。
	AllowLocalPaths []string
	// AllowLocalReadPaths は、手元で読み取りだけを許す場所です。
	AllowLocalReadPaths []string
	// NoTransfer は転送のツールを加えないことを示します (中継だけを行う)。
	NoTransfer bool
}

// Main は bridge サブコマンドの引数を解釈して実行します。
func Main(args []string) error {
	fs := flag.NewFlagSet("host-shell bridge", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	var opt Options
	fs.Func("allow-local-path", "restrict transfers to this local directory for reading and writing (repeatable)", func(v string) error {
		opt.AllowLocalPaths = append(opt.AllowLocalPaths, v)
		return nil
	})
	fs.Func("allow-local-read-path", "also allow transfer_push to read (not transfer_pull to write) under this local directory (repeatable)", func(v string) error {
		opt.AllowLocalReadPaths = append(opt.AllowLocalReadPaths, v)
		return nil
	})
	fs.BoolVar(&opt.NoTransfer, "no-transfer", false, "do not add transfer_push and transfer_pull (only relay)")
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
	cmd.Stderr = os.Stderr // ssh のエラーはエージェントのログに出す。
	err := Run(ctx, opt, &mcp.CommandTransport{Command: cmd}, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		err = nil
	}
	return err
}

// Run は remote (接続先の host-shell) につなぎ、local (エージェント) に対して MCP サーバーとして動きます。
// どちらかの接続が終わると戻ります。
func Run(ctx context.Context, opt Options, remote, local mcp.Transport) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	localPolicy, err := policy.New(policy.Options{AllowPaths: opt.AllowLocalPaths, AllowReadPaths: opt.AllowLocalReadPaths}, nil)
	if err != nil {
		return err
	}

	// エージェントとのセッションは、接続先とのセッションの後にできる。
	// それまでに接続先から通知や要求が届くことはない (ツールを呼ぶまで何も起きない)。
	var agent atomic.Pointer[mcp.ServerSession]
	client := mcp.NewClient(&mcp.Implementation{Name: tools.BridgeClientName, Version: version.Version}, &mcp.ClientOptions{
		// 接続先が利用者の確認を求めた結果 (input required) は、そのままエージェントに返し、
		// エージェントが回答を付けてやり直した呼び出しを接続先に中継する。
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
	cs, err := client.Connect(ctx, remote, nil)
	if err != nil {
		return fmt.Errorf("connect to remote host-shell: %w", err)
	}
	defer cs.Close()

	init := cs.InitializeResult()
	instructions := ""
	if init != nil {
		instructions = init.Instructions
	}
	if !opt.NoTransfer {
		instructions += "\n\n" + transferInstructions
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "host-shell", Title: "host-shell (bridge)", Version: remoteVersion(init)},
		&mcp.ServerOptions{Instructions: instructions})

	if err := relayTools(ctx, cs, srv); err != nil {
		return err
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

	done := make(chan error, 2)
	go func() { done <- ss.Wait() }()
	go func() {
		err := cs.Wait()
		if err == nil || errors.Is(err, io.EOF) {
			err = errors.New("remote host-shell disconnected")
		}
		done <- err
	}()
	err = <-done
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

// relayTools は接続先のツールを、同じ名前と説明でエージェントに見せ、呼び出しを中継します。
func relayTools(ctx context.Context, cs *mcp.ClientSession, srv *mcp.Server) error {
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list remote tools: %w", err)
		}
		srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// _meta (進み具合の通知の progressToken を含む) もそのまま渡す。
			p := &mcp.CallToolParams{
				Meta: req.Params.Meta, Name: req.Params.Name, Arguments: req.Params.Arguments,
				InputResponses: req.Params.InputResponses, RequestState: req.Params.RequestState,
			}
			// エージェントが要求を取り消すと ctx が終わり、接続先にも取り消しが伝わる。
			return cs.CallTool(ctx, p)
		})
	}
	return nil
}

// relayResources は接続先のリソース (help の各 topic) を中継します。
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

// callRemote は接続先のツールを呼び、構造化された結果を out に読み込みます。
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
