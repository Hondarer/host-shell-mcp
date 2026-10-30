package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// leftovers returns temporary files of atomic writes left in dir.
func leftovers(dir string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, ".*.host-shell-tmp-*"))
	return m
}

func TestAtomicWrite(t *testing.T) {
	cs := connect(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "app.conf")
	os.WriteFile(path, []byte("old\n"), 0o640)

	call[map[string]any](t, cs, "fs_write", map[string]any{"path": path, "content": "new\n", "atomic": true})
	if b, _ := os.ReadFile(path); string(b) != "new\n" {
		t.Fatalf("content = %q", b)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	call[editOut](t, cs, "fs_edit", map[string]any{"path": path, "old_string": "new", "new_string": "edited", "atomic": true})
	if b, _ := os.ReadFile(path); string(b) != "edited\n" {
		t.Fatalf("content after fs_edit = %q", b)
	}
	fresh := filepath.Join(dir, "fresh.txt")
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": fresh, "content": "x", "atomic": true})
	if b, _ := os.ReadFile(fresh); string(b) != "x" {
		t.Errorf("new file = %q", b)
	}
	for _, args := range []map[string]any{
		{"path": path, "content": "x", "atomic": true, "mode": "append"},
		{"path": path, "content": "x", "atomic": true, "mode": "create"},
		{"path": path, "content": "x", "atomic": true, "offset": 0},
	} {
		if msg := callErr(t, cs, "fs_write", args); !strings.Contains(msg, "atomic") {
			t.Errorf("%v: %s", args, msg)
		}
	}
	if l := leftovers(dir); len(l) > 0 {
		t.Errorf("temporary files left: %v", l)
	}
}

func TestAtomicWriteUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links and owners")
	}
	cs := connect(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "real.conf")
	link := filepath.Join(dir, "link.conf")
	os.WriteFile(target, []byte("old"), 0o600)
	os.Symlink("real.conf", link)
	call[map[string]any](t, cs, "fs_write", map[string]any{"path": link, "content": "via link", "atomic": true})
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "via link" {
		t.Errorf("target = %q", b)
	}

	// A file owned by someone else cannot keep its owner unless host-shell is root.
	if os.Getuid() == 0 || exec.Command("sudo", "-n", "true").Run() != nil {
		return
	}
	other := filepath.Join(dir, "other.conf")
	if out, err := exec.Command("sudo", "-n", "sh", "-c", "echo theirs > "+other+" && chown nobody "+other+" && chmod 666 "+other).CombinedOutput(); err != nil {
		t.Fatalf("prepare: %v %s", err, out)
	}
	if msg := callErr(t, cs, "fs_write", map[string]any{"path": other, "content": "mine", "atomic": true}); !strings.Contains(msg, "owner") {
		t.Errorf("error = %s", msg)
	}
	if b, _ := os.ReadFile(other); string(b) != "theirs\n" {
		t.Errorf("the file changed: %q", b)
	}
	if l := leftovers(dir); len(l) > 0 {
		t.Errorf("temporary files left: %v", l)
	}
}
