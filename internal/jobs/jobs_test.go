package jobs

import (
	"runtime"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/proc"
)

// TestInitialStdinOrder checks that a Write and CloseStdin right after Start never overtake the
// initial input. The race was intermittent, so the sequence is repeated.
func TestInitialStdinOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses cat")
	}
	m := NewManager(proc.DefaultShell("powershell"), 1<<20)
	defer m.StopAll(5 * time.Second)
	for i := range 50 {
		j, err := m.Start(proc.Spec{Argv: []string{"cat"}}, StartOptions{Stdin: "first\n", KeepStdin: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = j.Write("second\n", 5*time.Second); err != nil {
			t.Fatal(i, err)
		}
		if err = j.CloseStdin(); err != nil {
			t.Fatal(i, err)
		}
		select {
		case <-j.Done():
		case <-time.After(10 * time.Second):
			t.Fatalf("run %d: cat did not end after stdin closed", i)
		}
		if out, _, _ := j.Read(0, 1<<10); out != "first\nsecond\n" {
			t.Fatalf("run %d: output %q", i, out)
		}
	}
}
