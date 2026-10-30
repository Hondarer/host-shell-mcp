package server

import (
	"os/exec"
	"runtime"
	"testing"
)

// powershells returns the PowerShell shells available on this host.
func powershells(t *testing.T) []string {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell tests run on Windows")
	}
	shells := []string{"powershell"}
	if _, err := exec.LookPath("pwsh"); err == nil {
		shells = append(shells, "pwsh")
	}
	return shells
}

// TestPowerShellExitCode checks that the exit code follows the last statement, as with sh -c.
func TestPowerShellExitCode(t *testing.T) {
	cs := connect(t)
	for _, sh := range powershells(t) {
		for _, c := range []struct {
			script string
			want   int
		}{
			{`Get-Item C:\no-such-file`, 1},
			{`cmd /c exit 3; Get-Date`, 0},
			{`Get-Item C:\no-such-file -ErrorAction SilentlyContinue; Get-Date`, 0},
			{`cmd /c exit 3`, 3},
			{`Write-Output ok`, 0},
			{"Write-Output ok\n# a comment at the end", 0},
			{`exit 5`, 5},
			{`cmd /c "echo to stderr 1>&2"`, 0},
			{`throw "stop"`, 1},
		} {
			out := call[struct {
				ExitCode int    `json:"exit_code"`
				Stderr   string `json:"stderr"`
			}](t, cs, "exec_run", map[string]any{"command": c.script, "shell": sh})
			if out.ExitCode != c.want {
				t.Errorf("%s: %q: exit code %d, want %d (stderr %q)", sh, c.script, out.ExitCode, c.want, out.Stderr)
			}
		}
	}
}
