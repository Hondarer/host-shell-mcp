//go:build !windows

package routeproxy

import (
	"fmt"
	"os"
	"syscall"
)

// checkedOpen opens the password file and applies the check OpenSSH uses for the user's
// ~/.ssh/config (readconf.c): the owner is root or the current user, and neither the group nor
// others may write to it. Others may read it, as they may read ~/.ssh/config.
func checkedOpen(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open password file: %v", err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("cannot open password file: %v", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("%w %s: cannot read its owner", errBadPermissions, path)
	}
	if st.Uid != 0 && int(st.Uid) != os.Getuid() {
		f.Close()
		return nil, fmt.Errorf("%w %s: it must be owned by you or root (owner uid %d)", errBadPermissions, path, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		f.Close()
		return nil, fmt.Errorf("%w %s: mode %04o lets the group or others write to it (use chmod 600)", errBadPermissions, path, fi.Mode().Perm())
	}
	return f, nil
}
