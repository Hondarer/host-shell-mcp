package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProxyStdio(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "host-shell.exe")
	if out, e := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); e != nil {
		t.Fatalf("build: %v: %s", e, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "bridge", "--proxy", "--proxy-allow-host", "allowed.example:443", "--", binary, "--mcp", "--no-log", "--client-proxy")
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "stdio-proxy-test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	for _, name := range []string{"sys_get_info", "proxy_status"} {
		res, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if e != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, e, res)
		}
		b, _ := json.Marshal(res.StructuredContent)
		var out map[string]any
		json.Unmarshal(b, &out)
		if name == "proxy_status" && out["state"] != "ready" {
			t.Fatalf("%s", b)
		}
		if name == "sys_get_info" && out["client_proxy"] != true {
			t.Fatalf("%s", b)
		}
	}
}
