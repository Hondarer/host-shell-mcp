package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// Cancellation must interrupt a read even if the peer stays silent and idle_ms is long.
func TestNetExchangeCancellation(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "tcp"
		if secure {
			name = "tls"
		}
		t.Run(name, func(t *testing.T) {
			requested := make(chan struct{})
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(requested)
				<-r.Context().Done()
			}))
			if secure {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			d := &Deps{Cfg: config.Default(), Policy: policy.Unrestricted()}
			done := make(chan struct{})
			go func() {
				defer close(done)
				d.netExchange(ctx, nil, NetExchangeInput{
					Host: "127.0.0.1", Port: srv.Listener.Addr().(*net.TCPAddr).Port,
					TLS: secure, Insecure: secure, IdleMs: 60000,
					Send: "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
				})
			}()
			select {
			case <-requested:
			case <-done:
				t.Fatal("exchange ended before sending the request")
			case <-ctx.Done():
				t.Fatal("server did not receive the request")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("cancellation did not interrupt the exchange")
			}
		})
	}
}
