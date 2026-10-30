package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
	"github.com/hondarer/host-shell-mcp/internal/config"
	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/server"
)

// connectBridge connects "agent - bridge - host-shell on the target" over in-memory transports.
func connectBridge(t *testing.T, remotePolicy policy.Options, opt Options, copts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Default()
	cfg.LogFile = ""
	cfg.Policy = remotePolicy
	al, _ := audit.Open("")
	s, closeServer, err := server.New(cfg, al)
	if err != nil {
		t.Fatal(err)
	}
	rs, rc := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, rs, nil)
	if err != nil {
		t.Fatal(err)
	}
	ls, lc := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt, rc, ls) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, copts).Connect(ctx, lc, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("bridge did not stop")
		}
		ss.Close()
		closeServer()
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

func TestRelay(t *testing.T) {
	cs := connectBridge(t, policy.Options{}, Options{}, nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, n := range []string{"sys_info", "exec_run", "fs_read", "net_http", "transfer_push", "transfer_pull"} {
		if !names[n] {
			t.Errorf("tool %s missing", n)
		}
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "transfer_push") {
		t.Error("instructions do not mention transfers")
	}
	info := call[struct {
		OS string `json:"os"`
	}](t, cs, "sys_info", nil)
	if info.OS != runtime.GOOS {
		t.Errorf("sys_info = %+v", info)
	}
	rr, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "host-shell://help/transfer"})
	if err != nil || !strings.Contains(rr.Contents[0].Text, "transfer_pull") {
		t.Errorf("resource: %v", err)
	}
}

func TestRelayProgressAndConfirm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	var progress, asked atomic.Int32
	cs := connectBridge(t, policy.Options{Confirm: "fs_remove"}, Options{}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(context.Context, *mcp.ProgressNotificationClientRequest) { progress.Add(1) },
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			asked.Add(1)
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": true}}, nil
		},
	})
	p := &mcp.CallToolParams{Name: "exec_run", Arguments: map[string]any{"command": "echo a; sleep 2.2; echo b"}}
	p.SetProgressToken(1)
	if _, err := cs.CallTool(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if progress.Load() == 0 {
		t.Error("no progress notifications were relayed")
	}
	f := filepath.Join(t.TempDir(), "x")
	os.WriteFile(f, nil, 0o644)
	call[map[string]any](t, cs, "fs_remove", map[string]any{"path": f})
	if asked.Load() != 1 {
		t.Errorf("asked %d times", asked.Load())
	}
	if _, err := os.Stat(f); err == nil {
		t.Error("file was not removed")
	}
}

