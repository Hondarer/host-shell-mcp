package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
)

// connect はメモリ上の通信路でサーバーとクライアントをつなぎます。
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	cfg := config.Default()
	cfg.LogFile = filepath.Join(t.TempDir(), "audit.log")
	al, err := audit.Open(cfg.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	s, closeServer := New(cfg, al)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		ss.Wait()
		closeServer()
		al.Close()
	})
	return cs
}

func call[T any](t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s returned error: %v", name, res.Content[0].(*mcp.TextContent).Text)
	}
	var out T
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestListTools(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"exec_run", "job_start", "job_read", "fs_read", "fs_write", "search_grep", "fs_manifest", "sys_info"} {
		if !names[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestExecRun(t *testing.T) {
	cs := connect(t)
	cmd := "echo hello; exit 3"
	if runtime.GOOS == "windows" {
		cmd = "Write-Output hello; exit 3"
	}
	out := call[struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}](t, cs, "exec_run", map[string]any{"command": cmd})
	if out.ExitCode != 3 || strings.TrimSpace(out.Stdout) != "hello" {
		t.Fatalf("got %+v", out)
	}
}

func TestWriteReadAndGrep(t *testing.T) {
	cs := connect(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": path, "content": "one\ntwo\nthree\n"})

	read := call[struct {
		Content  string `json:"content"`
		NextLine int    `json:"next_line"`
	}](t, cs, "fs_read", map[string]any{"path": path, "line_start": 2, "line_count": 1})
	if read.Content != "two\n" || read.NextLine != 3 {
		t.Fatalf("fs_read got %+v", read)
	}

	grep := call[struct {
		Matches []struct {
			Line int `json:"line"`
		} `json:"matches"`
	}](t, cs, "search_grep", map[string]any{"pattern": "^th", "path": dir})
	if len(grep.Matches) != 1 || grep.Matches[0].Line != 3 {
		t.Fatalf("search_grep got %+v", grep)
	}

	if b, _ := os.ReadFile(path); string(b) != "one\ntwo\nthree\n" {
		t.Fatalf("file content = %q", b)
	}
}
