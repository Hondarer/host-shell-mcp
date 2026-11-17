//go:build !windows

package ownerdir

import (
	"fmt"
	"os"
	"syscall"
)

// Restrict limits the directory at path to the current user (mode 0700).
func Restrict(path string) error { return os.Chmod(path, 0700) }

func check(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s belongs to another user (uid %d); remove it or choose another directory", path, st.Uid)
	}
	if perm := fi.Mode().Perm(); perm&0077 != 0 {
		return fmt.Errorf("%s is accessible to other users (mode %04o); run chmod 700 on it", path, perm)
	}
	return nil
}
