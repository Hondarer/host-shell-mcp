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
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/transform"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// defaultExcludeDirs は検索で既定で降りないディレクトリです。
var defaultExcludeDirs = []string{".git", ".hg", ".svn"}

// errStop は走査を途中で終えるための目印です。
var errStop = errors.New("stop walking")

// walkFiles は root 以下を走査します。読めない項目は数えて飛ばします。
// fn には、パスと、root からの相対パス ("/" 区切り) を渡します。
func walkFiles(ctx context.Context, root string, excludeDirs []string, fn func(path, rel string, d fs.DirEntry) error) (skipped int, err error) {
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
		rel := d.Name()
		if r, err := filepath.Rel(root, path); err == nil && r != "." {
			rel = filepath.ToSlash(r)
		}
		return fn(path, rel, d)
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return skipped, err
}

// SearchGrepInput は search_grep の入力です。
type SearchGrepInput struct {
	Pattern      string   `json:"pattern" jsonschema:"regular expression (Go RE2 syntax: no lookaround or backreferences). Set literal to search for a plain string"`
	Path         string   `json:"path,omitempty" jsonschema:"root directory or a single file (default: current directory)"`
	Include      string   `json:"include,omitempty" jsonschema:"glob for files to search. Without a slash it matches the base name (*.go, *.{c,h}); with a slash it matches the path relative to path (src/**/*.ts)"`
	ExcludeDirs  []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git .hg .svn). Passing a list replaces the default"`
	IgnoreCase   bool     `json:"ignore_case,omitempty" jsonschema:"case-insensitive match"`
	Literal      bool     `json:"literal,omitempty" jsonschema:"treat pattern as a plain string, not a regular expression"`
	OutputMode   string   `json:"output_mode,omitempty" jsonschema:"content (default: matching lines), files (only paths of matching files) or count (number of matching lines per file)"`
	Context      int      `json:"context,omitempty" jsonschema:"content mode: lines of context to return before and after each match (like grep -C)"`
	Before       int      `json:"before,omitempty" jsonschema:"content mode: lines of context before each match (overrides context, like grep -B)"`
	After        int      `json:"after,omitempty" jsonschema:"content mode: lines of context after each match (overrides context, like grep -A)"`
	MaxMatches   int      `json:"max_matches,omitempty" jsonschema:"maximum matching lines (content mode) or files (files and count modes) to return (default 200)"`
	MaxFileBytes int64    `json:"max_file_bytes,omitempty" jsonschema:"skip files larger than this (default 10 MiB)"`
	Charset      string   `json:"charset,omitempty" jsonschema:"encoding of the files: auto (default: BOM, then UTF-8 if valid, otherwise the host file code page, decided per line), utf-8, system, or a charset name such as shift_jis, euc-jp, utf-16le"`
}

// Match は検索で一致した 1 行です。
type Match struct {
	Path    string   `json:"path"`
	Line    int      `json:"line" jsonschema:"1-based line number"`
	Text    string   `json:"text" jsonschema:"the matching line without its line ending (cut at 500 bytes)"`
	Before  []string `json:"before,omitempty" jsonschema:"context lines before the match, oldest first"`
	After   []string `json:"after,omitempty" jsonschema:"context lines after the match"`
	Charset string   `json:"charset,omitempty" jsonschema:"set when the line was converted to UTF-8 from this encoding"`
}

// FileMatches は 1 ファイルの一致の件数です。
type FileMatches struct {
	Path  string `json:"path"`
	Count int    `json:"count,omitempty" jsonschema:"count mode: number of matching lines"`
}

// SearchGrepOutput は search_grep の出力です。
type SearchGrepOutput struct {
	Matches      []Match       `json:"matches,omitempty" jsonschema:"content mode: matching lines"`
	Files        []FileMatches `json:"files,omitempty" jsonschema:"files and count modes: matching files"`
	FilesScanned int           `json:"files_scanned"`
	FilesMatched int           `json:"files_matched"`
	Skipped      int           `json:"skipped,omitempty" jsonschema:"entries skipped because they could not be read"`
	Truncated    bool          `json:"truncated,omitempty" jsonschema:"max_matches was reached; narrow the search or raise the limit"`
}

const maxMatchLineBytes = 500

// grepAction は一致を受け取った側が、検索をどう続けるかを示します。
type grepAction int

const (
	grepContinue grepAction = iota // 同じファイルの検索を続ける
	grepNextFile                   // 次のファイルへ進む
	grepStop                       // 検索を終える
)

