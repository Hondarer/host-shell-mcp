package server

import (
	"testing"

	"github.com/hondarer/host-shell-mcp/internal/testutil"
)

// exec_sudo starts the running executable (this test binary) as the askpass helper of sudo -A.
func TestMain(m *testing.M) { testutil.Main(m) }
