package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/server"
	"github.com/hondarer/host-shell-mcp/internal/session"
)

const targetArg = "hs-cli-test-target"

// TestMain lets the test binary act as the target host-shell --mcp (with targetArg) and as the
// background process of host-shell session start --detach, which runs this executable again.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case targetArg:
			os.Exit(runTarget())
		case "session":
			os.Exit(session.Main(os.Args[2:], os.Stdout, os.Stderr))
		}
	}
	os.Exit(m.Run())
}

func runTarget() int {
	cfg := config.Default()
	cfg.LogFile = ""
	al, _ := audit.Open("")
	pol, err := server.NewPolicy(cfg, nil, al)
	if err != nil {
		return 1
	}
	s, closeServer := server.New(cfg, server.Session{Policy: pol, Audit: al})
	defer closeServer()
	_ = s.Run(context.Background(), &mcp.StdioTransport{})
	return 0
}

func shortDir(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv(session.EnvDir, dir)
	t.Setenv(session.EnvName, "")
}

func targetCommand(t *testing.T) []string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{self, targetArg}
}

type result struct {
	code           int
	stdout, stderr string
}

func call(stdin string, args ...string) result {
	var out, errOut bytes.Buffer
	code := Call(args, strings.NewReader(stdin), &out, &errOut)
	return result{code, out.String(), errOut.String()}
}

func tools(args ...string) result {
	var out, errOut bytes.Buffer
	code := Tools(args, &out, &errOut)
	return result{code, out.String(), errOut.String()}
}

func sessionCmd(args ...string) result {
	var out, errOut bytes.Buffer
	code := session.Main(args, &out, &errOut)
	return result{code, out.String(), errOut.String()}
}

func waitReady(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := session.Status(name); err == nil && st.State == "ready" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %q did not become ready", name)
}

func TestCallAndTools(t *testing.T) {
	shortDir(t)
	done := make(chan result, 1)
	go func() { done <- sessionCmd(append([]string{"start", "--name", "e", "--"}, targetCommand(t)...)...) }()
	waitReady(t, "e")
	t.Setenv(session.EnvName, "e")

	if r := call("", "sys_get_info"); r.code != 0 || !strings.Contains(r.stdout, `"os":`) || strings.Count(r.stdout, "\n") != 1 {
		t.Errorf("sys_get_info: %+v", r)
	}
	f := filepath.Join(t.TempDir(), "a.txt")
	src := filepath.Join(t.TempDir(), "src.txt")
	os.WriteFile(src, []byte("from a file <&>\n"), 0o644)
	if r := call("", "fs_write", "path="+f, "content=@"+src); r.code != 0 {
		t.Fatalf("fs_write: %+v", r)
	}
	r := call(`{"path": "`+filepath.ToSlash(f)+`"}`, "fs_read", "--json", "-", "--quiet")
	var read struct {
		Content string `json:"content"`
	}
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &read) != nil || read.Content != "from a file <&>\n" {
		t.Errorf("fs_read: %+v", r)
	}
	if r := call("", "fs_read", "path="+filepath.Join(t.TempDir(), "missing")); r.code != exitToolError || r.stderr == "" || r.stdout != "" {
		t.Errorf("fs_read of a missing file: %+v", r)
	}
	if r := call("", "no_such_tool"); r.code != exitUsage || !strings.Contains(r.stderr, "host-shell tools") {
		t.Errorf("unknown tool: %+v", r)
	}
	if r := call("", "fs_read", "bogus=1"); r.code != exitUsage || !strings.Contains(r.stderr, "path") {
		t.Errorf("unknown argument: %+v", r)
	}
	if r := call("", "--session", "e", "fs_read"); r.code != exitUsage || !strings.Contains(r.stderr, "TOOL must come first") {
		t.Errorf("options before TOOL: %+v", r)
	}
	if r := call("", "sys_get_info", "--session", "nobody"); r.code != exitSession || !strings.Contains(r.stderr, "no session \"nobody\"") {
		t.Errorf("missing session: %+v", r)
	}

	if r := tools(); r.code != 0 || !strings.Contains(r.stdout, "exec_run ") || !strings.Contains(r.stdout, "transfer_push ") {
		t.Errorf("tools: %+v", r)
	}
	var schemas []struct {
		Name        string         `json:"name"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if r := tools("--schema", "fs_read"); r.code != 0 || json.Unmarshal([]byte(r.stdout), &schemas) != nil || len(schemas) != 1 || schemas[0].InputSchema["properties"] == nil {
		t.Errorf("tools --schema: %+v", r)
	}
	if r := tools("--instructions"); r.code != 0 || !strings.Contains(r.stdout, "transfer_push") {
		t.Errorf("tools --instructions: %+v", r)
	}
	if r := tools("nosuch"); r.code != exitUsage {
		t.Errorf("tools nosuch: %+v", r)
	}

	if r := sessionCmd("stop"); r.code != 0 {
		t.Fatalf("stop: %+v", r)
	}
	select {
	case r := <-done:
		if r.code != 0 || !strings.Contains(r.stderr, "is ready") {
			t.Errorf("session start: %+v", r)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("session did not stop")
	}
	if r := call("", "sys_get_info"); r.code != exitSession {
		t.Errorf("call after stop: %+v", r)
	}
}

func TestDetach(t *testing.T) {
	shortDir(t)
	r := sessionCmd(append([]string{"start", "--detach", "--name", "d", "--idle-timeout", "10m", "--"}, targetCommand(t)...)...)
	var st session.State
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &st) != nil || st.Mode != "detached" || st.State != "ready" || st.Confirm != "refuse" || st.PID == os.Getpid() {
		t.Fatalf("start --detach: %+v", r)
	}
	t.Cleanup(func() { sessionCmd("stop", "--name", "d") })
	if r := sessionCmd(append([]string{"start", "--detach", "--name", "d", "--"}, targetCommand(t)...)...); r.code == 0 || !strings.Contains(r.stderr, "already running") {
		t.Errorf("second start: %+v", r)
	}
	if r := call("", "sys_get_info", "--session", "d"); r.code != 0 {
		t.Errorf("call: %+v", r)
	}
	if r := sessionCmd("list"); r.code != 0 || !strings.Contains(r.stdout, `"name":"d"`) {
		t.Errorf("list: %+v", r)
	}
	if r := sessionCmd("stop", "--name", "d"); r.code != 0 {
		t.Errorf("stop: %+v", r)
	}
	if r := sessionCmd("status", "--name", "d"); r.code == 0 {
		t.Errorf("status after stop: %+v", r)
	}
}

func TestDetachFailure(t *testing.T) {
	shortDir(t)
	r := sessionCmd("start", "--detach", "--name", "f", "--", filepath.Join(t.TempDir(), "no-such-command"))
	if r.code != 1 || !strings.Contains(r.stderr, "ended before it was ready") {
		t.Errorf("start --detach with a bad command: %+v", r)
	}
}
