//go:build !windows

package tools

import "golang.org/x/sys/unix"

// availableBytes returns the space available to this user on the file system containing path.
func availableBytes(path string) (uint64, bool) {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
