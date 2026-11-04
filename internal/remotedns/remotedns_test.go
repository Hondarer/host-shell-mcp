package remotedns

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fixedResolver struct{}

func (fixedResolver) LookupIP(_ context.Context, network, host string) ([]net.IP, error) {
	if host == "missing.invalid" {
		return nil, &net.DNSError{IsNotFound: true}
	}
	if network == "ip6" {
		return []net.IP{net.ParseIP("2001:db8::1")}, nil
	}
	return []net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("192.0.2.1")}, nil
}
func (fixedResolver) LookupCNAME(context.Context, string) (string, error) {
	return "canonical.example.", nil
}

func TestResolverProtocol(t *testing.T) {
	for _, tc := range []struct{ family, host, want string }{
		{"4", "example.invalid", "OK 1 canonical.example\n4 192.0.2.1\n"},
		{"6", "example.invalid", "OK 1 canonical.example\n6 2001:db8::1\n"},
		{"0", "missing.invalid", "ERR NONAME\n"},
		{"99", "example.invalid", "ERR FAMILY\n"},
	} {
		a, b := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); defer b.Close(); serve(context.Background(), b, tc.host, fixedResolver{}) }()
		a.SetDeadline(time.Now().Add(time.Second))
		io.WriteString(a, tc.family+"\n")
		got, err := io.ReadAll(a)
		a.Close()
		<-done
		if err != nil || string(got) != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
}

func TestHookThroughChannelAndCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux/glibc adapter")
	}
	// CI sets this to a release adapter so it is exercised instead of a freshly compiled one.
	prebuilt := os.Getenv("HOST_SHELL_TEST_DNS_LIBRARY")
	required := []string{"python3"}
	if prebuilt == "" {
		required = append(required, "cc")
	}
	for _, name := range required {
		if _, err := exec.LookPath(name); err != nil {
			if prebuilt != "" {
				t.Fatal(name + " unavailable to test " + prebuilt)
			}
			t.Skip(name + " unavailable")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	target, err := NewTarget(ctx, TargetOptions{Library: prebuilt})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if st, err := os.Stat(target.Library); err != nil || st.Mode().Perm()&077 != 0 {
		t.Fatalf("adapter permissions: %v %v", st, err)
	}
	copyTarget, err := NewTarget(ctx, TargetOptions{Library: target.Library})
	if err != nil {
		t.Fatalf("prebuilt adapter: %v", err)
	}
	copyTarget.Close()
	if _, err := os.Stat(target.Library); err != nil {
		t.Fatalf("prebuilt original removed: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "invalid.so")
	if err := os.WriteFile(bad, []byte("not an ELF library"), 0600); err != nil {
		t.Fatal(err)
	}
	if badTarget, err := NewTarget(ctx, TargetOptions{Library: bad}); err == nil {
		badTarget.Close()
		t.Fatal("accepted an invalid adapter")
	}
	var to, bo channel.Options
	target.Register(&to)
	RegisterGateway(&bo)
	// Deterministic source-side resolver; all target traffic still crosses the channel.
	bo.Handle(channel.KindDNS, func(ctx context.Context, in *channel.Incoming) {
		c, err := in.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(timeout))
		serve(ctx, c, in.Host, fixedResolver{})
	})
	a, b := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	var remote mcp.Connection
	go func() { var e error; remote, e = channel.Target(a, to).Connect(ctx); done <- e }()
	bridge, err := channel.Client(b, bo).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	code := `import socket,concurrent.futures,subprocess,sys,os
r=socket.getaddrinfo('example.invalid',443,socket.AF_INET,socket.SOCK_STREAM,0,socket.AI_CANONNAME)
assert r[0][3]=='canonical.example',r
assert r[0][4]==('192.0.2.1',443),r
assert socket.getaddrinfo('example.invalid',80,socket.AF_INET6)[0][4][0]=='2001:db8::1'
assert socket.getaddrinfo('127.0.0.1',80)[0][4][0]=='127.0.0.1'
assert socket.getaddrinfo('localhost',80)
try: socket.getaddrinfo('missing.invalid',443)
except socket.gaierror as e: assert e.errno==socket.EAI_NONAME,e
else: raise AssertionError('NXDOMAIN')
try: socket.getaddrinfo('example.invalid',443,0,0,0,socket.AI_NUMERICHOST)
except socket.gaierror: pass
else: raise AssertionError('NUMERICHOST')
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as p:
 assert all(p.map(lambda _: socket.getaddrinfo('example.invalid',443)[0][4][0]=='192.0.2.1',range(24)))
subprocess.run([sys.executable,'-c',"import socket; assert socket.getaddrinfo('example.invalid',443)[0][4][0]=='192.0.2.1'"],check=True)
os.environ['HOST_SHELL_DNS_SOCKET']='/nonexistent/host-shell.sock'
try: socket.getaddrinfo('example.invalid',443)
except socket.gaierror as e: assert e.errno==socket.EAI_AGAIN,e
else: raise AssertionError('unavailable relay')
print('native adapter, concurrency, child inheritance, and errors passed')`
	cmd := exec.CommandContext(ctx, "python3", "-c", code)
	cmd.Env = target.Environment(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook: %v\n%s", err, out)
	}
	// Exercise the actual native adapter after a Node child environment is sanitized.
	if node, err := exec.LookPath("node"); err == nil {
		helper, err := filepath.Abs("../../.agents/skills/host-shell-code-server/scripts/linux/remote-dns/session.mjs")
		if err != nil {
			t.Fatal(err)
		}
		script := `
import {pathToFileURL} from 'node:url';
import {spawnSync} from 'node:child_process';
import assert from 'node:assert/strict';
const {applyHostShellRemoteDNS}=await import(pathToFileURL(process.argv[1]));
const env={...process.env};
delete env.LD_PRELOAD; delete env.NODE_OPTIONS;
applyHostShellRemoteDNS(env);
const childScript = [
 "import dns from 'node:dns/promises';",
 "import assert from 'node:assert/strict';",
 "assert.equal((await dns.lookup('example.invalid')).address,'192.0.2.1');",
 "assert.ok(process.report.getReport().sharedObjects.includes(process.env.HOST_SHELL_DNS_LIBRARY));",
 "console.log('sanitized Node child loaded adapter and resolved through channel');"
].join('\n');
const child=spawnSync(process.execPath,['--input-type=module','-e',childScript],{env,encoding:'utf8'});
assert.equal(child.status,0,child.stderr);
console.log(child.stdout);`
		cmd := exec.CommandContext(ctx, node, "--input-type=module", "-e", script, helper)
		cmd.Env = append(target.Environment(os.Environ()), "HOST_SHELL_CODE_SERVER_REMOTE_DNS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Node reinjection: %v\n%s", err, out)
		}
	}
	dir := filepath.Dir(target.Socket)
	bridge.Close()
	target.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("session directory remains: %v", err)
	}
	if prebuilt != "" {
		if _, err := os.Stat(prebuilt); err != nil {
			t.Fatalf("prebuilt original removed: %v", err)
		}
	}
}

func TestEnvironment(t *testing.T) {
	target := Target{Library: "/private/adapter.so", Socket: "/private/dns.sock"}
	got := strings.Join(target.Environment([]string{"X=1", "LD_PRELOAD=/existing.so", "HOST_SHELL_DNS_SOCKET=old", "HOST_SHELL_DNS_LIBRARY=old"}), "\n")
	if got != "X=1\nLD_PRELOAD=/private/adapter.so /existing.so\nHOST_SHELL_DNS_SOCKET=/private/dns.sock\nHOST_SHELL_DNS_LIBRARY=/private/adapter.so" {
		t.Fatal(got)
	}
}
