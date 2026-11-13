package sshpass

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"

	"github.com/hondarer/host-shell-mcp/internal/proc"
)

// execTool runs the client (ssh or scp) as a child and returns its exit code. Windows has no exec, so the wrapper waits:
// it joins a job object that kills ssh when the wrapper is killed (as VS Code does on disconnect),
// and ignores Ctrl+C, which the console delivers to ssh as well.
func execTool(path string, args, env []string) (int, error) {
	_ = proc.KillChildrenOnExit() // best effort: a job the caller put us in may forbid nesting
	signal.Ignore(os.Interrupt)
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

// Detach does nothing on Windows: OpenSSH for Windows 8.4 and later honors SSH_ASKPASS_REQUIRE.
func Detach(*exec.Cmd) {}
