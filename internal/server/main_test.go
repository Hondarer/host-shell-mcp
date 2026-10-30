package server

import (
	"os"
	"testing"

	"github.com/hondarer/host-shell-mcp/internal/tools"
)

func TestMain(m *testing.M) {
	// sudo_run starts the running executable (this test binary) as the askpass helper of sudo -A.
	if ran, code := tools.Askpass(); ran {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
