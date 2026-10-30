package tools

import (
	"io/fs"
	"os"
)

// keepOwner does nothing on Windows: a new file gets the ACL inherited from its directory.
func keepOwner(*os.File, fs.FileInfo) error { return nil }
