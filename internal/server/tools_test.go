package server

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type editOut struct {
	Replacements int    `json:"replacements"`
	Lines        []int  `json:"lines"`
	Snippet      string `json:"snippet"`
	Charset      string `json:"charset"`
	Newline      string `json:"newline"`
}

func TestEdit(t *testing.T) {
	cs := connect(t)
	path := filepath.Join(t.TempDir(), "a.c")
	orig := "int a = 1;\r\nint b = 1;\r\n\r\nint main() {\r\n\treturn a;\r\n}\r\n"
	os.WriteFile(path, []byte(orig), 0o644)

	// LF で指定しても CRLF のファイルに一致し、CRLF のまま書き戻す。
	out := call[editOut](t, cs, "fs_edit", map[string]any{"path": path,
		"old_string": "int main() {\n\treturn a;\n", "new_string": "int main() {\n\tint c = a + b;\n\treturn c;\n"})
	want := "int a = 1;\r\nint b = 1;\r\n\r\nint main() {\r\n\tint c = a + b;\r\n\treturn c;\r\n}\r\n"
	if b, _ := os.ReadFile(path); string(b) != want {
		t.Fatalf("file = %q", b)
	}
	if out.Replacements != 1 || out.Lines[0] != 4 || out.Newline != "crlf" || !strings.HasSuffix(out.Snippet, "     5\t\tint c = a + b;\n     6\t\treturn c;\n     7\t}\n") {
		t.Fatalf("fs_edit got %+v", out)
	}

	// 複数の箇所に一致する場合は、その行を示してエラーにする。
	if msg := callErr(t, cs, "fs_edit", map[string]any{"path": path, "old_string": "= 1;", "new_string": "= 2;"}); !strings.Contains(msg, "lines 1, 2") {
		t.Fatalf("error = %s", msg)
	}
	// 空白だけが違う候補があれば、その行を示す。
	if msg := callErr(t, cs, "fs_edit", map[string]any{"path": path, "old_string": "int  b = 1;", "new_string": "x"}); !strings.Contains(msg, "line 2") {
		t.Fatalf("error = %s", msg)
	}
	// 途中の置換が失敗した場合は、何も書き込まない。
	before, _ := os.ReadFile(path)
	callErr(t, cs, "fs_edit", map[string]any{"path": path, "edits": []map[string]any{
		{"old_string": "int a", "new_string": "long a"}, {"old_string": "missing", "new_string": "x"}}})
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("file changed by a failed edit")
	}
	// 複数の置換と replace_all。
	out = call[editOut](t, cs, "fs_edit", map[string]any{"path": path, "edits": []map[string]any{
		{"old_string": "= 1;", "new_string": "= 2;", "replace_all": true}, {"old_string": "return c;", "new_string": "return 0;"}}})
	if out.Replacements != 3 || len(out.Lines) != 3 {
		t.Fatalf("fs_edit got %+v", out)
	}
	// dry_run は書き込まない。
	before, _ = os.ReadFile(path)
	call[editOut](t, cs, "fs_edit", map[string]any{"path": path, "old_string": "return 0;", "new_string": "return 1;", "dry_run": true})
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("dry_run changed the file")
	}
}

func TestEditKeepsEncoding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses locale environment variables")
	}
	t.Setenv("LC_ALL", "ja_JP.SJIS")
	cs := connect(t)
	path := filepath.Join(t.TempDir(), "s.txt")
	os.WriteFile(path, []byte("\x93\xfa\x96\x7b\x8c\xea\n"), 0o644) // 日本語 (Shift_JIS)
	out := call[editOut](t, cs, "fs_edit", map[string]any{"path": path, "old_string": "本", "new_string": "英"})
	if b, _ := os.ReadFile(path); string(b) != "\x93\xfa\x89\x70\x8c\xea\n" || out.Charset != "shift_jis" {
		t.Fatalf("file = %x, out = %+v", b, out)
	}
}

func TestReadLineNumbers(t *testing.T) {
	cs := connect(t)
	path := filepath.Join(t.TempDir(), "n.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0o644)
	r := call[struct {
		Content string `json:"content"`
	}](t, cs, "fs_read", map[string]any{"path": path, "line_start": 2, "line_numbers": true})
	if r.Content != "     2\tb\n     3\tc\n" {
		t.Fatalf("content = %q", r.Content)
	}
	callErr(t, cs, "fs_read", map[string]any{"path": path, "line_numbers": true})
}

func TestFileOperations(t *testing.T) {
	cs := connect(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")

	mk := call[struct {
		Created bool `json:"created"`
	}](t, cs, "fs_mkdir", map[string]any{"path": filepath.Join(src, "sub")})
	if !mk.Created {
		t.Fatal("fs_mkdir did not create")
	}
	os.WriteFile(filepath.Join(src, "sub", "f.txt"), []byte("data"), 0o644)

	// ディレクトリのコピーには recursive が必要。
	callErr(t, cs, "fs_copy", map[string]any{"source": src, "destination": filepath.Join(dir, "copy")})
	cp := call[struct {
		Files int   `json:"files"`
		Bytes int64 `json:"bytes"`
	}](t, cs, "fs_copy", map[string]any{"source": src, "destination": filepath.Join(dir, "copy"), "recursive": true})
	if cp.Files != 1 || cp.Bytes != 4 {
		t.Fatalf("fs_copy got %+v", cp)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "copy", "sub", "f.txt")); string(b) != "data" {
		t.Fatalf("copied content = %q", b)
	}

	// 移動先が存在する場合は overwrite が必要。
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o644)
	callErr(t, cs, "fs_move", map[string]any{"source": filepath.Join(dir, "other.txt"), "destination": filepath.Join(dir, "copy", "sub", "f.txt")})
	call[map[string]any](t, cs, "fs_move", map[string]any{"source": filepath.Join(dir, "copy"), "destination": filepath.Join(dir, "moved")})
	if _, err := os.Stat(filepath.Join(dir, "moved", "sub", "f.txt")); err != nil {
		t.Fatal(err)
	}

	// 空でないディレクトリの削除には recursive が必要。
	if msg := callErr(t, cs, "fs_remove", map[string]any{"path": filepath.Join(dir, "moved")}); !strings.Contains(msg, "recursive") {
		t.Fatalf("error = %s", msg)
	}
	call[map[string]any](t, cs, "fs_remove", map[string]any{"path": filepath.Join(dir, "moved"), "recursive": true})
	if _, err := os.Stat(filepath.Join(dir, "moved")); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
	call[map[string]any](t, cs, "fs_remove", map[string]any{"path": filepath.Join(dir, "moved"), "missing_ok": true})
	home, _ := os.UserHomeDir()
	callErr(t, cs, "fs_remove", map[string]any{"path": home, "recursive": true})
}

