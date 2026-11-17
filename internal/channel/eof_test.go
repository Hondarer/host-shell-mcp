package channel

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) Close() error { return nil }
func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// runToEOF serves requests that end with the end of input and returns the output and the
// number of unanswered requests.
func runToEOF(t *testing.T, opt EOFOptions) (string, int) {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "slow"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		select {
		case <-time.After(300 * time.Millisecond):
			return nil, map[string]string{"result": "done"}, nil
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	})
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"slow","arguments":{}}}`,
	}, "\n") + "\n"
	out := &lockedBuffer{}
	tr := TrackRequests(&mcp.IOTransport{Reader: io.NopCloser(strings.NewReader(in)), Writer: out}, opt)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.Run(ctx, tr)
	return out.String(), tr.Unanswered()
}

func TestFinishOnEOF(t *testing.T) {
	out, n := runToEOF(t, EOFOptions{Finish: true})
	if n != 0 || !strings.Contains(out, `"id":1`) || !strings.Contains(out, `"result":"done"`) {
		t.Errorf("unanswered %d, output %q", n, out)
	}
}

func TestEOFCancelsByDefault(t *testing.T) {
	out, n := runToEOF(t, EOFOptions{})
	if n == 0 || strings.Contains(out, `"result":"done"`) {
		t.Errorf("unanswered %d, output %q", n, out)
	}
}
