package jobs

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

func ptySpec() proc.Spec {
	if runtime.GOOS == "windows" {
		return proc.Spec{Argv: []string{"powershell", "-NoProfile", "-Command", `$n = Read-Host 'name'; Write-Output "hello $n"`}}
	}
	return proc.Spec{Command: `printf "name? "; read n; echo "hello $n"`}
}

// TestPTYJob checks that a job started in a pseudo terminal can read a prompt without a newline and interact.
// It also checks that the prompt is readable without waiting for a newline when the output encoding is detected automatically.
func TestPTYJob(t *testing.T) {
	m := NewManager(proc.DefaultShell("powershell"), 1<<20)
	defer m.StopAll(5 * time.Second)
	j, err := m.Start(ptySpec(), StartOptions{PTY: true, Output: textenc.Choice{Auto: true, Enc: textenc.Console()}})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(sub string) string {
		deadline := time.Now().Add(20 * time.Second)
		for {
			data, _, _ := j.Read(0, 1<<20)
			if strings.Contains(data, sub) {
				return data
			}
			if time.Now().After(deadline) {
				t.Fatalf("%q not seen; output %q; status %+v", sub, data, j.Status())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	wait("name")
	if err := j.Write("bob\r", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	wait("hello bob")
	select {
	case <-j.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("job did not end")
	}
}
