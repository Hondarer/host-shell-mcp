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

// TestSudoPasswordNotLeaked checks that a password given to sudo_run reaches neither the command
// nor the result when sudo does not ask for it (a NOPASSWD rule, as on CI runners).
func TestSudoPasswordNotLeaked(t *testing.T) {
	if exec.Command("sudo", "-n", "true").Run() != nil {
		t.Skip("needs sudo without a password")
	}
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "host-shell-askpass-*"))
	cs := connect(t)
	const secret = "pw-not-for-the-command"
	out := call[sudoOut](t, cs, "sudo_run", map[string]any{"command": "cat; echo " + secret, "stdin": "hello\n", "password": secret})
	if out.ExitCode != 0 || out.Stdout != "hello\n[REDACTED]\n" {
		t.Fatalf("got %+v", out)
	}
	// Other tests may create and remove their own directories meanwhile: look only for new ones.
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "host-shell-askpass-*"))
	for _, d := range after {
		if !slices.Contains(before, d) {
			t.Errorf("askpass directory left behind: %s", d)
		}
	}
}

// TestSudoPassword runs sudo_run with a password. It needs a user whose sudo asks for a password;
// CI creates one and runs this test as that user with HOST_SHELL_TEST_SUDO_PASSWORD set.
func TestSudoPassword(t *testing.T) {
	password := os.Getenv("HOST_SHELL_TEST_SUDO_PASSWORD")
	if password == "" {
		t.Skip("set HOST_SHELL_TEST_SUDO_PASSWORD and run as a user whose sudo asks for it")
	}
	cs := connect(t)
	// The order matters: after a success sudo may remember the authentication.
	out := call[sudoOut](t, cs, "sudo_run", map[string]any{"command": "id -un"})
	if out.ExitCode == 0 || !strings.Contains(out.Stderr, "password") {
		t.Errorf("without a password: %+v", out)
	}
	out = call[sudoOut](t, cs, "sudo_run", map[string]any{"command": "id -un", "password": "wrong-" + password})
	if out.ExitCode == 0 {
		t.Errorf("with a wrong password: %+v", out)
	}
	out = call[sudoOut](t, cs, "sudo_run", map[string]any{"command": "id -un", "password": password})
	if out.ExitCode != 0 || strings.TrimSpace(out.Stdout) != "root" || strings.Contains(out.Stderr, password) {
		t.Errorf("with the password: %+v", out)
	}
}