func (d *Deps) searchGrep(ctx context.Context, _ *mcp.CallToolRequest, in SearchGrepInput) (*mcp.CallToolResult, SearchGrepOutput, error) {
	pat := in.Pattern
	if in.Literal {
		pat = regexp.QuoteMeta(pat)
	}
	if in.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, SearchGrepOutput{}, fmt.Errorf("invalid pattern (Go RE2 syntax; set literal for a plain string): %w", err)
	}
	include, err := compileGlob(in.Include)
	if err != nil {
		return nil, SearchGrepOutput{}, fmt.Errorf("include: %w", err)
	}
	ch, err := textenc.Parse(in.Charset, textenc.File, true, textenc.UTF8)
	if err != nil {
		return nil, SearchGrepOutput{}, fmt.Errorf("charset: %w", err)
	}
	mode := orDefault(in.OutputMode, "content")
	opt := grepOptions{re: re, ch: ch}
	switch mode {
	case "content":
		opt.before, opt.after = in.Context, in.Context
		if in.Before > 0 {
			opt.before = in.Before
		}
		if in.After > 0 {
			opt.after = in.After
		}
	case "files", "count":
	default:
		return nil, SearchGrepOutput{}, fmt.Errorf("unknown output_mode %q (use content, files or count)", in.OutputMode)
	}
	maxMatches := orDefaultInt(in.MaxMatches, 200)
	maxFile := in.MaxFileBytes
	if maxFile <= 0 {
		maxFile = 10 << 20
	}

	out := SearchGrepOutput{}
	skipped, err := walkFiles(ctx, orDefault(in.Path, "."), in.ExcludeDirs, func(path, rel string, de fs.DirEntry) error {
		if !de.Type().IsRegular() || !include.match(rel, de.Name()) {
			return nil
		}
		if info, err := de.Info(); err != nil || info.Size() > maxFile {
			return nil
		}
		out.FilesScanned++
		count := 0
		err := grepFile(path, opt, func(m Match) grepAction {
			count++
			switch mode {
			case "files":
				return grepNextFile
			case "count":
				return grepContinue
			}
			if len(out.Matches) >= maxMatches {
				out.Truncated = true
				return grepStop
			}
			out.Matches = append(out.Matches, m)
			return grepContinue
		})
		if count > 0 {
			out.FilesMatched++
			if mode != "content" {
				if len(out.Files) >= maxMatches {
					out.Truncated = true
					return errStop
				}
				fm := FileMatches{Path: path}
				if mode == "count" {
					fm.Count = count
				}
				out.Files = append(out.Files, fm)
			}
		}
		return err
	})
	if err != nil {
		return nil, SearchGrepOutput{}, err
	}
	out.Skipped += skipped
	return nil, out, nil
}

// grepOptions は 1 ファイルの検索の指定です。
type grepOptions struct {
	re            *regexp.Regexp
	ch            textenc.Choice
	before, after int
}

// grepFile は 1 ファイルを行ごとに UTF-8 に変換しながら検索します。
// BOM のない UTF-16 のファイルは、文字コードを指定しない限りバイナリとみなして飛ばします。
func grepFile(path string, opt grepOptions, emit func(Match) grepAction) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ch := opt.ch
	br := bufio.NewReaderSize(f, 64*1024)
	head, _ := br.Peek(8000)
	if bomEnc, n, ok := textenc.DetectBOM(head); ok && (ch.Auto || ch.Enc.Name == bomEnc.Name) {
		br.Discard(n)
		ch = textenc.Choice{Enc: bomEnc}
	} else if ch.Enc.ASCIICompatible() && bytes.IndexByte(head, 0) >= 0 {
		return nil // バイナリとみなす。
	}

	var src io.Reader = br
	var conv func([]byte) ([]byte, string)
	switch {
	case ch.Auto:
		conv = func(b []byte) ([]byte, string) {
			if utf8.Valid(b) {
				return b, ""
			}
			return []byte(ch.Enc.Decode(b)), ch.Enc.Name
		}
	case !ch.Enc.IsUTF8():
		src = transform.NewReader(br, ch.Enc.NewDecoder())
		conv = func(b []byte) ([]byte, string) { return b, ch.Enc.Name }
	}

	var (
		before  []string // 直前の行 (前の文脈)
		pending *Match   // 後の文脈を集めている一致
		stopped bool
	)
	flush := func() {
		if pending != nil && !stopped {
			if emit(*pending) == grepStop {
				stopped = true
			}
		}
		pending = nil
	}

	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for n := 1; sc.Scan() && !stopped; n++ {
		line, cs := sc.Bytes(), ""
		if conv != nil {
			line, cs = conv(line)
		}
		text := clipLine(line)
		if pending != nil {
			pending.After = append(pending.After, text)
			if len(pending.After) >= opt.after {
				flush()
			}
		}
		if opt.re.Match(line) {
			if opt.after == 0 && opt.before == 0 {
				// 文脈を返さない場合は、受け取った側の指示に従って先へ進む。
				switch emit(Match{Path: path, Line: n, Text: text, Charset: cs}) {
				case grepNextFile:
					return nil
				case grepStop:
					return errStop
				}
			} else {
				flush()
				pending = &Match{Path: path, Line: n, Text: text, Before: slices.Clone(before), Charset: cs}
				if opt.after == 0 {
					flush()
				}
			}
		}
		if opt.before > 0 {
			before = append(before, text)
			if len(before) > opt.before {
				before = before[1:]
			}
		}
	}
	flush()
	if stopped {
		return errStop
	}
	return nil // 長すぎる行などで読み取りを打ち切ったファイルは、そこまでの結果だけを返す。
}

