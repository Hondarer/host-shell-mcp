package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/policy"
)

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestToolSelection(t *testing.T) {
	names := toolNames(t, connectWith(t, policy.Options{Tools: "files,-fs_remove"}, nil))
	for name, want := range map[string]bool{"help_get": true, "sys_get_info": true, "fs_read": true, "fs_remove": false, "exec_run": false, "net_request_http": false} {
		if names[name] != want {
			t.Errorf("tool %s present = %v, want %v", name, names[name], want)
		}
	}

	cs := connectWith(t, policy.Options{ReadOnly: true}, nil)
	names = toolNames(t, cs)
	for name, want := range map[string]bool{"fs_read": true, "fs_write": false, "exec_run": false, "job_read": true, "net_request_http": true, "net_check": true, "svc_control": false} {
		if names[name] != want {
			t.Errorf("read-only: tool %s present = %v, want %v", name, names[name], want)
		}
	}
	if msg := callErr(t, cs, "net_request_http", map[string]any{"url": "http://localhost:1/", "method": "POST"}); !strings.Contains(msg, "read-only") {
		t.Errorf("POST in read-only mode: %s", msg)
	}

	info := call[struct {
		ToolGroups   []string `json:"tool_groups"`
		Restrictions []string `json:"restrictions"`
	}](t, cs, "sys_get_info", nil)
	if len(info.Restrictions) == 0 || !strings.Contains(strings.Join(info.Restrictions, " "), "Read-only") {
		t.Errorf("restrictions = %v", info.Restrictions)
	}
}

// The initialize instructions must reflect tool selection before any tool is called.
func TestInstructionsMatchToolSelection(t *testing.T) {
	for _, tc := range []struct {
		name            string
		options         policy.Options
		read, run, edit bool
	}{
		{name: "files", options: policy.Options{Tools: "files"}, read: true, edit: true},
		{name: "exec", options: policy.Options{Tools: "exec"}, run: true},
		{name: "read-only", options: policy.Options{ReadOnly: true}, read: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := connectWith(t, tc.options, nil)
			instructions := cs.InitializeResult().Instructions
			names := toolNames(t, cs)
			for _, check := range []struct {
				tool, guidance string
				want           bool
			}{
				{"fs_read", "Read files with fs_read:", tc.read},
				{"exec_run", "Run commands with exec_run (", tc.run},
				{"fs_edit", "Edit files with fs_edit (", tc.edit},
			} {
				if names[check.tool] != check.want || strings.Contains(instructions, check.guidance) != check.want {
					t.Errorf("%s: exposed=%v, guidance=%v, want %v", check.tool, names[check.tool], strings.Contains(instructions, check.guidance), check.want)
				}
			}
			resources, err := cs.ListResources(context.Background(), nil)
			if err != nil || resources == nil || len(resources.Resources) == 0 {
				t.Fatalf("help resources: %v, %v", resources, err)
			}
			res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "host-shell://help/overview"})
			if err != nil || res == nil || len(res.Contents) == 0 || !strings.Contains(res.Contents[0].Text, "Tool groups") {
				t.Fatalf("overview resource: %v, %v", res, err)
			}
		})
	}
}

func TestPathPolicy(t *testing.T) {
	dir := t.TempDir()
	rw, ro := filepath.Join(dir, "rw"), filepath.Join(dir, "ro")
	os.MkdirAll(rw, 0o755)
	os.MkdirAll(ro, 0o755)
	os.WriteFile(filepath.Join(ro, "a.txt"), []byte("hello\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("x\n"), 0o644)
	cs := connectWith(t, policy.Options{AllowPaths: []string{rw}, AllowReadPaths: []string{ro}}, nil)

	call[map[string]any](t, cs, "fs_read", map[string]any{"path": filepath.Join(ro, "a.txt")})
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": filepath.Join(rw, "b.txt"), "content": "ok"})
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"fs_read", map[string]any{"path": filepath.Join(dir, "secret.txt")}},
		{"fs_write", map[string]any{"path": filepath.Join(ro, "a.txt"), "content": "x"}},
		{"fs_copy", map[string]any{"source": filepath.Join(ro, "a.txt"), "destination": filepath.Join(dir, "c.txt")}},
		{"search_grep", map[string]any{"pattern": "x", "path": dir}},
		{"exec_run", map[string]any{"argv": []string{"pwd"}, "cwd": dir}},
	} {
		if msg := callErr(t, cs, c.tool, c.args); !strings.HasPrefix(msg, "policy:") {
			t.Errorf("%s: %s", c.tool, msg)
		}
	}
}

func TestCommandPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix programs")
	}
	cs := connectWith(t, policy.Options{AllowCommands: []string{"echo"}, DenyCommands: []string{"forbidden"}, UnsetEnv: []string{"HS_SECRET"}}, nil)
	out := call[struct {
		Stdout string `json:"stdout"`
	}](t, cs, "exec_run", map[string]any{"argv": []string{"echo", "hi"}})
	if strings.TrimSpace(out.Stdout) != "hi" {
		t.Errorf("stdout = %q", out.Stdout)
	}
	if msg := callErr(t, cs, "exec_run", map[string]any{"command": "echo hi"}); !strings.Contains(msg, `"sh" is not allowed`) {
		t.Errorf("shell: %s", msg)
	}
	if msg := callErr(t, cs, "exec_run", map[string]any{"argv": []string{"echo", "forbidden"}}); !strings.Contains(msg, "denied") {
		t.Errorf("deny: %s", msg)
	}

	t.Setenv("HS_SECRET", "s3cr3t")
	cs = connectWith(t, policy.Options{UnsetEnv: []string{"HS_*"}}, nil)
	out = call[struct {
		Stdout string `json:"stdout"`
	}](t, cs, "exec_run", map[string]any{"command": "echo \"[$HS_SECRET]\""})
	if strings.TrimSpace(out.Stdout) != "[]" {
		t.Errorf("HS_SECRET leaked: %q", out.Stdout)
	}
}

func TestRedact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	os.WriteFile(path, []byte("DB_PASSWORD=hunter2\nUSER=app\n"), 0o644)
	cs := connectWith(t, policy.Options{Redact: true}, nil)
	out := call[struct {
		Content string `json:"content"`
	}](t, cs, "fs_read", map[string]any{"path": path})
	if strings.Contains(out.Content, "hunter2") || !strings.Contains(out.Content, "DB_PASSWORD=[REDACTED]") || !strings.Contains(out.Content, "USER=app") {
		t.Errorf("content = %q", out.Content)
	}
}

func TestConfirm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	var asked atomic.Int32
	answer := "accept"
	cs := connectWith(t, policy.Options{Confirm: "fs_write"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			asked.Add(1)
			if !strings.Contains(req.Params.Message, "fs_write") {
				t.Errorf("message = %q", req.Params.Message)
			}
			return &mcp.ElicitResult{Action: answer, Content: map[string]any{"approve": true}}, nil
		},
	})
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": path, "content": "x"})
	answer = "decline"
	if msg := callErr(t, cs, "fs_write", map[string]any{"path": path, "content": "y"}); !strings.Contains(msg, "did not allow") {
		t.Errorf("decline: %s", msg)
	}
	call[map[string]any](t, cs, "fs_read", map[string]any{"path": path}) // not subject to confirmation
	if asked.Load() != 2 {
		t.Errorf("asked %d times", asked.Load())
	}
	// Clients without elicitation support do not run it.
	cs = connectWith(t, policy.Options{Confirm: "destructive"}, nil)
	if msg := callErr(t, cs, "fs_write", map[string]any{"path": path, "content": "z"}); !strings.Contains(msg, "requires confirmation") {
		t.Errorf("no elicitation: %s", msg)
	}
	if b, _ := os.ReadFile(path); string(b) != "x" {
		t.Errorf("file = %q", b)
	}
}

func TestProgressAndCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	var mu sync.Mutex
	var msgs []string
	cs := connectWith(t, policy.Options{}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			msgs = append(msgs, req.Params.Message)
			mu.Unlock()
		},
	})
	p := &mcp.CallToolParams{Name: "exec_run", Arguments: map[string]any{"command": "echo step1; sleep 2.5; echo step2"}}
	p.SetProgressToken("tok")
	if _, err := cs.CallTool(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(msgs) < 2 || !strings.Contains(strings.Join(msgs, "\n"), "last line: step1") {
		t.Errorf("progress messages = %q", msgs)
	}
	mu.Unlock()

	// Cancelling stops the process tree.
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "exec_run", Arguments: map[string]any{"command": "sleep 30 & echo $! > " + pidFile + "; wait"}})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("cancel: err=%v after %s", err, time.Since(start))
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(5 * time.Second)
	for pid > 0 && processExists(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d still running after cancel", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func processExists(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || len(s) < i+3 || s[i+2] != 'Z'
}

func TestNetRequestHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true}`)
		case "/echo":
			b, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "%s %s %s", r.Method, r.Header.Get("X-Test"), b)
		case "/bin":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0, 1, 2, 255})
		case "/redirect":
			http.Redirect(w, r, "/json", http.StatusFound)
		}
	}))
	defer srv.Close()
	cs := connect(t)

	type httpOut struct {
		Status       int               `json:"status"`
		Headers      map[string]string `json:"headers"`
		Body         string            `json:"body"`
		BodyEncoding string            `json:"body_encoding"`
		BodyBytes    int64             `json:"body_bytes"`
		SavedTo      string            `json:"saved_to"`
		FinalURL     string            `json:"final_url"`
	}
	out := call[httpOut](t, cs, "net_request_http", map[string]any{"url": srv.URL + "/json"})
	if out.Status != 200 || out.Body != `{"ok":true}` || out.BodyEncoding != "text" {
		t.Errorf("json: %+v", out)
	}
	out = call[httpOut](t, cs, "net_request_http", map[string]any{"url": srv.URL + "/echo", "method": "PUT", "headers": map[string]string{"X-Test": "h"}, "body": "payload"})
	if out.Body != "PUT h payload" {
		t.Errorf("echo: %+v", out)
	}
	out = call[httpOut](t, cs, "net_request_http", map[string]any{"url": srv.URL + "/bin"})
	if out.BodyEncoding != "base64" || out.Body != "AAEC/w==" {
		t.Errorf("bin: %+v", out)
	}
	out = call[httpOut](t, cs, "net_request_http", map[string]any{"url": srv.URL + "/redirect"})
	if out.Status != 200 || !strings.HasSuffix(out.FinalURL, "/json") {
		t.Errorf("redirect: %+v", out)
	}
	save := filepath.Join(t.TempDir(), "dl.bin")
	out = call[httpOut](t, cs, "net_request_http", map[string]any{"url": srv.URL + "/bin", "save_to": save})
	if b, _ := os.ReadFile(save); len(b) != 4 || out.Body != "" || out.BodyBytes != 4 {
		t.Errorf("save_to: %+v %v", out, b)
	}

	cs = connectWith(t, policy.Options{AllowHosts: []string{"example.invalid"}}, nil)
	if msg := callErr(t, cs, "net_request_http", map[string]any{"url": srv.URL + "/json"}); !strings.Contains(msg, "policy:") {
		t.Errorf("allow-host: %s", msg)
	}
}

func TestNetExchangeAndCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.WriteString(c, "+READY\r\n")
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				fmt.Fprintf(c, "+ECHO %s", buf[:n])
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	cs := connect(t)
	out := call[struct {
		Received string `json:"received"`
		Matched  bool   `json:"matched"`
	}](t, cs, "net_exchange", map[string]any{"host": "127.0.0.1", "port": port, "send": "PING\r\n", "until": "ECHO PING\r\n"})
	if !out.Matched || !strings.Contains(out.Received, "+READY") {
		t.Errorf("net_exchange: %+v", out)
	}

	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	chk := call[struct {
		Results []struct {
			Target    string `json:"target"`
			Connected *bool  `json:"connected"`
			Error     string `json:"error"`
		} `json:"results"`
	}](t, cs, "net_check", map[string]any{"targets": []string{fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("127.0.0.1:%d", closedPort), "localhost"}})
	if len(chk.Results) != 3 || chk.Results[0].Connected == nil || !*chk.Results[0].Connected ||
		chk.Results[1].Connected == nil || *chk.Results[1].Connected || chk.Results[2].Connected != nil {
		t.Errorf("net_check: %+v", chk)
	}

	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		ls := call[struct {
			Listeners []struct {
				Port int `json:"port"`
				PID  int `json:"pid"`
			} `json:"listeners"`
		}](t, cs, "net_list_listeners", map[string]any{"port": port})
		if len(ls.Listeners) == 0 || ls.Listeners[0].PID != os.Getpid() {
			t.Errorf("net_list_listeners: %+v (pid %d)", ls, os.Getpid())
		}
	}
	ifs := call[struct {
		Interfaces []struct {
			Name string `json:"name"`
		} `json:"interfaces"`
	}](t, cs, "net_list_interfaces", nil)
	if len(ifs.Interfaces) == 0 {
		t.Error("no interfaces")
	}
}

type jobStatus struct {
	ID       string `json:"id"`
	Running  bool   `json:"running"`
	ExitCode *int   `json:"exit_code"`
	PTY      bool   `json:"pty"`
}

type jobReadOut struct {
	Status     jobStatus `json:"status"`
	Output     string    `json:"output"`
	NextOffset int64     `json:"next_offset"`
	Matched    bool      `json:"matched"`
}

func TestPTYJob(t *testing.T) {
	cs := connect(t)
	args := map[string]any{"pty": true, "command": `if [ -t 0 ]; then echo TTY; fi; printf "name? "; read n; printf "\033[1;32mhello %s\033[0m\n" "$n"`}
	if runtime.GOOS == "windows" {
		args = map[string]any{"pty": true, "argv": []string{"powershell", "-NoProfile", "-Command", `$n = Read-Host 'name'; Write-Output "hello $n"`}}
	}
	st := call[jobStatus](t, cs, "job_start", args)
	if !st.PTY {
		t.Fatalf("status = %+v", st)
	}
	r := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 20, "until": `name`})
	if !r.Matched {
		raw := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "raw": true})
		t.Fatalf("prompt not seen: %q; status %+v; raw output (%d bytes): %q", r.Output, raw.Status, len(raw.Output), raw.Output)
	}
	if runtime.GOOS != "windows" && !strings.Contains(r.Output, "TTY") {
		t.Errorf("not a terminal: %q", r.Output)
	}
	call[map[string]any](t, cs, "job_write", map[string]any{"id": st.ID, "input": "bob\r"})
	r = call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "offset": r.NextOffset, "wait_sec": 20, "until": "hello bob"})
	if !r.Matched || strings.Contains(r.Output, "\x1b") {
		raw := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "raw": true})
		t.Errorf("answer: %+v; raw output (%d bytes): %q", r, len(raw.Output), raw.Output)
	}
	r = call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 20, "until": "^$x"})
	deadline := time.Now().Add(20 * time.Second)
	for r.Status.Running && time.Now().Before(deadline) {
		r = call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "offset": r.NextOffset, "wait_sec": 5})
	}
	if r.Status.Running || r.Status.ExitCode == nil || *r.Status.ExitCode != 0 {
		t.Errorf("end: %+v", r.Status)
	}
}

func TestJobSignalAndIdle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are Linux only")
	}
	cs := connect(t)
	st := call[jobStatus](t, cs, "job_start", map[string]any{"command": `trap 'echo got INT; exit 3' INT; echo ready; while :; do sleep 0.1; done`})
	call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 10, "until": "ready"})
	end := call[jobStatus](t, cs, "job_stop", map[string]any{"id": st.ID, "signal": "interrupt"})
	if end.Running || end.ExitCode == nil || *end.ExitCode != 3 {
		t.Errorf("after interrupt: %+v", end)
	}

	st = call[jobStatus](t, cs, "job_start", map[string]any{"command": `echo a; sleep 3; echo b`})
	start := time.Now()
	r := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "wait_sec": 10, "idle_ms": 300})
	if strings.TrimSpace(r.Output) != "a" || time.Since(start) > 2500*time.Millisecond {
		t.Errorf("idle: %q after %s", r.Output, time.Since(start))
	}
	tail := call[jobReadOut](t, cs, "job_read", map[string]any{"id": st.ID, "offset": -2})
	if tail.Output != "a\n" {
		t.Errorf("tail = %q", tail.Output)
	}
	call[jobStatus](t, cs, "job_stop", map[string]any{"id": st.ID})
}

func TestAdmin(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("admin tools are implemented for Linux and Windows")
	}
	cs := connect(t)
	disks := call[struct {
		Disks []struct {
			Mount      string `json:"mount"`
			TotalBytes uint64 `json:"total_bytes"`
		} `json:"disks"`
	}](t, cs, "disk_list", nil)
	if len(disks.Disks) == 0 || disks.Disks[0].TotalBytes == 0 {
		t.Errorf("disk_list: %+v", disks)
	}

	// proc_kill
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-Command", "Start-Sleep 30")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	k := call[struct {
		Exited bool `json:"exited"`
	}](t, cs, "proc_kill", map[string]any{"pid": cmd.Process.Pid})
	if !k.Exited {
		t.Error("proc_kill: process did not exit")
	}

	// Services and logs may be unavailable depending on the environment (systemd, permissions).
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "svc_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		if runtime.GOOS == "windows" {
			t.Errorf("svc_list: %v", res.Content[0].(*mcp.TextContent).Text)
		} else {
			t.Logf("svc_list unavailable: %v", res.Content[0].(*mcp.TextContent).Text)
		}
	} else {
		var out struct {
			Services []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"services"`
		}
		b, _ := json.Marshal(res.StructuredContent)
		json.Unmarshal(b, &out)
		if len(out.Services) == 0 {
			t.Error("svc_list returned no services")
		}
	}
	args := map[string]any{"lines": 5}
	if runtime.GOOS == "windows" {
		args["log"] = "System"
	}
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "log_query", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		if runtime.GOOS == "windows" {
			t.Errorf("log_query: %v", res.Content[0].(*mcp.TextContent).Text)
		} else {
			t.Logf("log_query unavailable: %v", res.Content[0].(*mcp.TextContent).Text)
		}
	}

	if runtime.GOOS == "windows" {
		key := `HKCU\Software\host-shell-test`
		call[map[string]any](t, cs, "reg_write", map[string]any{"key": key, "value": "s", "data": "text"})
		call[map[string]any](t, cs, "reg_write", map[string]any{"key": key, "value": "n", "type": "REG_DWORD", "data": 7})
		call[map[string]any](t, cs, "reg_write", map[string]any{"key": key, "value": "m", "type": "REG_MULTI_SZ", "data": []string{"a", "b"}})
		rd := call[struct {
			Values []struct {
				Name string `json:"name"`
				Type string `json:"type"`
				Data any    `json:"data"`
			} `json:"values"`
		}](t, cs, "reg_read", map[string]any{"key": key})
		if len(rd.Values) != 3 {
			t.Errorf("reg_read: %+v", rd)
		}
		call[map[string]any](t, cs, "reg_delete", map[string]any{"key": key, "recursive": true})
		callErr(t, cs, "reg_read", map[string]any{"key": key})
	}
}
