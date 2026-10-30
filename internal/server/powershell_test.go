package server

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
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

// psStdin is text that no single Windows code page can represent.
const psStdin = "日本語 😀 Ελληνικά"

// needUTF8Console skips the test when host-shell has no UTF-8 console for PowerShell, which happens
// when the test runs with stdin or stdout on a console (see proc.UTF8Console).
func needUTF8Console(t *testing.T) {
	if !proc.UTF8Console() {
		t.Skip("no UTF-8 console for PowerShell: the test's stdin or stdout is a console")
	}
}

// TestPowerShellStdin checks that text passed as stdin reaches PowerShell intact, whatever the
// console code page, with stdin_encoding omitted or set to utf-8.
func TestPowerShellStdin(t *testing.T) {
	shells := powershells(t)
	needUTF8Console(t)
	cs := connect(t)
	for _, sh := range shells {
		for _, enc := range []string{"", "utf-8"} {
			out := call[struct {
				Stdout string `json:"stdout"`
				Stderr string `json:"stderr"`
			}](t, cs, "exec_run", map[string]any{"command": `$input | ForEach-Object { $_ }`, "shell": sh, "stdin": psStdin + "\n", "stdin_encoding": enc})
			if strings.TrimSpace(out.Stdout) != psStdin {
				t.Errorf("%s (stdin_encoding %q): stdout %q, stderr %q", sh, enc, out.Stdout, out.Stderr)
			}
		}
	}
}

// TestPowerShellStdinJob checks stdin sent to a PowerShell job with job_write.
func TestPowerShellStdinJob(t *testing.T) {
	shells := powershells(t)
	needUTF8Console(t)
	cs := connect(t)
	for _, sh := range shells {
		st := call[jobStatus](t, cs, "job_start", map[string]any{"command": `$input | ForEach-Object { "got: $_" }`, "shell": sh, "stdin_open": true})
		call[map[string]any](t, cs, "job_write", map[string]any{"id": st.ID, "input": psStdin + "\n", "close": true})
		r := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 30, "until": "got: "})
		if !strings.Contains(r.Output, "got: "+psStdin) {
			t.Errorf("%s: output %q", sh, r.Output)
		}
	}
}

// TestPowerShellArgvStdin checks stdin for PowerShell started with argv.
func TestPowerShellArgvStdin(t *testing.T) {
	shells := powershells(t)
	needUTF8Console(t)
	cs := connect(t)
	for _, sh := range shells {
		out := call[struct {
			Stdout string `json:"stdout"`
		}](t, cs, "exec_run", map[string]any{"argv": []string{sh + ".exe", "-NoProfile", "-Command", "[Console]::OutputEncoding = [Text.Encoding]::UTF8; $input"}, "stdin": psStdin + "\n"})
		if strings.TrimSpace(out.Stdout) != psStdin {
			t.Errorf("%s: stdout %q", sh, out.Stdout)
		}
	}
}

// TestPowerShellCmdConsole checks that cmd keeps a console of its own in the OEM code page.
func TestPowerShellCmdConsole(t *testing.T) {
	powershells(t)
	needUTF8Console(t)
	cs := connect(t)
	out := call[struct {
		Stdout string `json:"stdout"`
	}](t, cs, "exec_run", map[string]any{"command": "chcp", "shell": "cmd"})
	if strings.Contains(out.Stdout, "65001") && textenc.Console().Name != textenc.UTF8.Name {
		t.Errorf("chcp in cmd: %q, want the OEM code page (%s)", out.Stdout, textenc.Console().Name)
	}
}

// TestPowerShellPlainErrors checks that errors are plain text and that progress is not written
// (PowerShell 5.1 writes both as CLIXML when started with -EncodedCommand).
func TestPowerShellPlainErrors(t *testing.T) {
	cs := connect(t)
	for _, sh := range powershells(t) {
		for _, script := range []string{
			`Get-Item C:\no-such-file`,
			`throw "host-shell-stop"`,
			`Write-Progress -Activity busy -Status half -PercentComplete 50; Write-Output done`,
		} {
			out := call[struct {
				Stdout string `json:"stdout"`
				Stderr string `json:"stderr"`
			}](t, cs, "exec_run", map[string]any{"command": script, "shell": sh})
			if strings.Contains(out.Stderr, "CLIXML") || strings.Contains(out.Stdout, "CLIXML") || strings.Contains(out.Stderr, "\x1b") {
				t.Errorf("%s: %q: stdout %q, stderr %q", sh, script, out.Stdout, out.Stderr)
			}
			if strings.HasPrefix(script, "Write-Progress") && (strings.TrimSpace(out.Stdout) != "done" || out.Stderr != "") {
				t.Errorf("%s: progress: stdout %q, stderr %q", sh, out.Stdout, out.Stderr)
			}
			if strings.HasPrefix(script, "throw") && !strings.Contains(out.Stderr, "host-shell-stop") {
				t.Errorf("%s: throw: stderr %q", sh, out.Stderr)
			}
		}
	}
}

// TestPowerShellPTY checks that Read-Host reads from the terminal of a pty job.
func TestPowerShellPTY(t *testing.T) {
	cs := connect(t)
	for _, sh := range powershells(t) {
		st := call[jobStatus](t, cs, "job_start", map[string]any{"pty": true, "shell": sh, "command": `$n = Read-Host 'name'; "hello [$n]"`})
		r := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 20, "until": "name"})
		if !r.Matched {
			t.Errorf("%s: no prompt: %q", sh, r.Output)
		}
		call[map[string]any](t, cs, "job_write", map[string]any{"id": st.ID, "input": "bob\r"})
		r = call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 20, "until": `hello \[bob\]`, "offset": r.NextOffset})
		if !r.Matched {
			t.Errorf("%s: Read-Host: %q", sh, r.Output)
		}
		call[map[string]any](t, cs, "job_stop", map[string]any{"id": st.ID})
	}
}
