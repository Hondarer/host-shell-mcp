package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FsMkdirInput is the input of fs_mkdir.
type FsMkdirInput struct {
	Path string `json:"path" jsonschema:"directory to create. Missing parent directories are created too; an existing directory is not an error"`
}

// FsMkdirOutput is the output of fs_mkdir.
type FsMkdirOutput struct {
	Path    string `json:"path"`
	Created bool   `json:"created" jsonschema:"false if the directory already existed"`
}

func (d *Deps) fsMkdir(_ context.Context, _ *mcp.CallToolRequest, in FsMkdirInput) (*mcp.CallToolResult, FsMkdirOutput, error) {
	if in.Path == "" {
		return nil, FsMkdirOutput{}, errors.New("path is required")
	}
	out := FsMkdirOutput{Path: absPath(in.Path)}
	if info, err := os.Stat(in.Path); err == nil {
		if !info.IsDir() {
			return nil, FsMkdirOutput{}, fmt.Errorf("%s exists and is not a directory", in.Path)
		}
		return nil, out, nil
	}
	if err := os.MkdirAll(in.Path, 0o755); err != nil {
		return nil, FsMkdirOutput{}, err
	}
	out.Created = true
	return nil, out, nil
}

// FsMoveInput is the input of fs_move.
type FsMoveInput struct {
	Source      string `json:"source" jsonschema:"file or directory to move or rename"`
	Destination string `json:"destination" jsonschema:"new path (the full target path, not a directory to move into)"`
	Overwrite   bool   `json:"overwrite,omitempty" jsonschema:"replace an existing destination file. An existing destination directory is never replaced"`
	MakeDirs    bool   `json:"make_dirs,omitempty" jsonschema:"create parent directories of destination if missing"`
}

// FsMoveOutput is the output of fs_move.
type FsMoveOutput struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Copied      bool   `json:"copied,omitempty" jsonschema:"moved across file systems by copying and deleting the source"`
}

func (d *Deps) fsMove(_ context.Context, _ *mcp.CallToolRequest, in FsMoveInput) (*mcp.CallToolResult, FsMoveOutput, error) {
	src, err := os.Lstat(in.Source)
	if err != nil {
		return nil, FsMoveOutput{}, err
	}
	if err := prepareDestination(in.Destination, in.Overwrite, in.MakeDirs); err != nil {
		return nil, FsMoveOutput{}, err
	}
	out := FsMoveOutput{Source: absPath(in.Source), Destination: absPath(in.Destination)}
	err = os.Rename(in.Source, in.Destination)
	if err == nil {
		return nil, out, nil
	}
	if !isCrossDevice(err) {
		return nil, FsMoveOutput{}, err
	}
	// Across file systems, copy and then remove the source.
	if _, cerr := copyTree(in.Source, in.Destination, src, true); cerr != nil {
		if !src.IsDir() {
			os.Remove(in.Destination)
		}
		return nil, FsMoveOutput{}, fmt.Errorf("copy across file systems: %w", cerr)
	}
	if err := os.RemoveAll(in.Source); err != nil {
		return nil, FsMoveOutput{}, fmt.Errorf("copied to %s but could not remove the source: %w", in.Destination, err)
	}
	out.Copied = true
	return nil, out, nil
}

// FsCopyInput is the input of fs_copy.
type FsCopyInput struct {
	Source      string `json:"source" jsonschema:"file or directory to copy"`
	Destination string `json:"destination" jsonschema:"path of the copy (the full target path, not a directory to copy into)"`
	Recursive   bool   `json:"recursive,omitempty" jsonschema:"required to copy a directory and its contents"`
	Overwrite   bool   `json:"overwrite,omitempty" jsonschema:"replace existing files at the destination (directories are merged)"`
	MakeDirs    bool   `json:"make_dirs,omitempty" jsonschema:"create parent directories of destination if missing"`
}

// FsCopyOutput is the output of fs_copy.
type FsCopyOutput struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Files       int    `json:"files" jsonschema:"files copied"`
	Dirs        int    `json:"dirs,omitempty" jsonschema:"directories created"`
	Symlinks    int    `json:"symlinks,omitempty" jsonschema:"symbolic links recreated (not followed)"`
	Bytes       int64  `json:"bytes" jsonschema:"bytes copied"`
}

func (d *Deps) fsCopy(_ context.Context, _ *mcp.CallToolRequest, in FsCopyInput) (*mcp.CallToolResult, FsCopyOutput, error) {
	src, err := os.Lstat(in.Source)
	if err != nil {
		return nil, FsCopyOutput{}, err
	}
	if src.IsDir() && !in.Recursive {
		return nil, FsCopyOutput{}, fmt.Errorf("%s is a directory; set recursive to copy it", in.Source)
	}
	if src.IsDir() && isWithin(in.Destination, in.Source) {
		return nil, FsCopyOutput{}, errors.New("destination is inside the source directory")
	}
	if dst, err := os.Lstat(in.Destination); err == nil && src.IsDir() && dst.IsDir() {
		// Directory onto directory merges the contents when overwrite is set.
		if !in.Overwrite {
			return nil, FsCopyOutput{}, fmt.Errorf("%s already exists; set overwrite to merge into it", in.Destination)
		}
	} else if err := prepareDestination(in.Destination, in.Overwrite, in.MakeDirs); err != nil {
		return nil, FsCopyOutput{}, err
	}
	st, err := copyTree(in.Source, in.Destination, src, in.Overwrite)
	out := FsCopyOutput{Source: absPath(in.Source), Destination: absPath(in.Destination),
		Files: st.files, Dirs: st.dirs, Symlinks: st.links, Bytes: st.bytes}
	if err != nil {
		return nil, FsCopyOutput{}, fmt.Errorf("%w (copied %d files before the error)", err, st.files)
	}
	return nil, out, nil
}

