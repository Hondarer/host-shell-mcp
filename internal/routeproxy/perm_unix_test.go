//go:build !windows

package routeproxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPasswordFilePermissions checks the ~/.ssh/config rule: others may read, nobody but the
// owner may write.
func TestPasswordFilePermissions(t *testing.T) {
	for _, tt := range []struct {
		mode os.FileMode
		ok   bool
	}{
		{0o600, true},
		{0o400, true},
		{0o644, true},
		{0o640, true},
		{0o664, false},
		{0o602, false},
		{0o666, false},
	} {
		path := filepath.Join(t.TempDir(), "p")
		if err := os.WriteFile(path, []byte("pw\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, tt.mode); err != nil {
			t.Fatal(err)
		}
		_, err := readPasswordFile(path)
		if tt.ok && err != nil {
			t.Errorf("mode %04o rejected: %v", tt.mode, err)
		}
		if !tt.ok && !errors.Is(err, errBadPermissions) {
			t.Errorf("mode %04o: err = %v, want a permission error", tt.mode, err)
		}
	}
}
