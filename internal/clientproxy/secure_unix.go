//go:build !windows

package clientproxy

import "os"

func secureDir(path string) error { return os.Chmod(path, 0700) }
