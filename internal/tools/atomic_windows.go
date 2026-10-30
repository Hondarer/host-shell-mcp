package tools

import (
	"io/fs"
	"os"
)

// keepOwner is a no-op on Windows, where new files inherit ACLs from the parent directory.
func keepOwner(*os.File, fs.FileInfo) error { return nil }
