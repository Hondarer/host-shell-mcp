package tools

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultExcludeDirs は検索で既定で降りないディレクトリです。
var defaultExcludeDirs = []string{".git", ".hg", ".svn"}

// errStop は走査を途中で終えるための目印です。
var errStop = errors.New("stop walking")

// walkFiles は root 以下を走査します。読めない項目は数えて飛ばします。
func walkFiles(ctx context.Context, root string, excludeDirs []string, fn func(path string, d fs.DirEntry) error) (skipped int, err error) {
	if excludeDirs == nil {
		excludeDirs = defaultExcludeDirs
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if path == root {
				return err
			}
			skipped++
			return nil
		}
		if d.IsDir() && path != root && slices.Contains(excludeDirs, d.Name()) {
			return fs.SkipDir
		}
		return fn(path, d)
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return skipped, err
}

// matchName は glob が空の場合は常に一致とみなします。
func matchName(glob, name string) (bool, error) {
	if glob == "" {
		return true, nil
	}
	return filepath.Match(glob, name)
}

// SearchGrepInput は search_grep の入力です。
type SearchGrepInput struct {
	Pattern      string   `json:"pattern" jsonschema:"regular expression (Go RE2 syntax)"`
	Path         string   `json:"path,omitempty" jsonschema:"root directory or file (default: current directory)"`
	Include      string   `json:"include,omitempty" jsonschema:"glob matched against the base name, e.g. *.go"`
	ExcludeDirs  []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git .hg .svn)"`
	IgnoreCase   bool     `json:"ignore_case,omitempty"`
	MaxMatches   int      `json:"max_matches,omitempty" jsonschema:"maximum matches to return (default 200)"`
	MaxFileBytes int64    `json:"max_file_bytes,omitempty" jsonschema:"skip files larger than this (default 10 MiB)"`
}

// Match は検索で一致した 1 行です。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SearchGrepOutput は search_grep の出力です。
type SearchGrepOutput struct {
	Matches      []Match `json:"matches"`
	FilesScanned int     `json:"files_scanned"`
	Skipped      int     `json:"skipped,omitempty" jsonschema:"entries skipped because they could not be read"`
	Truncated    bool    `json:"truncated,omitempty"`
}

const maxMatchLineBytes = 500

func (d *Deps) searchGrep(ctx context.Context, _ *mcp.CallToolRequest, in SearchGrepInput) (*mcp.CallToolResult, SearchGrepOutput, error) {
	pat := in.Pattern
	if in.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, SearchGrepOutput{}, err
	}
	if _, err := matchName(in.Include, ""); err != nil {
		return nil, SearchGrepOutput{}, fmt.Errorf("invalid include: %w", err)
	}
	maxMatches := orDefaultInt(in.MaxMatches, 200)
	maxFile := in.MaxFileBytes
	if maxFile <= 0 {
		maxFile = 10 << 20
	}

	out := SearchGrepOutput{Matches: []Match{}}
	skipped, err := walkFiles(ctx, orDefault(in.Path, "."), in.ExcludeDirs, func(path string, de fs.DirEntry) error {
		if !de.Type().IsRegular() {
			return nil
		}
		if ok, _ := matchName(in.Include, de.Name()); !ok {
			return nil
		}
		if info, err := de.Info(); err != nil || info.Size() > maxFile {
			return nil
		}
		out.FilesScanned++
		return grepFile(path, re, func(m Match) bool {
			if len(out.Matches) >= maxMatches {
				out.Truncated = true
				return false
			}
			out.Matches = append(out.Matches, m)
			return true
		})
	})
	if err != nil {
		return nil, SearchGrepOutput{}, err
	}
	out.Skipped += skipped
	return nil, out, nil
}

// grepFile は 1 ファイルを行ごとに検索します。バイナリ ファイルは飛ばします。
// TODO: UTF-8 以外 (Shift_JIS、UTF-16 など) のテキスト ファイルへの対応を検討する。
func grepFile(path string, re *regexp.Regexp, emit func(Match) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64*1024)
	if head, _ := br.Peek(8000); bytes.IndexByte(head, 0) >= 0 {
		return nil
	}
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Bytes()
		if !re.Match(line) {
			continue
		}
		if len(line) > maxMatchLineBytes {
			line = line[:maxMatchLineBytes]
		}
		if !emit(Match{Path: path, Line: n, Text: string(bytes.ToValidUTF8(line, []byte("�")))}) {
			return errStop
		}
	}
	return nil // 長すぎる行などで読み取りを打ち切ったファイルは、そこまでの結果だけを返す。
}

