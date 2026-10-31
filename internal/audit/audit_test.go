package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDailyFilesAndPruning(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "audit.log")
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	clock := func() time.Time { return day }
	other := filepath.Join(dir, "audit-notes.log") // non-dated filename
	os.WriteFile(other, []byte("keep"), 0o600)
	os.WriteFile(filepath.Join(dir, "audit.log"), []byte("old"), 0o600) // undated file

	l, err := open(base, clock)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		l.Event("tick", "i", i)
		day = day.AddDate(0, 0, 1)
	}
	l.Event("last")

	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{
		"audit-2026-10-04.log", "audit-2026-10-05.log", "audit-2026-10-06.log", "audit-2026-10-07.log",
		"audit-2026-10-08.log", "audit-2026-10-09.log", "audit-notes.log", "audit.log",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "audit-2026-10-09.log")); len(lines(t, b)) != 1 {
		t.Errorf("today's file = %q", b)
	}
}

func TestConcurrentWriters(t *testing.T) {
	base := filepath.Join(t.TempDir(), "audit.log")
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		l, err := Open(base)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				l.Tool("exec_run", map[string]any{"command": "echo hello", "i": i}, time.Millisecond, false, nil)
			}
		}()
	}
	wg.Wait()
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(base), "audit-*.log"))
	total := 0
	for _, m := range matches {
		b, _ := os.ReadFile(m)
		total += len(lines(t, b))
	}
	if total != 1000 {
		t.Errorf("%d records, want 1000", total)
	}
}

// lines parses JSON Lines and fails on any line that is not a JSON object.
func lines(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("broken line %q: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

func TestSessionSecretMask(t *testing.T) {
	dir := t.TempDir()
	l, e := Open(filepath.Join(dir, "audit.log"))
	if e != nil {
		t.Fatal(e)
	}
	secret := "session-secret-value"
	l.SetMask(func(s string) string { return strings.ReplaceAll(s, secret, "[REDACTED]") })
	l.Tool("test", map[string]string{"proxy": secret}, 0, false, errors.New(secret))
	l.Event("server_stop", "error", secret)
	paths, _ := filepath.Glob(filepath.Join(dir, "audit-*.log"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	b, e := os.ReadFile(paths[0])
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte(secret)) || !bytes.Contains(b, []byte("[REDACTED]")) {
		t.Fatalf("secret mask failed")
	}
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if !json.Valid(line) {
			t.Fatal("invalid audit JSON")
		}
	}
}