// FsRemoveInput is the input of fs_remove.
type FsRemoveInput struct {
	Path      string `json:"path" jsonschema:"file, symbolic link or directory to delete. A symbolic link is removed, not its target"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"required to delete a non-empty directory and everything under it"`
	MissingOK bool   `json:"missing_ok,omitempty" jsonschema:"do not fail if path does not exist"`
}

// FsRemoveOutput is the output of fs_remove.
type FsRemoveOutput struct {
	Path    string `json:"path"`
	Removed bool   `json:"removed" jsonschema:"false if path did not exist (missing_ok)"`
	Type    string `json:"type,omitempty" jsonschema:"file, dir, symlink or other"`
}

func (d *Deps) fsRemove(_ context.Context, _ *mcp.CallToolRequest, in FsRemoveInput) (*mcp.CallToolResult, FsRemoveOutput, error) {
	if in.Path == "" {
		return nil, FsRemoveOutput{}, errors.New("path is required")
	}
	out := FsRemoveOutput{Path: absPath(in.Path)}
	info, err := os.Lstat(in.Path)
	if errors.Is(err, fs.ErrNotExist) && in.MissingOK {
		return nil, out, nil
	} else if err != nil {
		return nil, FsRemoveOutput{}, err
	}
	if protectedPath(out.Path) {
		return nil, FsRemoveOutput{}, fmt.Errorf("refusing to remove %s (file system root or home directory)", out.Path)
	}
	out.Type = fileType(info.Mode())
	if info.IsDir() && in.Recursive {
		err = os.RemoveAll(in.Path)
	} else {
		err = os.Remove(in.Path)
		if err != nil && info.IsDir() {
			if ents, rerr := os.ReadDir(in.Path); rerr == nil && len(ents) > 0 {
				err = fmt.Errorf("%s is a non-empty directory (%d entries); set recursive to delete it with its contents", in.Path, len(ents))
			}
		}
	}
	if err != nil {
		return nil, FsRemoveOutput{}, err
	}
	out.Removed = true
	return nil, out, nil
}

// prepareDestination checks the destination's parent directory and any existing file.
func prepareDestination(dst string, overwrite, makeDirs bool) error {
	if info, err := os.Lstat(dst); err == nil {
		switch {
		case !overwrite:
			return fmt.Errorf("%s already exists; set overwrite to replace it", dst)
		case info.IsDir():
			return fmt.Errorf("%s is an existing directory; remove it first with fs_remove", dst)
		}
		return nil
	}
	parent := filepath.Dir(dst)
	if makeDirs {
		return os.MkdirAll(parent, 0o755)
	}
	if info, err := os.Stat(parent); err != nil {
		return fmt.Errorf("parent directory %s does not exist; set make_dirs to create it", parent)
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", parent)
	}
	return nil
}

type copyStats struct {
	files, dirs, links int
	bytes              int64
}

// copyTree copies src to dst. Directories are copied with their contents; symbolic links are recreated, not followed.
func copyTree(src, dst string, info fs.FileInfo, overwrite bool) (copyStats, error) {
	var st copyStats
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return st, copySymlink(src, dst, overwrite, &st)
	case info.Mode().IsRegular():
		return st, copyFile(src, dst, info.Mode().Perm(), overwrite, &st)
	case !info.IsDir():
		return st, fmt.Errorf("%s is not a regular file, directory or symbolic link", src)
	}
	err := filepath.WalkDir(src, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		info, err := de.Info()
		if err != nil {
			return err
		}
		switch {
		case de.IsDir():
			if err := os.Mkdir(target, info.Mode().Perm()|0o700); err == nil {
				st.dirs++
			} else if !errors.Is(err, fs.ErrExist) {
				return err
			}
			return nil
		case de.Type()&fs.ModeSymlink != 0:
			return copySymlink(path, target, overwrite, &st)
		case de.Type().IsRegular():
			return copyFile(path, target, info.Mode().Perm(), overwrite, &st)
		}
		return nil // Skip device files and the like.
	})
	return st, err
}

func copyFile(src, dst string, perm fs.FileMode, overwrite bool, st *copyStats) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !overwrite {
		flag |= os.O_EXCL
	}
	out, err := os.OpenFile(dst, flag, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if info, err := os.Stat(src); err == nil {
		os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	st.files++
	st.bytes += n
	return nil
}

func copySymlink(src, dst string, overwrite bool, st *copyStats) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if overwrite {
		if info, err := os.Lstat(dst); err == nil && !info.IsDir() {
			os.Remove(dst)
		}
	}
	if err := os.Symlink(target, dst); err != nil {
		return err
	}
	st.links++
	return nil
}

// isWithin reports whether path is dir or below it.
func isWithin(path, dir string) bool {
	p, d := absPath(path), absPath(dir)
	rel, err := filepath.Rel(d, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// protectedPath reports whether path must not be removed (a file system root or the home directory).
func protectedPath(abs string) bool {
	clean := filepath.Clean(abs)
	if clean == filepath.Dir(clean) || strings.TrimSuffix(clean, string(filepath.Separator)) == filepath.VolumeName(clean) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && strings.EqualFold(filepath.Clean(home), clean) {
		return true
	}
	return false
}
