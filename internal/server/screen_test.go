package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPTYScreen(t *testing.T) {
	cs := connect(t)
	screen := func(id, until string) []string {
		t.Helper()
		r := call[jobReadOut](t, cs, "job_read", map[string]any{"id": id, "wait_sec": 20, "until": until, "screen": true})
		if !r.Matched {
			t.Errorf("%q not seen; screen:\n%s", until, r.Output)
		}
		// Let the program finish drawing, then read the screen once more.
		r = call[jobReadOut](t, cs, "job_read", map[string]any{"id": id, "wait_sec": 3, "idle_ms": 500, "offset": r.NextOffset, "screen": true})
		return strings.Split(r.Output, "\n")
	}
	stop := func(id string) { call[map[string]any](t, cs, "job_stop", map[string]any{"id": id}) }

	if runtime.GOOS == "windows" {
		st := call[jobStatus](t, cs, "job_start", map[string]any{"pty": true, "cols": 80, "rows": 12, "command": "Write-Output before; Start-Sleep 1; Clear-Host; Write-Output after; Start-Sleep 30"})
		defer stop(st.ID)
		lines := screen(st.ID, "after")
		if s := strings.Join(lines, "\n"); strings.Contains(s, "before") || !strings.Contains(s, "after") {
			t.Errorf("screen after Clear-Host:\n%s", s)
		}
		return
	}

	if _, err := exec.LookPath("top"); err == nil {
		st := call[jobStatus](t, cs, "job_start", map[string]any{"pty": true, "cols": 80, "rows": 12, "argv": []string{"top"}})
		lines := screen(st.ID, "PID")
		stop(st.ID)
		if len(lines) != 12 || !strings.HasPrefix(lines[0], "top") {
			t.Errorf("top screen (%d rows):\n%s", len(lines), strings.Join(lines, "\n"))
		}
	}
	if _, err := exec.LookPath("vi"); err == nil {
		file := filepath.Join(t.TempDir(), "sample.txt")
		var b strings.Builder
		for i := 1; i <= 30; i++ {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		os.WriteFile(file, []byte(b.String()), 0o644)
		st := call[jobStatus](t, cs, "job_start", map[string]any{"pty": true, "cols": 80, "rows": 12, "argv": []string{"vi", file}})
		lines := screen(st.ID, "line 1")
		call[map[string]any](t, cs, "job_write", map[string]any{"id": st.ID, "input": "\x1b:q!\r"})
		stop(st.ID)
		if len(lines) != 12 || lines[0] != "line 1" || lines[10] != "line 11" || strings.Contains(strings.Join(lines, "\n"), "line 12\n") {
			t.Errorf("vi screen (%d rows):\n%s", len(lines), strings.Join(lines, "\n"))
		}
	}

	st := call[jobStatus](t, cs, "job_start", map[string]any{"command": "echo plain"})
	if msg := callErr(t, cs, "job_read", map[string]any{"id": st.ID, "screen": true}); !strings.Contains(msg, "pty") {
		t.Errorf("screen of a pipe job: %s", msg)
	}
}
