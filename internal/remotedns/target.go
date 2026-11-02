package remotedns

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// Target owns a private per-session resolver socket and native adapter.
type Target struct {
	Library, Socket string
	dir             string
	listener        net.Listener
	cancel          context.CancelFunc
	once            sync.Once
	mu              sync.Mutex
	closed          bool
	wg              sync.WaitGroup
}

// NewTarget prepares the native hook; a supplied library must match the target ABI.
func NewTarget(ctx context.Context, library string) (*Target, error) {
	t := &Target{}
	var err error
	t.dir, err = os.MkdirTemp("", "host-shell-dns-")
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(t.dir, " :\t\r\n") {
		t.Close()
		return nil, fmt.Errorf("remote DNS requires a temporary directory path without whitespace or colons")
	}
	t.Library, err = prepare(ctx, t.dir, library)
	if err != nil {
		t.Close()
		return nil, err
	}
	t.Socket = filepath.Join(t.dir, "dns.sock")
	t.listener, err = net.Listen("unix", t.Socket)
	if err == nil {
		err = os.Chmod(t.Socket, 0600)
	}
	if err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (t *Target) Register(opt *channel.Options) {
	opt.Enable(channel.RemoteDNS)
	opt.OnAttach(func(c *channel.Conn) {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.cancel = cancel
		t.wg.Add(1)
		t.mu.Unlock()
		go func() {
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
					stopClose := context.AfterFunc(ctx, func() { local.Close() })
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
					requestCtx, stop := context.WithTimeout(ctx, timeout)
					defer stop()
					remote, err := c.Dial(requestCtx, channel.KindDNS, host, 53, 0)
					if err != nil {
						io.WriteString(local, "ERR AGAIN\n")
						return
					}
					defer remote.Close()
					remote.SetDeadline(time.Now().Add(timeout))
					if _, err = fmt.Fprintln(remote, family); err != nil {
						return
					}
					io.Copy(local, io.LimitReader(remote, 8192))
				}()
			}
		}()
		go func() { <-c.Done(); t.Close() }()
	})
}

func (t *Target) Close() {
	t.once.Do(func() {
		t.mu.Lock()
		t.closed = true
		if t.cancel != nil {
			t.cancel()
		}
		if t.listener != nil {
			t.listener.Close()
		}
		t.mu.Unlock()
		t.wg.Wait()
		if t.dir != "" {
			os.RemoveAll(t.dir)
		}
	})
}

// Environment adds the adapter while preserving other preload libraries.
func (t *Target) Environment(base []string) []string {
	out := make([]string, 0, len(base)+2)
	preload := t.Library
	for _, kv := range base {
		key, value, _ := strings.Cut(kv, "=")
		switch key {
		case "HOST_SHELL_DNS_SOCKET":
			continue
		case "LD_PRELOAD":
			if value != "" {
				preload += " " + value
			}
		default:
			out = append(out, kv)
		}
	}
	return append(out, "LD_PRELOAD="+preload, "HOST_SHELL_DNS_SOCKET="+t.Socket)
}
