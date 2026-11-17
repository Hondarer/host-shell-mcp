package ownerdir

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0700 {
		t.Errorf("mode = %04o", fi.Mode().Perm())
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err == nil {
		t.Error("Ensure accepted a directory other users can read")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(link); err == nil {
		t.Error("Ensure accepted a symbolic link")
	}
}