func TestSearchModes(t *testing.T) {
	cs := connect(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "a"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "a", "x.go"), []byte("one\ntwo\nfoo(1)\nthree\nfoo(2)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "y.go"), []byte("foo(3)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte("foo(4)\n"), 0o644)

	type grepOut struct {
		Matches []struct {
			Line   int      `json:"line"`
			Text   string   `json:"text"`
			Before []string `json:"before"`
			After  []string `json:"after"`
		} `json:"matches"`
		Files []struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
		} `json:"files"`
		FilesMatched int `json:"files_matched"`
	}
	g := call[grepOut](t, cs, "search_grep", map[string]any{"pattern": "foo(1)", "literal": true, "path": dir, "context": 1})
	if len(g.Matches) != 1 || g.Matches[0].Line != 3 || g.Matches[0].Before[0] != "two" || g.Matches[0].After[0] != "three" {
		t.Fatalf("content got %+v", g)
	}
	g = call[grepOut](t, cs, "search_grep", map[string]any{"pattern": `foo\(`, "path": dir, "include": "src/**/*.go", "output_mode": "count"})
	if len(g.Files) != 2 || g.FilesMatched != 2 {
		t.Fatalf("count got %+v", g)
	}
	for _, f := range g.Files {
		if strings.HasSuffix(f.Path, "x.go") && f.Count != 2 {
			t.Fatalf("count got %+v", g)
		}
	}
	g = call[grepOut](t, cs, "search_grep", map[string]any{"pattern": "foo", "path": dir, "output_mode": "files"})
	if len(g.Files) != 3 || g.Matches != nil {
		t.Fatalf("files got %+v", g)
	}

	// 更新日時の新しい順に並べる。
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "src", "a", "x.go"), old, old)
	f := call[struct {
		Paths []string `json:"paths"`
	}](t, cs, "search_files", map[string]any{"pattern": "**/*.go", "path": dir, "sort": "mtime"})
	if len(f.Paths) != 2 || !strings.HasSuffix(f.Paths[1], "x.go") {
		t.Fatalf("search_files got %+v", f)
	}
}

func TestJobStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses cat")
	}
	cs := connect(t)
	type status struct {
		ID        string `json:"id"`
		Running   bool   `json:"running"`
		StdinOpen bool   `json:"stdin_open"`
		ExitCode  *int   `json:"exit_code"`
	}
	st := call[status](t, cs, "job_start", map[string]any{"argv": []string{"cat"}, "stdin_open": true, "stdin": "first\n"})
	if !st.StdinOpen {
		t.Fatalf("job_start got %+v", st)
	}
	call[map[string]any](t, cs, "job_write", map[string]any{"id": st.ID, "input": "second\n", "close": true})
	var r struct {
		Status status `json:"status"`
		Output string `json:"output"`
	}
	for i := 0; i < 10; i++ {
		r = call[struct {
			Status status `json:"status"`
			Output string `json:"output"`
		}](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 2})
		if !r.Status.Running {
			break
		}
	}
	if r.Status.Running || r.Output != "first\nsecond\n" || r.Status.StdinOpen {
		t.Fatalf("job_read got %+v", r)
	}
	callErr(t, cs, "job_write", map[string]any{"id": st.ID, "input": "late\n"})
}

func TestHelp(t *testing.T) {
	cs := connect(t)
	ctx := context.Background()
	if ins := cs.InitializeResult().Instructions; !strings.Contains(ins, "fs_edit") || !strings.Contains(ins, "help") {
		t.Fatalf("instructions = %s", ins)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "help", Arguments: map[string]any{"topic": "edit"}})
	if err != nil || res.IsError {
		t.Fatal(err, res)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "old_string") {
		t.Fatalf("help text = %s", text)
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "help", Arguments: map[string]any{}})
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "# This host") {
		t.Fatalf("overview = %s", text)
	}
	callErr(t, cs, "help", map[string]any{"topic": "nope"})

	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "host-shell://help/search"})
	if err != nil || !strings.Contains(rr.Contents[0].Text, "output_mode") {
		t.Fatal(err, rr)
	}
}
