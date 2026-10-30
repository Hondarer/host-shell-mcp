package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// replaceAtomically writes data to a temporary file beside path and renames it over the target
// (following symlinks). It preserves existing permissions, owner, and group (failing before modifying
// the target if ownership cannot be preserved), or applies perm for a new file. ACLs, extended
// attributes, and hard links are not preserved.
func replaceAtomically(path string, data []byte, perm fs.FileMode) error {
	target := path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if target, err = filepath.EvalSymlinks(path); err != nil {
			return fmt.Errorf("resolve symbolic link %s: %w", path, err)
		}
	}
	info, err := os.Stat(target)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", target)
		}
		perm = info.Mode().Perm()
	case errors.Is(err, fs.ErrNotExist):
		info = nil
	default:
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".host-shell-tmp-*")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(perm); err != nil {
			return err
		}
	}
	if info != nil {
		if err := keepOwner(tmp, info); err != nil {
			return fmt.Errorf("%s: %w; write without atomic to keep the owner", target, err)
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("%w (another process may have the file open; write without atomic)", err)
		}
		return err
	}
	done = true
	return nil
}
