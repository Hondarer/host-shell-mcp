// host-shell は、SSH などの標準入出力で中継して使う、接続先のマシン上で動く MCP サーバーです。
//
//	ssh -T hop1 ssh -T target /opt/host-shell/host-shell --mcp
//
// 標準出力は MCP の JSON-RPC 専用です。診断メッセージは標準エラーに、操作の記録はログ ファイルに出力します。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/server"
	"github.com/hondarer/host-shell-mcp/internal/version"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("host-shell: ")
	log.SetFlags(0)

	cfg := config.Default()
	var (
		mcpMode  = flag.Bool("mcp", false, "serve MCP over stdin/stdout")
		showVer  = flag.Bool("version", false, "print version and exit")
		noLog    = flag.Bool("no-log", false, "disable the audit log")
		logFile  = flag.String("log-file", cfg.LogFile, "audit log file (JSON Lines)")
		maxOut   = flag.Int("max-output", cfg.MaxOutputBytes, "maximum bytes of each output")
		timeout  = flag.Duration("timeout", cfg.DefaultTimeout, "default command timeout")
		maxTime  = flag.Duration("max-timeout", cfg.MaxTimeout, "maximum command timeout a client may request")
		winShell = flag.String("windows-shell", cfg.WindowsShell, "default shell on Windows: cmd, powershell or pwsh")
	)
	flag.Parse()

	if *showVer {
		// --version は MCP として起動していないときだけ使うため、標準出力に書いてよい。
		fmt.Println("host-shell", version.Version)
		return
	}
	if !*mcpMode {
		fmt.Fprintln(os.Stderr, "usage: host-shell --mcp [options]")
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
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func run(cfg config.Config) error {
	al, err := audit.Open(cfg.LogFile)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer al.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, closeServer := server.New(cfg, al)
	defer closeServer()

	al.Event("server_start", "version", version.Version, "pid", os.Getpid())
	// 標準入力が閉じられる (SSH が切断される) と Run は戻る。
	err = s.Run(ctx, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		err = nil // シグナルによる停止は正常な終了として扱う。
	}
	al.Event("server_stop", "error", fmt.Sprint(err))
	return err
}