// SearchFilesInput は search_files の入力です。
type SearchFilesInput struct {
	Pattern     string   `json:"pattern,omitempty" jsonschema:"glob matched against the base name, e.g. *.log (default: all)"`
	Path        string   `json:"path,omitempty" jsonschema:"root directory (default: current directory)"`
	Type        string   `json:"type,omitempty" jsonschema:"file, dir or empty for both"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git .hg .svn)"`
	MaxResults  int      `json:"max_results,omitempty" jsonschema:"maximum paths to return (default 1000)"`
}

// SearchFilesOutput は search_files の出力です。
type SearchFilesOutput struct {
	Paths     []string `json:"paths"`
	Skipped   int      `json:"skipped,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

func (d *Deps) searchFiles(ctx context.Context, _ *mcp.CallToolRequest, in SearchFilesInput) (*mcp.CallToolResult, SearchFilesOutput, error) {
	if _, err := matchName(in.Pattern, ""); err != nil {
		return nil, SearchFilesOutput{}, fmt.Errorf("invalid pattern: %w", err)
	}
	max := orDefaultInt(in.MaxResults, 1000)
	root := orDefault(in.Path, ".")
	out := SearchFilesOutput{Paths: []string{}}
	skipped, err := walkFiles(ctx, root, in.ExcludeDirs, func(path string, de fs.DirEntry) error {
		if path == root {
			return nil
		}
		switch in.Type {
		case "file":
			if de.IsDir() {
				return nil
			}
		case "dir":
			if !de.IsDir() {
				return nil
			}
		}
		if ok, _ := matchName(in.Pattern, de.Name()); !ok {
			return nil
		}
		if len(out.Paths) >= max {
			out.Truncated = true
			return errStop
		}
		out.Paths = append(out.Paths, path)
		return nil
	})
	if err != nil {
		return nil, SearchFilesOutput{}, err
	}
	out.Skipped += skipped
	return nil, out, nil
}

// FsManifestInput は fs_manifest の入力です。
type FsManifestInput struct {
	Path        string   `json:"path,omitempty" jsonschema:"root directory (default: current directory)"`
	Include     string   `json:"include,omitempty" jsonschema:"glob matched against the base name, e.g. *.c"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git .hg .svn)"`
	MaxFiles    int      `json:"max_files,omitempty" jsonschema:"maximum files to return (default 2000)"`
}

// ManifestEntry は 1 ファイルのパスとハッシュです。
type ManifestEntry struct {
	Path   string `json:"path" jsonschema:"path relative to root, with forward slashes"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// FsManifestOutput は fs_manifest の出力です。
type FsManifestOutput struct {
	Root      string          `json:"root"`
	Files     []ManifestEntry `json:"files"`
	Skipped   int             `json:"skipped,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

func (d *Deps) fsManifest(ctx context.Context, _ *mcp.CallToolRequest, in FsManifestInput) (*mcp.CallToolResult, FsManifestOutput, error) {
	if _, err := matchName(in.Include, ""); err != nil {
		return nil, FsManifestOutput{}, fmt.Errorf("invalid include: %w", err)
	}
	root := orDefault(in.Path, ".")
	max := orDefaultInt(in.MaxFiles, 2000)
	out := FsManifestOutput{Root: absPath(root), Files: []ManifestEntry{}}
	skipped, err := walkFiles(ctx, root, in.ExcludeDirs, func(path string, de fs.DirEntry) error {
		if !de.Type().IsRegular() {
			return nil
		}
		if ok, _ := matchName(in.Include, de.Name()); !ok {
			return nil
		}
		if len(out.Files) >= max {
			out.Truncated = true
			return errStop
		}
		size, sum, err := hashFile(path)
		if err != nil {
			out.Skipped++
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		out.Files = append(out.Files, ManifestEntry{Path: filepath.ToSlash(rel), Size: size, SHA256: sum})
		return nil
	})
	if err != nil {
		return nil, FsManifestOutput{}, err
	}
	out.Skipped += skipped
	return nil, out, nil
}

func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
