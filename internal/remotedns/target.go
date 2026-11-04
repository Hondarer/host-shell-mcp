package remotedns

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// TargetOptions selects how processes on the target reach the bridge's resolver.
type TargetOptions struct {
	// Library is a prebuilt Linux/glibc adapter; empty compiles one on the target (Linux only).
	Library string
	// Listen is a loopback address (for example 127.0.0.1:53) for a DNS server answering A and AAAA
	// queries through the bridge. It is required where no preload adapter exists (Windows).
	Listen string
	// Configure routes the OS resolver to Listen for the life of the target (Windows: an NRPT rule and,
	// without an IPv4 default route, a loopback default route) and undoes it on Close.
	Configure bool
}

// Target owns a private per-session resolver socket and native adapter, and optionally a loopback DNS server.
type Target struct {
	Library, Socket string
	// Listen is the address of the loopback DNS server, if enabled.
	Listen string
	// Configured lists the OS resolver changes made for --remote-dns-configure.
	Configured []string
	undo       func()
	dir        string
	listener   net.Listener
	stub       *stub
	ctx        context.Context
	cancel     context.CancelFunc
	conn       *channel.Conn
	once       sync.Once
	mu         sync.Mutex
	closed     bool
	wg         sync.WaitGroup
}

// NewTarget prepares the native hook (Linux) and the optional loopback DNS server; a supplied library must
// match the target ABI.
func NewTarget(ctx context.Context, opt TargetOptions) (*Target, error) {
	t := &Target{}
	t.ctx, t.cancel = context.WithCancel(context.Background())
	if runtime.GOOS == "linux" {
		if err := t.preparePreload(ctx, opt.Library); err != nil {
			t.Close()
			return nil, err
		}
	} else if opt.Listen == "" {
		t.Close()
		return nil, fmt.Errorf("--remote-dns on %s needs --remote-dns-listen 127.0.0.1:53, and --remote-dns-configure or an administrator-added resolver route to it (help_get topic windows)", runtime.GOOS)
	} else if opt.Library != "" {
		t.Close()
		return nil, fmt.Errorf("--remote-dns-library is for Linux/glibc targets; use --remote-dns-listen on %s", runtime.GOOS)
	}
	if opt.Listen != "" {
		s, err := listenStub(opt.Listen, t.ask)
		if err != nil {
			t.Close()
			return nil, err
		}
		t.stub, t.Listen = s, s.addr
	}
	if opt.Configure {
		// Configure only after the server holds the address, so a second session fails before touching the OS.
		server, err := resolverAddress(t.Listen)
		if err == nil {
			t.undo, t.Configured, err = configureResolver(ctx, server)
		}
		if err != nil {
			t.Close()
			return nil, err
		}
	}
	return t, nil
}

func (t *Target) preparePreload(ctx context.Context, library string) error {
	var err error
	t.dir, err = os.MkdirTemp("", "host-shell-dns-")
	if err != nil {
		return err
	}
	if strings.ContainsAny(t.dir, " :\t\r\n") {
		return fmt.Errorf("remote DNS requires a temporary directory path without whitespace or colons")
	}
	t.Library, err = prepare(ctx, t.dir, library)
	if err != nil {
		return err
	}
	t.Socket = filepath.Join(t.dir, "dns.sock")
	t.listener, err = net.Listen("unix", t.Socket)
	if err == nil {
		err = os.Chmod(t.Socket, 0600)
	}
	return err
}

// ask resolves host for family ("0", "4" or "6") through the attached bridge and returns its reply
// ("OK <n> <canonical>" followed by "<family> <address>" lines, or "ERR <code>").
func (t *Target) ask(ctx context.Context, family, host string) (string, error) {
	t.mu.Lock()
	c := t.conn
	t.mu.Unlock()
	if c == nil {
		return "", errors.New("bridge not attached")
	}
	ctx, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	remote, err := c.Dial(ctx, channel.KindDNS, host, 53, 0)
	if err != nil {
		return "", err
	}
	defer remote.Close()
	remote.SetDeadline(time.Now().Add(timeout))
	if _, err = fmt.Fprintln(remote, family); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(remote, 8192))
	if err != nil && len(b) == 0 {
		return "", err
	}
	return string(b), nil
}

func (t *Target) Register(opt *channel.Options) {
	opt.Enable(channel.RemoteDNS)
	opt.OnAttach(func(c *channel.Conn) {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return
		}
		t.conn = c
		if t.listener != nil {
			t.wg.Add(1)
			go t.servePreload()
		}
		if t.stub != nil {
			t.stub.serve(t.ctx, &t.wg)
		}
		t.mu.Unlock()
		go func() { <-c.Done(); t.Close() }()
	})
}

// servePreload answers the preload adapter's line protocol on the private Unix socket.
func (t *Target) servePreload() {
	defer t.wg.Done()
	slots := make(chan struct{}, 64)
	for {
		local, err := t.listener.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			local.Close()
			continue
		}
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer func() { <-slots }()
			defer local.Close()
			stopClose := context.AfterFunc(t.ctx, func() { local.Close() })
			defer stopClose()
			local.SetDeadline(time.Now().Add(timeout))
			line, err := bufio.NewReader(io.LimitReader(local, 280)).ReadString('\n')
			if err != nil {
				return
			}
			family, host, ok := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
			if !ok || (family != "0" && family != "4" && family != "6") || !channel.ValidDestination(host, 53) {
				io.WriteString(local, "ERR NONAME\n")
				return
			}
			reply, err := t.ask(t.ctx, family, host)
			if err != nil {
				io.WriteString(local, "ERR AGAIN\n")
				return
			}
			io.WriteString(local, reply)
		}()
	}
}

func (t *Target) Close() {
	t.once.Do(func() {
		// Restore the OS resolver first so lookups stop reaching a server that is going away.
		if t.undo != nil {
			t.undo()
		}
		t.mu.Lock()
		t.closed = true
		if t.cancel != nil {
			t.cancel()
		}
		if t.listener != nil {
			t.listener.Close()
		}
		if t.stub != nil {
			t.stub.close()
		}
		t.mu.Unlock()
		t.wg.Wait()
		if t.dir != "" {
			os.RemoveAll(t.dir)
		}
	})
}

// Environment adds the adapter while preserving other preload libraries. Without a preload adapter
// (the loopback DNS server alone) the environment is unchanged.
func (t *Target) Environment(base []string) []string {
	if t.Library == "" {
		return base
	}
	out := make([]string, 0, len(base)+3)
	preload := t.Library
	for _, kv := range base {
		key, value, _ := strings.Cut(kv, "=")
		switch key {
		case "HOST_SHELL_DNS_SOCKET", "HOST_SHELL_DNS_LIBRARY":
			continue
		case "LD_PRELOAD":
			if value != "" {
				preload += " " + value
			}
		default:
			out = append(out, kv)
		}
	}
	return append(out, "LD_PRELOAD="+preload, "HOST_SHELL_DNS_SOCKET="+t.Socket, "HOST_SHELL_DNS_LIBRARY="+t.Library)
}
