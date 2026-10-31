package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func build(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "host-shell.exe")
	if out, e := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); e != nil {
		t.Fatalf("build: %v: %s", e, out)
	}
	return binary
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if e != nil || res.IsError {
		t.Fatalf("%s: %v %+v", name, e, res)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	json.Unmarshal(b, &out)
	return out
}

func echoOnce(t *testing.T, port int) {
	t.Helper()
	c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	payload := bytes.Repeat([]byte{0, 255, '\n'}, 100000)
	go c.Write(payload)
	got := make([]byte, len(payload))
	if _, e = io.ReadFull(c, got); e != nil || !bytes.Equal(got, payload) {
		t.Fatalf("echo through %d: %v", port, e)
	}
}

// TestForwardStdio runs the HTTP proxy and both forward directions in one real SSH-like stdio session.
func TestForwardStdio(t *testing.T) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	binary := build(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "bridge", "--proxy", "--proxy-allow-host", "allowed.example:443",
		"--forward-local", "0:127.0.0.1:"+port, "--forward-allow-remote", "127.0.0.1:"+port,
		"--", binary, "--mcp", "--no-log", "--client-proxy", "--allow-forward-local", "--allow-forward-remote")
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "stdio-forward-test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	if callTool(t, cs, "proxy_status", nil)["state"] != "ready" {
		t.Fatal("proxy not ready")
	}
	status := callTool(t, cs, "forward_status", nil)
	local := int(status["forwards"].([]any)[0].(map[string]any)["listen_port"].(float64))
	echoOnce(t, local)
	remote := callTool(t, cs, "forward_open", map[string]any{"direction": "remote", "host": "127.0.0.1", "port": l.Addr().(*net.TCPAddr).Port})
	echoOnce(t, int(remote["listen_port"].(float64)))
	info := callTool(t, cs, "sys_get_info", nil)
	if info["client_proxy"] != true || info["forward_local"] != true || len(info["remote_forwards"].([]any)) != 1 {
		t.Fatalf("sys_get_info %v", info)
	}
	if strings.Contains(fmt.Sprint(info["restrictions"]), "remote forward ports") {
		t.Fatalf("listen ports restricted without --allow-forward-remote-port: %v", info["restrictions"])
	}
	// Ordinary tools keep working beside forwarded traffic.
	if out := callTool(t, cs, "fs_list", map[string]any{"path": "."}); out == nil {
		t.Fatal("fs_list")
	}
}

func TestForwardStdioPermissions(t *testing.T) {
	binary := build(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "bridge", "--forward-remote", "0:127.0.0.1:1", "--", binary, "--mcp", "--no-log", "--allow-forward-local")
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader("")
	if e := cmd.Run(); e == nil || !strings.Contains(stderr.String(), "--allow-forward-remote") {
		t.Fatalf("missing permission: %v: %s", e, stderr.String())
	}
	// --allow-forward-remote-port only restricts; it neither permits nor accepts malformed ranges.
	for args, want := range map[string]string{
		"--allow-forward-remote-port 15432":                        "--allow-forward-remote-port requires --allow-forward-remote",
		"--allow-forward-remote --allow-forward-remote-port 20-10": `invalid --allow-forward-remote-port "20-10"`,
	} {
		stderr.Reset()
		cmd = exec.CommandContext(ctx, binary, append([]string{"--mcp", "--no-log"}, strings.Fields(args)...)...)
		cmd.Stderr = &stderr
		cmd.Stdin = strings.NewReader("")
		if e := cmd.Run(); e == nil || !strings.Contains(stderr.String(), want) {
			t.Fatalf("%s: %v: %s", args, e, stderr.String())
		}
	}
	// A target that permits forwarding still serves a bridge that requests nothing.
	cmd = exec.CommandContext(ctx, binary, "bridge", "--", binary, "--mcp", "--no-log", "--allow-forward-local", "--allow-forward-remote", "--allow-forward-remote-port", "20000-20099,15432")
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "stdio-plain-test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	info := callTool(t, cs, "sys_get_info", nil)
	if info["forward_remote"] != true {
		t.Fatal("permission not reported")
	}
	if !strings.Contains(fmt.Sprint(info["restrictions"]), "Allowed remote forward ports: 20000-20099, 15432") {
		t.Fatalf("restrictions %v", info["restrictions"])
	}
}
