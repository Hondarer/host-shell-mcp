//go:build !windows

package tools

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// keepOwner gives f the owner and group of the file described by info.
func keepOwner(f *os.File, info fs.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	uid, gid := int(st.Uid), int(st.Gid)
	if uid == os.Getuid() && gid == os.Getegid() {
		return nil // a new file gets these anyway
	}
	if err := f.Chown(uid, gid); err != nil {
		return fmt.Errorf("cannot keep the owner %d:%d of the file (%v)", uid, gid, err)
	}
	return nil
}
