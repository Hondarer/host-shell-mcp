//go:build !windows

package tools

import (
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	if ran, code := Askpass(); ran {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// TestAskpass runs the test binary as sudo would run the askpass helper.
func TestAskpass(t *testing.T) {
	env, cleanup, err := withAskpass("p@ss w0rd")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	run := func() (string, error) {
		cmd := exec.Command(env["SUDO_ASKPASS"], "[sudo] password for someone: ")
		cmd.Env = append(os.Environ(), askpassEnv+"="+env[askpassEnv])
		out, err := cmd.Output()
		return string(out), err
	}
	for i := 0; i < passwordTries; i++ {
		if out, err := run(); err != nil || out != "p@ss w0rd\n" {
			t.Fatalf("try %d: %q, %v", i+1, out, err)
		}
	}
	// One more prompt than queued copies fails instead of waiting forever.
	if out, err := run(); err == nil {
		t.Fatalf("extra try printed %q", out)
	}
	dir := env[askpassEnv]
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("named pipe left behind: %v", err)
	}
}
