package pty

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConPTY verifies interactive prompt/input handling and stdout capture over ConPTY for cmd and PowerShell.
func TestConPTY(t *testing.T) {
	for _, argv := range [][]string{
		{"cmd", "/v:on", "/c", "set /p n=name? & echo hello !n!"},
		{"powershell", "-NoProfile", "-Command", "$n = Read-Host 'name'; Write-Output \"hello $n\""},
	} {
		t.Run(argv[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			term, err := Start(ctx, exec.CommandContext(ctx, argv[0], argv[1:]...), 80, 25)
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			var mu sync.Mutex
			var buf strings.Builder
			done := make(chan struct{})
			go func() {
				defer close(done)
				b := make([]byte, 4096)
				for {
					n, err := term.Read(b)
					mu.Lock()
					buf.Write(b[:n])
					mu.Unlock()
					if err != nil {
						return
					}
				}
			}()
			output := func() string {
				mu.Lock()
				defer mu.Unlock()
				return buf.String()
			}
			deadline := time.Now().Add(20 * time.Second)
			for !strings.Contains(output(), "name") {
				if time.Now().After(deadline) {
					t.Fatalf("prompt not seen: %q", output())
				}
				time.Sleep(100 * time.Millisecond)
			}
			io.WriteString(term, "bob\r")
			if code, err := term.Wait(); err != nil || code != 0 {
				t.Errorf("exit %d, %v", code, err)
			}
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("output did not end")
			}
			if !strings.Contains(output(), "hello bob") {
				t.Errorf("output = %q", output())
			}
		})
	}
}
