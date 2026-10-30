package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var known = map[string]string{"exec_run": "exec", "fs_read": "files", "fs_write": "files", "sudo_run": "exec", "net_http": "net"}

func TestSelector(t *testing.T) {
	p, err := New(Options{Tools: "all,-exec,+exec_run"}, known)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tool, group string
		want        bool
	}{
		{"exec_run", "exec", true}, {"sudo_run", "exec", false}, {"fs_read", "files", true},
	} {
		if got := p.Enabled(c.tool, c.group, false); got != c.want {
			t.Errorf("Enabled(%s) = %v, want %v", c.tool, got, c.want)
		}
	}
	if _, err := New(Options{Tools: "all,-nosuch"}, known); err == nil {
		t.Error("unknown name should be rejected")
	}
	p, _ = New(Options{Confirm: "destructive,-fs_write"}, known)
	if !p.NeedsConfirm("exec_run", "exec", false) || p.NeedsConfirm("fs_write", "files", false) || p.NeedsConfirm("fs_read", "files", true) {
		t.Error("confirm selection is wrong")
	}
	if Unrestricted().NeedsConfirm("exec_run", "exec", false) || !Unrestricted().Enabled("exec_run", "exec", false) {
		t.Error("unrestricted policy must enable everything without confirmation")
	}
}

func TestPaths(t *testing.T) {
	dir := t.TempDir()
	rw := filepath.Join(dir, "rw")
	ro := filepath.Join(dir, "ro")
	os.MkdirAll(rw, 0o755)
	os.MkdirAll(ro, 0o755)
	p, err := New(Options{AllowPaths: []string{rw}, AllowReadPaths: []string{ro}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ok := func(path string, write bool) {
		t.Helper()
		if err := p.CheckPath(path, write); err != nil {
			t.Errorf("CheckPath(%s, %v): %v", path, write, err)
		}
	}
	ng := func(path string, write bool) {
		t.Helper()
		if err := p.CheckPath(path, write); err == nil {
			t.Errorf("CheckPath(%s, %v) should fail", path, write)
		}
	}
	ok(filepath.Join(rw, "new", "file.txt"), true)
	ok(filepath.Join(ro, "a.txt"), false)
	ng(filepath.Join(ro, "a.txt"), true)
	ng(filepath.Join(dir, "other"), false)
	ng(filepath.Join(rw, "..", "other"), true)
	ng(rw+"x", false) // A name prefix match is not allowed.

	if runtime.GOOS != "windows" {
		// A symbolic link inside an allowed location that points outside is judged after resolving it.
		os.Symlink(dir, filepath.Join(rw, "escape"))
		ng(filepath.Join(rw, "escape", "other"), false)
	}

	// When only read-only locations are given, writes are allowed nowhere.
	p, _ = New(Options{AllowReadPaths: []string{ro}}, nil)
	if p.CheckPath(filepath.Join(ro, "x"), true) == nil {
		t.Error("write must be refused with only read paths")
	}
	if Unrestricted().CheckPath("/anything", true) != nil {
		t.Error("unrestricted policy must allow any path")
	}
}

func TestCommands(t *testing.T) {
	p, err := New(Options{AllowCommands: []string{"git", "go*"}, DenyCommands: []string{`push\s+--force`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCommand([]string{"/usr/bin/git"}, "git status"); err != nil {
		t.Error(err)
	}
	if err := p.CheckCommand([]string{"gofmt"}, "gofmt -l ."); err != nil {
		t.Error(err)
	}
	if err := p.CheckCommand([]string{"sh"}, "git status"); err == nil || !strings.Contains(err.Error(), "argv") {
		t.Errorf("shell must be refused with a hint: %v", err)
	}
	if err := p.CheckCommand([]string{"git"}, "git push  --force origin"); err == nil {
		t.Error("denied pattern must be refused")
	}
}

func TestHosts(t *testing.T) {
	p, err := New(Options{AllowHosts: []string{"localhost", "*.internal:443", "10.0.0.0/8", "unix:/var/run/*.sock"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		host string
		port int
		want bool
	}{
		{"localhost", 8080, true}, {"api.internal", 443, true}, {"api.internal", 80, false},
		{"10.1.2.3", 22, true}, {"192.168.0.1", 22, false}, {"example.com", 443, false},
	} {
		if got := p.CheckHost(c.host, c.port) == nil; got != c.want {
			t.Errorf("CheckHost(%s, %d) = %v, want %v", c.host, c.port, got, c.want)
		}
	}
	if p.CheckUnixSocket("/var/run/docker.sock") != nil || p.CheckUnixSocket("/tmp/x.sock") == nil {
		t.Error("unix socket rules are wrong")
	}
}

func TestEnvAndRedact(t *testing.T) {
	p, _ := New(Options{UnsetEnv: []string{"AWS_*", "TOKEN"}, Redact: true, RedactPatterns: []string{`secret-\d+`}}, nil)
	env := p.FilterEnv([]string{"AWS_SECRET_ACCESS_KEY=x", "TOKEN=y", "PATH=/bin"})
	if len(env) != 1 || env[0] != "PATH=/bin" {
		t.Errorf("FilterEnv = %v", env)
	}
	in := "password=hunter2 Authorization: Bearer abc.def key AKIAABCDEFGHIJKLMNOP id secret-42 url https://u:pw@host/x\n" +
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----"
	out := p.RedactString(in)
	for _, leak := range []string{"hunter2", "abc.def", "AKIAABCDEFGHIJKLMNOP", "secret-42", ":pw@", "MIIE"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked in %q", leak, out)
		}
	}
	for _, keep := range []string{"password=", "Authorization: Bearer ", "https://u:", "BEGIN RSA PRIVATE KEY"} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q should be kept in %q", keep, out)
		}
	}
}
