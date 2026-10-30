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
	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// connect はメモリ上の通信路でサーバーとクライアントをつなぎます。
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, policy.Options{}, nil)
}

// connectWith は制限とクライアントの設定を指定してつなぎます。
func connectWith(t *testing.T, po policy.Options, copts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	cfg := config.Default()
	cfg.LogFile = filepath.Join(t.TempDir(), "audit.log")
	cfg.Policy = po
	al, err := audit.Open(cfg.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	s, closeServer, err := New(cfg, al)
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
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
		closeServer()
		al.Close()
	})
	return cs
}

// callErr はツールを呼び、エラーになることを確かめてその説明を返します。
func callErr(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("%s %v should fail", name, args)
	}
	return res.Content[0].(*mcp.TextContent).Text
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
	for _, want := range []string{"help", "exec_run", "job_start", "job_read", "job_write", "fs_read", "fs_edit", "fs_write",
		"fs_mkdir", "fs_move", "fs_copy", "fs_remove", "search_grep", "search_files", "fs_manifest", "sys_info"} {
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

func TestWriteAtOffset(t *testing.T) {
	cs := connect(t)
	path := filepath.Join(t.TempDir(), "b.bin")
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": path, "content": "hello world"})

	// 途中の範囲だけを置き換え、ファイルは切り詰めない。
	out := call[struct {
		BytesWritten int   `json:"bytes_written"`
		Size         int64 `json:"size"`
	}](t, cs, "fs_write", map[string]any{"path": path, "content": "V09STEQ=", "encoding": "base64", "offset": 6})
	if out.BytesWritten != 5 || out.Size != 11 {
		t.Fatalf("fs_write got %+v", out)
	}
	if b, _ := os.ReadFile(path); string(b) != "hello WORLD" {
		t.Fatalf("file content = %q", b)
	}

	// 末尾より後ろの位置に書くと、間は 0 で埋まる。
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": path, "content": "!", "offset": 13})
	if b, _ := os.ReadFile(path); string(b) != "hello WORLD\x00\x00!" {
		t.Fatalf("file content = %q", b)
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "fs_write",
		Arguments: map[string]any{"path": path, "content": "x", "offset": 0, "mode": "append"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("offset with mode append should fail")
	}
}

func TestEncodingConversion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and locale environment variables")
	}
	t.Setenv("LC_ALL", "ja_JP.SJIS") // 接続先の既定の文字コードを Shift_JIS にする
	cs := connect(t)
	dir := t.TempDir()

	// コマンドの出力: 行ごとに自動判定する。
	run := call[struct {
		Stdout         string `json:"stdout"`
		StdoutEncoding string `json:"stdout_encoding"`
	}](t, cs, "exec_run", map[string]any{"command": `printf 'utf-8: 日本語\nsjis: \223\372\226\173\214\352\n'`})
	if run.Stdout != "utf-8: 日本語\nsjis: 日本語\n" || run.StdoutEncoding != "shift_jis" {
		t.Fatalf("exec_run got %+v", run)
	}

	// 書き込み: Shift_JIS と CRLF に変換する。
	sjisPath := filepath.Join(dir, "sjis.txt")
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": sjisPath, "content": "一行目\n日本語\n", "charset": "shift_jis", "newline": "crlf"})
	if b, _ := os.ReadFile(sjisPath); !strings.HasSuffix(string(b), "\x93\xfa\x96\x7b\x8c\xea\r\n") {
		t.Fatalf("file content = %x", b)
	}

	// 読み取り: 既定の文字コードとして自動判定し、改行の種類を返す。
	type readOut struct {
		Content    string `json:"content"`
		Charset    string `json:"charset"`
		BOM        bool   `json:"bom"`
		Newline    string `json:"newline"`
		NextOffset int64  `json:"next_offset"`
	}
	for _, args := range []map[string]any{{"path": sjisPath}, {"path": sjisPath, "line_start": 1}} {
		r := call[readOut](t, cs, "fs_read", args)
		if r.Content != "一行目\r\n日本語\r\n" || r.Charset != "shift_jis" || r.Newline != "crlf" {
			t.Fatalf("fs_read %v got %+v", args, r)
		}
	}

	// 検索: Shift_JIS のファイルの日本語に一致する。
	grep := call[struct {
		Matches []Match `json:"matches"`
	}](t, cs, "search_grep", map[string]any{"pattern": "本語", "path": dir})
	if len(grep.Matches) != 1 || grep.Matches[0].Line != 2 || grep.Matches[0].Charset != "shift_jis" {
		t.Fatalf("search_grep got %+v", grep)
	}

	// UTF-16 と BOM: BOM で判定し、BOM は内容に含めない。
	u16Path := filepath.Join(dir, "u16.txt")
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": u16Path, "content": "あ\n", "charset": "utf-16le", "bom": true})
	if b, _ := os.ReadFile(u16Path); string(b) != "\xff\xfe\x42\x30\x0a\x00" {
		t.Fatalf("file content = %x", b)
	}
	if r := call[readOut](t, cs, "fs_read", map[string]any{"path": u16Path}); r.Content != "あ\n" || r.Charset != "utf-16le" || !r.BOM {
		t.Fatalf("fs_read utf-16 got %+v", r)
	}

	// 範囲の末尾で分断された文字は、次の読み取りに回す。
	u8Path := filepath.Join(dir, "u8.txt")
	os.WriteFile(u8Path, []byte("日本語"), 0o644)
	if r := call[readOut](t, cs, "fs_read", map[string]any{"path": u8Path, "length": 4}); r.Content != "日" || r.NextOffset != 3 || r.Charset != "utf-8" {
		t.Fatalf("fs_read range got %+v", r)
	}

	// 表せない文字はエラーにする。
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "fs_write",
		Arguments: map[string]any{"path": filepath.Join(dir, "x.txt"), "content": "😀", "charset": "shift_jis"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("writing an unencodable character should fail")
	}
}

// Match は search_grep の一致の 1 件です。
type Match struct {
	Line    int    `json:"line"`
	Charset string `json:"charset"`
}