// clipLine は 1 行を上限の長さに切り詰め、UTF-8 の文字列にします。
func clipLine(line []byte) string {
	if len(line) > maxMatchLineBytes {
		line = line[:maxMatchLineBytes]
		line = line[:len(line)-textenc.IncompleteTail(line)]
	}
	return string(bytes.ToValidUTF8(line, []byte("�")))
}

// SearchFilesInput は search_files の入力です。
type SearchFilesInput struct {
	Pattern     string   `json:"pattern,omitempty" jsonschema:"glob. Without a slash it matches the base name (*.log, *.{yml,yaml}); with a slash it matches the path relative to path (**/test/*.py, src/*/main.go). Default: all"`
	Path        string   `json:"path,omitempty" jsonschema:"root directory (default: current directory)"`
	Type        string   `json:"type,omitempty" jsonschema:"file, dir or empty for both"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git .hg .svn). Passing a list replaces the default"`
	Sort        string   `json:"sort,omitempty" jsonschema:"path (default: walk order) or mtime (most recently modified first)"`
	MaxResults  int      `json:"max_results,omitempty" jsonschema:"maximum paths to return (default 1000)"`
}

// SearchFilesOutput は search_files の出力です。
type SearchFilesOutput struct {
	Paths     []string `json:"paths"`
	Skipped   int      `json:"skipped,omitempty" jsonschema:"entries skipped because they could not be read"`
	Truncated bool     `json:"truncated,omitempty" jsonschema:"max_results was reached"`
}

// maxSortCandidates は更新日時で並べるときに集める候補の上限です。
const maxSortCandidates = 100000

func (d *Deps) searchFiles(ctx context.Context, _ *mcp.CallToolRequest, in SearchFilesInput) (*mcp.CallToolResult, SearchFilesOutput, error) {
	pattern, err := compileGlob(in.Pattern)
	if err != nil {
		return nil, SearchFilesOutput{}, fmt.Errorf("pattern: %w", err)
	}
	switch in.Type {
	case "", "file", "dir":
	default:
		return nil, SearchFilesOutput{}, fmt.Errorf("unknown type %q (use file, dir or omit)", in.Type)
	}
	byMtime := false
	switch in.Sort {
	case "", "path":
	case "mtime":
		byMtime = true
	default:
		return nil, SearchFilesOutput{}, fmt.Errorf("unknown sort %q (use path or mtime)", in.Sort)
	}
	max := orDefaultInt(in.MaxResults, 1000)
	root := orDefault(in.Path, ".")

	type found struct {
		path  string
		mtime time.Time
	}
	var all []found
	out := SearchFilesOutput{Paths: []string{}}
	skipped, err := walkFiles(ctx, root, in.ExcludeDirs, func(path, rel string, de fs.DirEntry) error {
		if path == root {
			return nil
		}
		if (in.Type == "file" && de.IsDir()) || (in.Type == "dir" && !de.IsDir()) {
			return nil
		}
		if !pattern.match(rel, de.Name()) {
			return nil
		}
		if !byMtime {
			if len(out.Paths) >= max {
				out.Truncated = true
				return errStop
			}
			out.Paths = append(out.Paths, path)
			return nil
		}
		if len(all) >= maxSortCandidates {
			out.Truncated = true
			return errStop
		}
		f := found{path: path}
		if info, err := de.Info(); err == nil {
			f.mtime = info.ModTime()
		}
		all = append(all, f)
		return nil
	})
	if err != nil {
		return nil, SearchFilesOutput{}, err
	}
	if byMtime {
		slices.SortStableFunc(all, func(a, b found) int { return b.mtime.Compare(a.mtime) })
		if len(all) > max {
			all, out.Truncated = all[:max], true
		}
		for _, f := range all {
			out.Paths = append(out.Paths, f.path)
		}
	}
	out.Skipped += skipped
	return nil, out, nil
}

// FsManifestInput は fs_manifest の入力です。
type FsManifestInput struct {
	Path        string   `json:"path,omitempty" jsonschema:"root directory (default: current directory)"`
	Include     string   `json:"include,omitempty" jsonschema:"glob for files to list (same syntax as search_files pattern), e.g. *.c or src/**/*.h"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git .hg .svn). Passing a list replaces the default"`
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
	include, err := compileGlob(in.Include)
	if err != nil {
		return nil, FsManifestOutput{}, fmt.Errorf("include: %w", err)
	}
	root := orDefault(in.Path, ".")
	max := orDefaultInt(in.MaxFiles, 2000)
	out := FsManifestOutput{Root: absPath(root), Files: []ManifestEntry{}}
	skipped, err := walkFiles(ctx, root, in.ExcludeDirs, func(path, rel string, de fs.DirEntry) error {
		if !de.Type().IsRegular() || !include.match(rel, de.Name()) {
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
		out.Files = append(out.Files, ManifestEntry{Path: rel, Size: size, SHA256: sum})
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
