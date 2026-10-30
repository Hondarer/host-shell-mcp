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

// defaultExcludeDirs are the directories searches skip by default.
var defaultExcludeDirs = []string{".git", ".hg", ".svn"}

// errStop marks an early end of the walk.
var errStop = errors.New("stop walking")

// walkFiles walks everything under root. Unreadable entries are counted and skipped.
// With useGitignore, entries git ignores are skipped too (see gitignore).
// fn receives the path and the path relative to root ("/" separated).
func walkFiles(ctx context.Context, root string, excludeDirs []string, useGitignore bool, fn func(path, rel string, d fs.DirEntry) error) (skipped int, err error) {
	if excludeDirs == nil {
		excludeDirs = defaultExcludeDirs
	}
	var ign *gitignore
	if fi, err := os.Stat(root); useGitignore && err == nil && fi.IsDir() {
		ign = newGitignore(root)
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
		if ign != nil && path != root {
			abs, _ := filepath.Abs(path)
			if ign.ignored(abs, d.IsDir()) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
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

// SearchGrepInput is the input of search_grep.
type SearchGrepInput struct {
	Pattern      string   `json:"pattern" jsonschema:"Go RE2 regular expression (no lookaround or backreferences), or plain string when literal is set"`
	Path         string   `json:"path,omitempty" jsonschema:"root directory or file path (default: current directory)"`
	Include      string   `json:"include,omitempty" jsonschema:"file glob: matches base name without '/' (*.go, *.{c,h}) or relative path with '/' (src/**/*.ts)"`
	ExcludeDirs  []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git, .hg, .svn; replaces default when set)"`
	IgnoreCase   bool     `json:"ignore_case,omitempty" jsonschema:"case-insensitive match"`
	Literal      bool     `json:"literal,omitempty" jsonschema:"match pattern as a literal string"`
	OutputMode   string   `json:"output_mode,omitempty" jsonschema:"content (default: matching lines), files (matching file paths) or count (match count per file)"`
	Context      int      `json:"context,omitempty" jsonschema:"content mode: context lines before and after each match (grep -C)"`
	Before       int      `json:"before,omitempty" jsonschema:"content mode: context lines before each match (overrides context, grep -B)"`
	After        int      `json:"after,omitempty" jsonschema:"content mode: context lines after each match (overrides context, grep -A)"`
	MaxMatches   int      `json:"max_matches,omitempty" jsonschema:"max matching lines (content) or files (files/count) to return (default 200)"`
	MaxFileBytes int64    `json:"max_file_bytes,omitempty" jsonschema:"skip files larger than this in bytes (default 10 MiB)"`
	Charset      string   `json:"charset,omitempty" jsonschema:"file encoding: auto (default: BOM, then per-line UTF-8 if valid, else host file code page), utf-8, system, or a charset name (shift_jis, euc-jp, utf-16le, ...)"`
	Gitignore    *bool    `json:"gitignore,omitempty" jsonschema:"respect .gitignore and .git/info/exclude (default true)"`
	Multiline    bool     `json:"multiline,omitempty" jsonschema:"match across lines (. matches \\n, ^/$ match line boundaries); reports line, end_line and matched text (skips files > 32 MiB)"`
}

// Match is one matching line.
type Match struct {
	Path    string   `json:"path"`
	Line    int      `json:"line" jsonschema:"1-based line number"`
	EndLine int      `json:"end_line,omitempty" jsonschema:"multiline: 1-based end line number"`
	Text    string   `json:"text" jsonschema:"matching line without line ending (or matched text in multiline mode), truncated at 500 bytes"`
	Before  []string `json:"before,omitempty" jsonschema:"leading context lines"`
	After   []string `json:"after,omitempty" jsonschema:"trailing context lines"`
	Charset string   `json:"charset,omitempty" jsonschema:"source encoding when decoded to UTF-8"`
}

// FileMatches is the number of matches in one file.
type FileMatches struct {
	Path  string `json:"path"`
	Count int    `json:"count,omitempty" jsonschema:"count mode: matching lines"`
}

// SearchGrepOutput is the output of search_grep.
type SearchGrepOutput struct {
	Matches      []Match       `json:"matches,omitempty" jsonschema:"content mode: matching lines"`
	Files        []FileMatches `json:"files,omitempty" jsonschema:"files and count modes: matching files"`
	FilesScanned int           `json:"files_scanned"`
	FilesMatched int           `json:"files_matched"`
	Skipped      int           `json:"skipped,omitempty" jsonschema:"unreadable entries skipped"`
	SkippedLarge int           `json:"skipped_large,omitempty" jsonschema:"files skipped due to max_file_bytes (or 32 MiB in multiline mode)"`
	Truncated    bool          `json:"truncated,omitempty" jsonschema:"stopped at max_matches"`
}

const maxMatchLineBytes = 500

// maxMultilineBytes is the largest file searched with multiline, which reads the whole file.
// It is the same limit as for fs_edit.
const maxMultilineBytes = maxEditBytes

// grepAction tells how the search continues after a match is handled.
type grepAction int

const (
	grepContinue grepAction = iota // continue searching the same file
	grepNextFile                   // move on to the next file
	grepStop                       // end the search
)

func (d *Deps) searchGrep(ctx context.Context, _ *mcp.CallToolRequest, in SearchGrepInput) (*mcp.CallToolResult, SearchGrepOutput, error) {
	pat := in.Pattern
	if in.Literal {
		pat = regexp.QuoteMeta(pat)
	}
	if in.IgnoreCase {
		pat = "(?i)" + pat
	}
	if in.Multiline {
		pat = "(?sm)" + pat
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
	opt := grepOptions{re: re, ch: ch, multiline: in.Multiline}
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
	if in.Multiline && maxFile > maxMultilineBytes {
		maxFile = maxMultilineBytes
	}

	out := SearchGrepOutput{}
	skipped, err := walkFiles(ctx, orDefault(in.Path, "."), in.ExcludeDirs, in.Gitignore == nil || *in.Gitignore, func(path, rel string, de fs.DirEntry) error {
		if !de.Type().IsRegular() || !include.match(rel, de.Name()) {
			return nil
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		if info.Size() > maxFile {
			out.SkippedLarge++
			return nil
		}
		out.FilesScanned++
		count := 0
		search := grepFile
		if opt.multiline {
			search = grepFileMultiline
		}
		err = search(path, opt, func(m Match) grepAction {
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

// grepOptions specifies the search of one file.
type grepOptions struct {
	re            *regexp.Regexp
	ch            textenc.Choice
	before, after int
	multiline     bool
}

// grepFile searches one file line by line, converting it to UTF-8.
// Files with NUL bytes are treated as binary and skipped, unless they look like UTF-16 (DetectUTF16).
func grepFile(path string, opt grepOptions, emit func(Match) grepAction) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64*1024)
	ch, ok := fileEncoding(br, opt.ch)
	if !ok {
		return nil
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
		before  []string // preceding lines (leading context)
		pending *Match   // match collecting trailing context
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
				// Without context, follow the handler's instruction on how to continue.
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
	return nil // For a file whose reading stopped early (for example at an overlong line), return the results so far.
}

// fileEncoding decides the encoding of a file from its start (BOM, UTF-16, binary) and skips the BOM.
// ok is false for a binary file: one with NUL bytes that does not look like UTF-16.
func fileEncoding(br *bufio.Reader, ch textenc.Choice) (textenc.Choice, bool) {
	head, _ := br.Peek(8000)
	if bomEnc, n, ok := textenc.DetectBOM(head); ok && (ch.Auto || ch.Enc.Name == bomEnc.Name) {
		br.Discard(n)
		return textenc.Choice{Enc: bomEnc}, true
	} else if e, ok := textenc.DetectUTF16(head); ok && ch.Auto {
		return textenc.Choice{Enc: e}, true
	} else if ch.Enc.ASCIICompatible() && bytes.IndexByte(head, 0) >= 0 {
		return ch, false
	}
	return ch, true
}

// grepFileMultiline searches the whole file at once, so that matches can span lines.
// With automatic detection the file is UTF-8 if it is valid as a whole, otherwise the fallback encoding.
func grepFileMultiline(path string, opt grepOptions, emit func(Match) grepAction) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64*1024)
	ch, ok := fileEncoding(br, opt.ch)
	if !ok {
		return nil
	}
	raw, err := io.ReadAll(br)
	if err != nil {
		return nil
	}
	text, cs := raw, ""
	if !ch.Enc.IsUTF8() && !(ch.Auto && utf8.Valid(raw)) {
		text, cs = []byte(ch.Enc.Decode(raw)), ch.Enc.Name
	}

	// starts[i] is the offset where line i+1 begins.
	starts := []int{0}
	for i, b := range text {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	lineOf := func(off int) int { // 1-based line containing off
		i, _ := slices.BinarySearch(starts, off+1)
		return i
	}
	lineText := func(n int) string { // line n (1-based) without its line ending
		end := len(text)
		if n < len(starts) {
			end = starts[n]
		}
		return clipLine(bytes.TrimRight(text[starts[n-1]:end], "\r\n"))
	}
	for _, m := range opt.re.FindAllIndex(text, -1) {
		if m[0] == m[1] {
			continue // empty matches carry nothing to show
		}
		first, last := lineOf(m[0]), lineOf(m[1]-1)
		match := Match{Path: path, Line: first, EndLine: last, Text: clipLine(text[m[0]:m[1]]), Charset: cs}
		for n := max(1, first-opt.before); n < first; n++ {
			match.Before = append(match.Before, lineText(n))
		}
		for n := last + 1; n <= min(len(starts), last+opt.after); n++ {
			if n == len(starts) && starts[n-1] == len(text) {
				break // no line after a final newline
			}
			match.After = append(match.After, lineText(n))
		}
		switch emit(match) {
		case grepNextFile:
			return nil
		case grepStop:
			return errStop
		}
	}
	return nil
}

// clipLine shortens a line to the limit and makes it a UTF-8 string.
func clipLine(line []byte) string {
	if len(line) > maxMatchLineBytes {
		line = line[:maxMatchLineBytes]
		line = line[:len(line)-textenc.IncompleteTail(line)]
	}
	return string(bytes.ToValidUTF8(line, []byte("�")))
}

// SearchFindInput is the input of search_find.
type SearchFindInput struct {
	Pattern     string   `json:"pattern,omitempty" jsonschema:"glob: matches base name without '/' (*.log, *.{yml,yaml}) or relative path with '/' (**/test/*.py, src/*/main.go); default matches all"`
	Path        string   `json:"path,omitempty" jsonschema:"root directory (default: current directory)"`
	Type        string   `json:"type,omitempty" jsonschema:"file, dir or empty for both"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git, .hg, .svn; replaces default when set)"`
	Gitignore   *bool    `json:"gitignore,omitempty" jsonschema:"respect .gitignore and .git/info/exclude (default true)"`
	Sort        string   `json:"sort,omitempty" jsonschema:"path (default: walk order) or mtime (newest first)"`
	MaxResults  int      `json:"max_results,omitempty" jsonschema:"max paths to return (default 1000)"`
}

// SearchFindOutput is the output of search_find.
type SearchFindOutput struct {
	Paths     []string `json:"paths"`
	Skipped   int      `json:"skipped,omitempty" jsonschema:"unreadable entries skipped"`
	Truncated bool     `json:"truncated,omitempty" jsonschema:"stopped at max_results"`
}

// maxSortCandidates caps the candidates collected when sorting by modification time.
const maxSortCandidates = 100000

func (d *Deps) searchFind(ctx context.Context, _ *mcp.CallToolRequest, in SearchFindInput) (*mcp.CallToolResult, SearchFindOutput, error) {
	pattern, err := compileGlob(in.Pattern)
	if err != nil {
		return nil, SearchFindOutput{}, fmt.Errorf("pattern: %w", err)
	}
	switch in.Type {
	case "", "file", "dir":
	default:
		return nil, SearchFindOutput{}, fmt.Errorf("unknown type %q (use file, dir or omit)", in.Type)
	}
	byMtime := false
	switch in.Sort {
	case "", "path":
	case "mtime":
		byMtime = true
	default:
		return nil, SearchFindOutput{}, fmt.Errorf("unknown sort %q (use path or mtime)", in.Sort)
	}
	max := orDefaultInt(in.MaxResults, 1000)
	root := orDefault(in.Path, ".")

	type found struct {
		path  string
		mtime time.Time
	}
	var all []found
	out := SearchFindOutput{Paths: []string{}}
	skipped, err := walkFiles(ctx, root, in.ExcludeDirs, in.Gitignore == nil || *in.Gitignore, func(path, rel string, de fs.DirEntry) error {
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
		return nil, SearchFindOutput{}, err
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

// FsManifestInput is the input of fs_manifest.
type FsManifestInput struct {
	Path        string   `json:"path,omitempty" jsonschema:"root directory (default: current directory)"`
	Include     string   `json:"include,omitempty" jsonschema:"file glob (same syntax as search_find pattern), e.g. *.c or src/**/*.h"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty" jsonschema:"directory names to skip (default: .git, .hg, .svn; replaces default when set)"`
	MaxFiles    int      `json:"max_files,omitempty" jsonschema:"max files to return (default 2000)"`
}

// ManifestEntry is the path and hash of one file.
type ManifestEntry struct {
	Path   string `json:"path" jsonschema:"path relative to root, with forward slashes"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode" jsonschema:"permission bits in octal, e.g. 0644"`
	SHA256 string `json:"sha256"`
}

// FsManifestOutput is the output of fs_manifest.
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
	skipped, err := walkFiles(ctx, root, in.ExcludeDirs, false, func(path, rel string, de fs.DirEntry) error {
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
		mode := ""
		if info, err := de.Info(); err == nil {
			mode = fmt.Sprintf("%04o", info.Mode().Perm())
		}
		out.Files = append(out.Files, ManifestEntry{Path: rel, Size: size, Mode: mode, SHA256: sum})
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
