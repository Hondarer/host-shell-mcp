// Package ownerdir creates and checks directories that only the current user can use.
package ownerdir

import "os"

// Ensure creates the directory at path, with missing parents, for the current user only. An
// existing directory must belong to the current user and grant nobody else access: in a shared
// temporary directory another user can create the path first and would otherwise reach what is
// placed inside. Windows applies the owner-only DACL instead of checking.
func Ensure(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return check(path)
}
