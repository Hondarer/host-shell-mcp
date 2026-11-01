// Package testutil holds helpers shared by tests across packages. It must not import packages
// whose internal tests use it.
package testutil

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/askpass"
)

// Main runs the tests of a package whose tests start exec_sudo: the test binary then also acts
// as the sudo -A askpass helper, as host-shell does.
func Main(m *testing.M) {
	if ran, code := askpass.Serve(); ran {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// Connect serves srv over in-memory transports and returns a connected client session that is
// closed when the test ends.
func Connect(t *testing.T, srv *mcp.Server, copts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, copts).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		ss.Wait()
	})
	return cs
}

// Text returns the first text content of a tool result.
func Text(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// Call calls a tool, fails the test on an error result, and decodes the structured output.
func Call[T any](t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s returned error: %v", name, Text(res))
	}
	var out T
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// CallErr calls a tool, checks that it fails and returns the error text.
func CallErr(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("%s %v should fail", name, args)
	}
	return Text(res)
}

// ToolNames lists the session's tools in the order the server returns them.
func ToolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// ToolSet returns the session's tool names as a set.
func ToolSet(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	for _, n := range ToolNames(t, cs) {
		set[n] = true
	}
	return set
}

// FreePort returns a loopback TCP port that was free a moment ago; nothing listens on it.
func FreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// AuditFile resolves the daily file the audit logger writes for base.
func AuditFile(t *testing.T, base string) string {
	t.Helper()
	m, _ := filepath.Glob(strings.TrimSuffix(base, ".log") + "*")
	if len(m) == 0 {
		t.Fatal("no audit file")
	}
	return m[0]
}

// WaitFor polls cond every 5ms until it holds, failing the test with msg after timeout.
func WaitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
