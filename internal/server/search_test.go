package server

import (
	"os"
	"path/filepath"
	"testing"
)

type grepOut struct {
	Matches []struct {
		Line    int      `json:"line"`
		EndLine int      `json:"end_line"`
		Text    string   `json:"text"`
		Before  []string `json:"before"`
		After   []string `json:"after"`
	} `json:"matches"`
	FilesScanned int `json:"files_scanned"`
	SkippedLarge int `json:"skipped_large"`
}

func TestGrepMultiline(t *testing.T) {
	cs := connect(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc a() {\n\treturn 1\n}\n\nfunc b() {\n}\n"), 0o644)

	out := call[grepOut](t, cs, "search_grep", map[string]any{"pattern": `func a\(\) \{.*?\n\}`, "path": dir, "multiline": true, "context": 1})
	if len(out.Matches) != 1 {
		t.Fatalf("got %+v", out)
	}
	m := out.Matches[0]
	if m.Line != 3 || m.EndLine != 5 || m.Text != "func a() {\n\treturn 1\n}" || len(m.Before) != 1 || m.Before[0] != "" || len(m.After) != 1 || m.After[0] != "" {
		t.Errorf("match = %+v", m)
	}
	// ^ and $ match at line boundaries.
	out = call[grepOut](t, cs, "search_grep", map[string]any{"pattern": `^\treturn 1$`, "path": dir, "multiline": true})
	if len(out.Matches) != 1 || out.Matches[0].Line != 4 || out.Matches[0].EndLine != 4 {
		t.Errorf("anchored match = %+v", out.Matches)
	}
	// Without multiline the pattern spanning lines does not match.
	out = call[grepOut](t, cs, "search_grep", map[string]any{"pattern": `func a\(\) \{\n`, "path": dir})
	if len(out.Matches) != 0 {
		t.Errorf("line mode matched %+v", out.Matches)
	}

	// Files over 32 MiB are skipped with multiline, and counted.
	big, _ := os.Create(filepath.Join(dir, "big.txt"))
	big.Truncate(33 << 20)
	big.Close()
	out = call[grepOut](t, cs, "search_grep", map[string]any{"pattern": "x", "path": dir, "multiline": true, "max_file_bytes": 64 << 20})
	if out.SkippedLarge != 1 || out.FilesScanned != 1 {
		t.Errorf("large file: %+v", out)
	}
}
