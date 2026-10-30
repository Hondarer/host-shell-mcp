//go:build !windows

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type sudoOut struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// TestSudoPasswordNotLeaked verifies that a password passed to exec_sudo reaches neither the
// command's stdin nor the output when sudo does not prompt for one (e.g. NOPASSWD on CI).
func TestSudoPasswordNotLeaked(t *testing.T) {
	if exec.Command("sudo", "-n", "true").Run() != nil {
		t.Skip("needs sudo without a password")
	}
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "host-shell-askpass-*"))
	cs := connect(t)
	const secret = "pw-not-for-the-command"
	out := call[sudoOut](t, cs, "exec_sudo", map[string]any{"command": "cat; echo " + secret, "stdin": "hello\n", "password": secret})
	if out.ExitCode != 0 || out.Stdout != "hello\n[REDACTED]\n" {
		t.Fatalf("got %+v", out)
	}
	// Check only for newly left-behind directories, ignoring concurrent tests.
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "host-shell-askpass-*"))
	for _, d := range after {
		if !slices.Contains(before, d) {
			t.Errorf("askpass directory left behind: %s", d)
		}
	}
}

// TestSudoPassword tests password-authenticated exec_sudo when HOST_SHELL_TEST_SUDO_PASSWORD is set.
func TestSudoPassword(t *testing.T) {
	password := os.Getenv("HOST_SHELL_TEST_SUDO_PASSWORD")
	if password == "" {
		t.Skip("set HOST_SHELL_TEST_SUDO_PASSWORD and run as a user whose sudo asks for it")
	}
	cs := connect(t)
	// Test failure cases before the valid password, as sudo caches successful authentication.
	out := call[sudoOut](t, cs, "exec_sudo", map[string]any{"command": "id -un"})
	if out.ExitCode == 0 || !strings.Contains(out.Stderr, "password") {
		t.Errorf("without a password: %+v", out)
	}
	out = call[sudoOut](t, cs, "exec_sudo", map[string]any{"command": "id -un", "password": "wrong-" + password})
	if out.ExitCode == 0 {
		t.Errorf("with a wrong password: %+v", out)
	}
	out = call[sudoOut](t, cs, "exec_sudo", map[string]any{"command": "id -un", "password": password})
	if out.ExitCode != 0 || strings.TrimSpace(out.Stdout) != "root" || strings.Contains(out.Stderr, password) {
		t.Errorf("with the password: %+v", out)
	}
}