type transferOut struct {
	Transferred int `json:"transferred"`
	Unchanged   int `json:"unchanged"`
	Failed      int `json:"failed"`
	Files       []struct {
		Path   string `json:"path"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"files"`
}

func TestTransfer(t *testing.T) {
	cs := connectBridge(t, policy.Options{}, Options{}, nil)
	src := t.TempDir()
	big := make([]byte, 2<<20+12345) // large enough to be sent in three 1 MiB chunks
	rand.Read(big)
	files := map[string][]byte{
		"a.txt":         []byte("hello\n"),
		"empty":         {},
		"sub/deep/b.go": []byte("package b\n"),
		"big.bin":       big,
		".git/HEAD":     []byte("ref\n"),
	}
	for name, data := range files {
		p := filepath.Join(src, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, data, 0o644)
	}
	if runtime.GOOS != "windows" {
		os.Chmod(filepath.Join(src, "a.txt"), 0o755)
	}
	remote := filepath.Join(t.TempDir(), "remote")

	out := call[transferOut](t, cs, "transfer_push", map[string]any{"local_path": src, "remote_path": remote, "exclude": []string{".git"}})
	if out.Transferred != 4 || out.Failed != 0 {
		t.Fatalf("push: %+v", out)
	}
	for name, data := range files {
		got, err := os.ReadFile(filepath.Join(remote, filepath.FromSlash(name)))
		if name == ".git/HEAD" {
			if err == nil {
				t.Error("excluded file was sent")
			}
			continue
		}
		if !bytes.Equal(got, data) {
			t.Errorf("%s differs after push (%d bytes, want %d)", name, len(got), len(data))
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(remote, "a.txt")); fi.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v", fi.Mode())
		}
	}

	// Sending again skips unchanged files. A different file is not sent without overwrite.
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("changed\n"), 0o644)
	out = call[transferOut](t, cs, "transfer_push", map[string]any{"local_path": src, "remote_path": remote, "exclude": []string{".git"}})
	if out.Unchanged != 3 || out.Failed != 1 {
		t.Errorf("second push: %+v", out)
	}
	out = call[transferOut](t, cs, "transfer_push", map[string]any{"local_path": src, "remote_path": remote, "exclude": []string{".git"}, "overwrite": true, "dry_run": true})
	if out.Transferred != 1 {
		t.Errorf("dry run: %+v", out)
	}
	if b, _ := os.ReadFile(filepath.Join(remote, "a.txt")); string(b) != "hello\n" {
		t.Error("dry run changed the file")
	}
	out = call[transferOut](t, cs, "transfer_push", map[string]any{"local_path": src, "remote_path": remote, "exclude": []string{".git"}, "overwrite": true})
	if out.Transferred != 1 || out.Failed != 0 {
		t.Errorf("overwrite: %+v", out)
	}

	// Send a single file into an existing directory.
	out = call[transferOut](t, cs, "transfer_push", map[string]any{"local_path": filepath.Join(src, "sub", "deep", "b.go"), "remote_path": remote + "/"})
	if _, err := os.Stat(filepath.Join(remote, "b.go")); err != nil || out.Transferred != 1 {
		t.Errorf("single file into dir: %+v %v", out, err)
	}

	// Pull it back.
	back := filepath.Join(t.TempDir(), "back")
	out = call[transferOut](t, cs, "transfer_pull", map[string]any{"local_path": back, "remote_path": remote})
	if out.Failed != 0 || out.Transferred != 5 {
		t.Fatalf("pull: %+v", out)
	}
	if got, _ := os.ReadFile(filepath.Join(back, "big.bin")); !bytes.Equal(got, big) {
		t.Error("big.bin differs after pull")
	}
	out = call[transferOut](t, cs, "transfer_pull", map[string]any{"local_path": back, "remote_path": remote})
	if out.Unchanged != 5 {
		t.Errorf("second pull: %+v", out)
	}
	single := filepath.Join(t.TempDir(), "one.txt")
	call[transferOut](t, cs, "transfer_pull", map[string]any{"local_path": single, "remote_path": filepath.Join(remote, "a.txt")})
	if b, _ := os.ReadFile(single); string(b) != "changed\n" {
		t.Errorf("single pull = %q", b)
	}
}

func TestLocalPolicy(t *testing.T) {
	allowed := t.TempDir()
	cs := connectBridge(t, policy.Options{}, Options{AllowLocalPaths: []string{allowed}}, nil)
	other := filepath.Join(t.TempDir(), "f")
	os.WriteFile(other, []byte("x"), 0o644)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "transfer_push", Arguments: map[string]any{"local_path": other, "remote_path": filepath.Join(t.TempDir(), "f")}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "policy:") {
		t.Errorf("push outside allowed path: %+v", res.Content)
	}
}

func TestTransferParallel(t *testing.T) {
	cs := connectBridge(t, policy.Options{}, Options{}, nil)
	src := t.TempDir()
	for i := 0; i < 40; i++ {
		os.WriteFile(filepath.Join(src, fmt.Sprintf("f%02d.txt", i)), []byte(strings.Repeat("x", i)), 0o644)
	}
	big := make([]byte, 3<<20)
	rand.Read(big)
	os.WriteFile(filepath.Join(src, "big.bin"), big, 0o644)

	var lists [][]string
	for _, parallel := range []int{1, 4} {
		remote := filepath.Join(t.TempDir(), "remote")
		out := call[transferOut](t, cs, "transfer_push", map[string]any{"local_path": src, "remote_path": remote, "parallel": parallel})
		if out.Transferred != 41 || out.Failed != 0 {
			t.Fatalf("parallel %d: %+v", parallel, out)
		}
		var paths []string
		for _, f := range out.Files {
			paths = append(paths, f.Path)
		}
		lists = append(lists, paths)
		if got, _ := os.ReadFile(filepath.Join(remote, "big.bin")); !bytes.Equal(got, big) {
			t.Errorf("parallel %d: big.bin differs", parallel)
		}
		back := filepath.Join(t.TempDir(), "back")
		if out := call[transferOut](t, cs, "transfer_pull", map[string]any{"local_path": back, "remote_path": remote, "parallel": parallel}); out.Transferred != 41 || out.Failed != 0 {
			t.Errorf("pull parallel %d: %+v", parallel, out)
		}
	}
	if !slices.Equal(lists[0], lists[1]) {
		t.Errorf("order differs:\n%v\n%v", lists[0], lists[1])
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "transfer_push", Arguments: map[string]any{"local_path": src, "remote_path": t.TempDir(), "parallel": 17}})
	if err != nil || !res.IsError {
		t.Errorf("parallel 17: %v %+v", err, res)
	}
}
