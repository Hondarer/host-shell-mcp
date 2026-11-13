package sshpass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoredEncrypted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dirEnv, dir)
	if err := Set("alice@hop1", "S3cret pass!"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "alice@hop1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), dpapiPrefix) || strings.Contains(string(b), "S3cret") {
		t.Errorf("file content %q is not a DPAPI blob", b)
	}
	if pw, found, err := Get("alice@hop1"); err != nil || !found || pw != "S3cret pass!" {
		t.Errorf("Get = %q %v %v", pw, found, err)
	}
	// A plain-text file (written by hand) is still read.
	plain := filepath.Join(dir, "bob@hop1")
	if err := os.WriteFile(plain, []byte("plain\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// %TEMP% may allow writing to others; a file in the default directory inherits the profile's ACL.
	if err := secureFile(plain); err != nil {
		t.Fatal(err)
	}
	if pw, _, err := Get("bob@hop1"); err != nil || pw != "plain" {
		t.Errorf("plain Get = %q %v", pw, err)
	}
}
