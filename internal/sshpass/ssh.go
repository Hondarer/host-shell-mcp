package sshpass

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Environment variables naming the clients that the wrappers run; by default "ssh" and "scp" are
// looked up in PATH, skipping the wrapper's own directory.
const (
	sshPathEnv = "HOST_SHELL_SSH_PATH"
	scpPathEnv = "HOST_SHELL_SCP_PATH"
)

// WrapperTool returns the client ("ssh" or "scp") that the host-shell executable stands in for when
// it runs under the name argv0, or "" for any other name.
//
// VS Code Remote - SSH takes the path of an executable (remote.SSH.path) without arguments, and
// copies the server with the scp next to it, so links or copies of host-shell named ssh and scp in
// a directory of their own stand in for both.
func WrapperTool(argv0 string) string {
	base := filepath.Base(argv0)
	if runtime.GOOS == "windows" {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	}
	switch base {
	case "ssh":
		return "ssh"
	case "scp":
		return "scp"
	}
	return ""
}

// findTool returns the client to run: the path in env when set, otherwise name in PATH. The wrapper
// itself (through any link) is skipped, so a wrapper named ssh in PATH does not run itself.
func findTool(name, env, self string) (string, error) {
	if p := os.Getenv(env); p != "" {
		path, err := exec.LookPath(p)
		if err != nil {
			return "", fmt.Errorf("cannot find %s %q: %v", name, p, err)
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if sameFile(path, self) {
			return "", fmt.Errorf("%s is host-shell itself; set %s to the %s client", path, env, name)
		}
		return path, nil
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		path, err := exec.LookPath(filepath.Join(dir, name))
		if err != nil || sameFile(path, self) {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("cannot find the %s client in PATH (set %s)", name, env)
}

// RunTool runs the client tool ("ssh" or "scp") with args, with stored passwords supplied through
// SSH_ASKPASS, and returns its exit code. Standard input, output and error are the wrapper's own.
// The mode follows the ssh in PATH, which is also the one that scp starts.
func RunTool(tool string, args []string, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "host-shell %s: %v\n", tool, err)
		return 255
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	ssh, err := findTool("ssh", sshPathEnv, self)
	if err != nil {
		return fail(err)
	}
	path := ssh
	if tool == "scp" {
		if path, err = findTool("scp", scpPathEnv, self); err != nil {
			return fail(err)
		}
	}
	code, err := execTool(path, args, Env(os.Environ(), self, Mode(ssh)))
	if err != nil {
		return fail(err)
	}
	return code
}
