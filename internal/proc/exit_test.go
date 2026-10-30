//go:build linux || windows

package proc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// exitChildEnv makes the test binary act as a host-shell that starts a long command and waits to be killed.
const exitChildEnv = "HOST_SHELL_TEST_EXIT_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(exitChildEnv) != "" {
		exitChild()
		return
	}
	os.Exit(m.Run())
}

func exitChild() {
	if err := KillChildrenOnExit(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	argv := []string{"sleep", "60"}
	if runtime.GOOS == "windows" {
		argv = []string{"ping", "-n", "60", "127.0.0.1"}
	}
	cmd, err := Command(context.Background(), Spec{Argv: argv}, DefaultShell("powershell"))
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("pid:", cmd.Process.Pid)
	time.Sleep(time.Minute)
}

// TestChildrenEndWhenKilled checks that a command ends when the host-shell that started it is killed.
func TestChildrenEndWhenKilled(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(exe, "-test.run=^$")
	parent.Env = append(os.Environ(), exitChildEnv+"=1")
	out, _ := parent.StdoutPipe()
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "pid:")))
	if err != nil {
		parent.Process.Kill()
		t.Fatalf("child said %q", line)
	}
	if !alive(pid) {
		t.Fatalf("command %d is not running", pid)
	}
	parent.Process.Kill() // SIGKILL or TerminateProcess: no chance to clean up
	parent.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			Signal(pid, "kill", false)
			t.Fatalf("command %d survived the killed host-shell", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
