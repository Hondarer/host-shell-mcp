package tools

import (
	"bufio"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/transform"

	"github.com/hondarer/host-shell-mcp/internal/limit"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
	"github.com/hondarer/host-shell-mcp/internal/wire"
)

// registerFiles defines the file tools.
func registerFiles(r *Registry, d *Deps) {
	// File operations
	add(r, groupFiles, &mcp.Tool{
		Name:        "fs_list",
		Title:       "List directory",
		Description: "List entries in a single directory (name, type, size, mode, mod_time). Non-recursive; use search_find to search directory trees.",
		Annotations: readOnly(),
	}, d.fsList)
	add(r, groupFiles, &mcp.Tool{
		Name:        "fs_stat",
		Title:       "File attributes",
		Description: "Get type, size, mode, mod_time and symlink target of a path without following a final symlink.",
		Annotations: readOnly(),
	}, d.fsStat)
	add(r, groupFiles, &mcp.Tool{
		Name:  "fs_read",
		Title: "Read file",
		Description: "Read a file on the remote host. Line mode: line_start (1-based), line_count and optional line_numbers (continue with next_line). " +
			"Byte mode: offset and length (continue with next_offset). Binary mode: encoding base64 with offset and length. " +
			"Text mode decodes to UTF-8 (charset auto by default) and reports charset, bom and newline.",
		Annotations: readOnly(),
	}, d.fsRead)
	add(r, groupFiles, &mcp.Tool{
		Name:  "fs_edit",
		Title: "Edit file",
		Description: "Replace exact text in an existing file (prefer over fs_write for partial edits). old_string must match once unless replace_all is set; edits applies multiple replacements atomically. " +
			"Preserves file encoding, BOM and line endings (LF in inputs matches CRLF files) and returns a numbered diff snippet. " +
			"Copy old_string from fs_read without line number prefixes.",
		Annotations: destructive(),
	}, d.fsEdit)
	add(r, groupFiles, &mcp.Tool{
		Name:  "fs_write",
		Title: "Write file",
		Description: "Create, overwrite, or update a file with text or base64 content. mode: overwrite (default), append, create. " +
			"offset writes at a byte position without truncating; make_dirs creates parent directories; perm sets octal permissions; atomic renames a temp file over the target. " +
			"charset, newline and bom control text encoding (default UTF-8). Use fs_edit for partial edits to existing files.",
		Annotations: destructive(),
	}, d.fsWrite)
	add(r, groupFiles, &mcp.Tool{
		Name:        "fs_mkdir",
		Title:       "Create directory",
		Description: "Create a directory and any missing parent directories (no-op if the directory already exists).",
		Annotations: additive(true),
	}, d.fsMkdir)
	add(r, groupFiles, &mcp.Tool{
		Name:  "fs_move",
		Title: "Move or rename",
		Description: "Move or rename a file or directory to destination (full target path). Fails if destination exists unless overwrite is set (files only). " +
			"Falls back to copy-and-delete across file systems.",
		Annotations: destructive(),
	}, d.fsMove)
	add(r, groupFiles, &mcp.Tool{
		Name:  "fs_copy",
		Title: "Copy",
		Description: "Copy a file or directory tree (with recursive) to destination (full target path), preserving permissions, mtimes and symlinks. " +
			"Fails if destination exists unless overwrite is set (replaces files, merges directories).",
		Annotations: destructive(),
	}, d.fsCopy)
	add(r, groupFiles, &mcp.Tool{
		Name:        "fs_remove",
		Title:       "Delete",
		Description: "Delete a file, symlink, empty directory, or directory tree (with recursive). Refuses filesystem roots and home directory; missing_ok ignores nonexistent paths.",
		Annotations: destructive(),
	}, d.fsRemove)
	add(r, groupFiles, &mcp.Tool{
		Name:        "fs_manifest",
		Title:       "File hashes",
		Description: "List regular files under a path with relative path, size, octal mode and SHA-256 hash (for incremental sync or verification).",
		Annotations: readOnly(),
	}, d.fsManifest)
}

// Entry is one directory entry.
type Entry struct {
	Name    string `json:"name"`
	Type    string `json:"type" jsonschema:"file, dir, symlink or other"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"mod_time"`
}

func newEntry(name string, info fs.FileInfo) Entry {
	return Entry{
		Name:    name,
		Type:    fileType(info.Mode()),
		Size:    info.Size(),
		Mode:    info.Mode().String(),
		ModTime: info.ModTime().Format(time.RFC3339),
	}
}

func fileType(m fs.FileMode) string {
	switch {
	case m.IsRegular():
		return "file"
	case m.IsDir():
		return "dir"
	case m&fs.ModeSymlink != 0:
		return "symlink"
	default:
		return "other"
	}
}

// FsListInput is the input of fs_list.
type FsListInput struct {
	Path       string `json:"path,omitempty" jsonschema:"directory path (default: current directory)"`
	MaxEntries int    `json:"max_entries,omitempty" jsonschema:"maximum entries to return (default 1000)"`
}

// FsListOutput is the output of fs_list.
type FsListOutput struct {
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated,omitempty"`
}

func (d *Deps) fsList(_ context.Context, _ *mcp.CallToolRequest, in FsListInput) (*mcp.CallToolResult, FsListOutput, error) {
	path := cmp.Or(in.Path, ".")
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, FsListOutput{}, err
	}
	maxEntries := orDefaultInt(in.MaxEntries, 1000)
	out := FsListOutput{Path: absPath(path), Entries: []Entry{}}
	for _, e := range ents {
		if len(out.Entries) >= maxEntries {
			out.Truncated = true
			break
		}
		info, err := e.Info()
		if err != nil {
			continue // Ignore entries removed after listing.
		}
		out.Entries = append(out.Entries, newEntry(e.Name(), info))
	}
	return nil, out, nil
}

// FsStatInput is the input of fs_stat.
type FsStatInput struct {
	Path string `json:"path"`
}

// FsStatOutput is the output of fs_stat.
type FsStatOutput struct {
	Path           string  `json:"path"`
	Entry          Entry   `json:"entry"`
	LinkTarget     string  `json:"link_target,omitempty"`
	AvailableBytes *uint64 `json:"available_bytes,omitempty" jsonschema:"free space available to this user on the file system containing the path (check before placing large files)"`
}

func (d *Deps) fsStat(_ context.Context, _ *mcp.CallToolRequest, in FsStatInput) (*mcp.CallToolResult, FsStatOutput, error) {
	info, err := os.Lstat(in.Path)
	if err != nil {
		return nil, FsStatOutput{}, err
	}
	out := FsStatOutput{Path: absPath(in.Path), Entry: newEntry(info.Name(), info)}
	if info.Mode()&fs.ModeSymlink != 0 {
		out.LinkTarget, _ = os.Readlink(in.Path)
	}
	if n, ok := availableBytes(in.Path); ok {
		out.AvailableBytes = &n
	}
	return nil, out, nil
}

// FsReadInput is the input of fs_read.
type FsReadInput struct {
	Path        string `json:"path"`
	Encoding    string `json:"encoding,omitempty" jsonschema:"text (default) or base64"`
	Charset     string `json:"charset,omitempty" jsonschema:"text mode: auto (default: BOM, then UTF-8 if valid, else host file code page), utf-8, system, or a charset name (shift_jis, euc-jp, utf-16le, ...)"`
	LineStart   int    `json:"line_start,omitempty" jsonschema:"text mode: 1-based first line to read (ignores offset/length when set)"`
	LineCount   int    `json:"line_count,omitempty" jsonschema:"text mode: max lines to read (default 2000)"`
	LineNumbers bool   `json:"line_numbers,omitempty" jsonschema:"line mode: prefix lines with 'N\\t' (requires line_start; strip prefixes before passing to fs_edit)"`
	Offset      int64  `json:"offset,omitempty" jsonschema:"byte offset to start reading"`
	Length      int    `json:"length,omitempty" jsonschema:"max bytes to read (capped by server output limit)"`
}

// FsReadOutput is the output of fs_read.
type FsReadOutput struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Encoding   string `json:"encoding"`
	Charset    string `json:"charset,omitempty" jsonschema:"text mode: detected or selected charset (pass to fs_write to preserve encoding)"`
	BOM        bool   `json:"bom,omitempty" jsonschema:"text mode: file has a byte order mark (stripped from content)"`
	Newline    string `json:"newline,omitempty" jsonschema:"text mode: line endings in content (lf, crlf, cr or mixed)"`
	Content    string `json:"content"`
	EOF        bool   `json:"eof"`
	NextOffset int64  `json:"next_offset,omitempty" jsonschema:"byte mode: next byte offset"`
	NextLine   int    `json:"next_line,omitempty" jsonschema:"line mode: next 1-based line number"`
	Truncated  bool   `json:"truncated,omitempty" jsonschema:"stopped at output limit before completing the requested range"`
}

func (d *Deps) fsRead(_ context.Context, req *mcp.CallToolRequest, in FsReadInput) (*mcp.CallToolResult, FsReadOutput, error) {
	f, err := os.Open(in.Path)
	if err != nil {
		return nil, FsReadOutput{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, FsReadOutput{}, err
	}
	if info.IsDir() {
		return nil, FsReadOutput{}, fmt.Errorf("%s is a directory", in.Path)
	}
	out := FsReadOutput{Path: absPath(in.Path), Size: info.Size(), Encoding: cmp.Or(in.Encoding, "text")}

	switch out.Encoding {
	case "text":
		err = d.readText(f, in, &out)
	case "base64":
		if in.Charset != "" {
			return nil, FsReadOutput{}, errors.New("charset applies to encoding text only")
		}
		limit := d.base64Budget()
		if fromBridge(req) {
			// Bridge transfers do not pass through the agent's context, so read in large chunks.
			limit = wire.TransferChunkBytes
		}
		err = d.readBase64(f, in, limit, &out)
	default:
		err = fmt.Errorf("unknown encoding %q", in.Encoding)
	}
	if err != nil {
		return nil, FsReadOutput{}, err
	}
	return nil, out, nil
}

// readText reads a file, converting it to UTF-8.
func (d *Deps) readText(f *os.File, in FsReadInput, out *FsReadOutput) error {
	ch, err := readCharset(in.Charset)
	if err != nil {
		return err
	}
	// Check the start of the file for a BOM, or for UTF-16 without one, regardless of the range read.
	head := make([]byte, 4096)
	k, _ := f.ReadAt(head, 0)
	ch, skip, _ := ch.Detect(head[:k])
	out.BOM = skip > 0
	if in.LineNumbers && in.LineStart <= 0 {
		return errors.New("line_numbers requires line_start (use line_start: 1 to read from the beginning)")
	}
	if in.LineStart > 0 {
		err = d.readLines(f, int64(skip), ch, in, out)
	} else {
		err = d.readTextBytes(f, int64(skip), ch, in, out)
	}
	if err == nil {
		out.Newline = textenc.Newline(out.Content)
	}
	return err
}

func (d *Deps) readLines(f *os.File, skip int64, ch textenc.Choice, in FsReadInput, out *FsReadOutput) error {
	src := io.NewSectionReader(f, skip, out.Size-skip)
	var r *bufio.Reader
	converted := false
	decode := func(b []byte) string { return string(b) }
	if ch.Auto {
		// Per line: pass through when valid UTF-8, otherwise treat it as the target's default encoding.
		r = bufio.NewReader(src)
		decode = func(b []byte) string {
			s, decoded := ch.Enc.DecodeUnlessUTF8(b)
			converted = converted || decoded
			return s
		}
	} else {
		r = bufio.NewReader(transform.NewReader(src, ch.Enc.NewDecoder()))
	}

	count := orDefaultInt(in.LineCount, 2000)
	var sb strings.Builder
	line := 1
	for ; line < in.LineStart+count; line++ {
		b, err := r.ReadBytes('\n')
		if len(b) == 0 && errors.Is(err, io.EOF) {
			out.EOF = true // Text after the last newline does not count as a line.
			break
		}
		if line >= in.LineStart {
			s := decode(b)
			if in.LineNumbers {
				s = fmt.Sprintf("%6d\t%s", line, s)
			}
			if sb.Len()+len(s) > d.Cfg.MaxOutputBytes {
				out.Truncated = true
				if sb.Len() == 0 {
					// When a single line exceeds the limit, return it truncated and move on to the next line.
					s, _ = limit.Text(s, d.Cfg.MaxOutputBytes)
					sb.WriteString(s)
					line++
				}
				break
			}
			sb.WriteString(s)
		}
		if errors.Is(err, io.EOF) {
			out.EOF = true
			break
		}
		if err != nil {
			return err
		}
	}
	out.Content = strings.ToValidUTF8(sb.String(), "�")
	if !out.EOF {
		out.NextLine = line
	}
	out.Charset = ch.Enc.Name
	if ch.Auto && !converted {
		out.Charset = textenc.UTF8.Name
	}
	return nil
}

func (d *Deps) readTextBytes(f *os.File, skip int64, ch textenc.Choice, in FsReadInput, out *FsReadOutput) error {
	start := in.Offset
	if start == 0 {
		start = skip
	}
	n := d.Cfg.MaxOutputBytes
	if in.Length > 0 && in.Length < n {
		n = in.Length
	} else if in.Length > n {
		out.Truncated = true
	}
	buf := make([]byte, n)
	k, err := f.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	buf = buf[:k]
	atEOF := start+int64(k) >= out.Size

	enc := ch.Enc
	if ch.Auto && textenc.LooksUTF8(buf) {
		enc = textenc.UTF8
	}
	// Fit the converted size within the limit and leave a character split at the end of the range for the next read.
	s, consumed, full := enc.DecodePrefix(buf, d.Cfg.MaxOutputBytes, atEOF)
	if consumed == 0 && k > 0 {
		s, consumed, full = enc.DecodePrefix(buf, d.Cfg.MaxOutputBytes, true)
	}
	out.Truncated = out.Truncated || full
	out.Content = s
	out.Charset = enc.Name
	out.NextOffset = start + int64(consumed)
	out.EOF = out.NextOffset >= out.Size
	return nil
}

func (d *Deps) readBase64(f *os.File, in FsReadInput, n int, out *FsReadOutput) error {
	if in.Length > 0 && in.Length < n {
		n = in.Length
	} else if in.Length > n {
		out.Truncated = true
	}
	buf := make([]byte, n)
	k, err := f.ReadAt(buf, in.Offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	out.Content = base64.StdEncoding.EncodeToString(buf[:k])
	out.NextOffset = in.Offset + int64(k)
	out.EOF = out.NextOffset >= out.Size
	return nil
}

// fromBridge reports whether the request comes from host-shell bridge.
func fromBridge(req *mcp.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	p := req.Session.InitializeParams()
	return p != nil && p.ClientInfo != nil && p.ClientInfo.Name == wire.BridgeClientName
}

// FsWriteInput is the input of fs_write.
type FsWriteInput struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty" jsonschema:"text (default) or base64"`
	Mode     string `json:"mode,omitempty" jsonschema:"overwrite (default), append or create (fail if exists)"`
	Offset   *int64 `json:"offset,omitempty" jsonschema:"write at byte offset without truncating (creates file if missing; incompatible with append and atomic)"`
	MakeDirs bool   `json:"make_dirs,omitempty" jsonschema:"create missing parent directories"`
	Perm     string `json:"perm,omitempty" jsonschema:"octal permissions for a new file, e.g. 0644 (ignored on Windows)"`
	Charset  string `json:"charset,omitempty" jsonschema:"text: output encoding: utf-8 (default), system (host file code page), or a charset name (shift_jis, euc-jp, utf-16le, ...); unrepresentable characters fail"`
	Newline  string `json:"newline,omitempty" jsonschema:"text: line ending conversion: keep (default), lf or crlf"`
	Atomic   bool   `json:"atomic,omitempty" jsonschema:"overwrite mode (without offset): write a temp file and rename over target so readers never see partial content; preserves permissions, owner, and symlink target, but not ACLs, xattrs, or hard links"`
	BOM      bool   `json:"bom,omitempty" jsonschema:"text: prepend a byte order mark (utf-8, utf-16le, utf-16be; only at start of file)"`
}

// AuditArgs logs content length and SHA-256 instead of raw content.
func (in FsWriteInput) AuditArgs() any {
	sum := sha256.Sum256([]byte(in.Content))
	in.Content = fmt.Sprintf("<%d chars, sha256 %s>", len(in.Content), hex.EncodeToString(sum[:]))
	return in
}

// data encodes Content for writing, applying newline conversion, charset encoding, and optional BOM in text mode.
func (in FsWriteInput) data() ([]byte, error) {
	switch cmp.Or(in.Encoding, "text") {
	case "text":
	case "base64":
		if in.Charset != "" || in.Newline != "" || in.BOM {
			return nil, errors.New("charset, newline and bom apply to encoding text only")
		}
		b, err := base64.StdEncoding.DecodeString(in.Content)
		if err != nil {
			return nil, fmt.Errorf("decode base64: %w", err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown encoding %q", in.Encoding)
	}

	s, err := textenc.ConvertNewline(in.Content, in.Newline)
	if err != nil {
		return nil, err
	}
	ch, err := writeCharset(in.Charset)
	if err != nil {
		return nil, err
	}
	b, err := ch.Enc.Encode(s)
	if err != nil {
		return nil, err
	}
	if in.BOM {
		bom := ch.Enc.BOM()
		switch {
		case bom == nil:
			return nil, fmt.Errorf("bom is not supported for charset %s", ch.Enc.Name)
		case in.Mode == "append" || (in.Offset != nil && *in.Offset > 0):
			return nil, errors.New("bom can be written only at the start of the file")
		}
		b = append(bom, b...)
	}
	return b, nil
}

// FsWriteOutput is the output of fs_write.
type FsWriteOutput struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
	Size         int64  `json:"size" jsonschema:"file size after the write"`
}

func (d *Deps) fsWrite(_ context.Context, _ *mcp.CallToolRequest, in FsWriteInput) (*mcp.CallToolResult, FsWriteOutput, error) {
	flag := os.O_WRONLY | os.O_CREATE
	switch cmp.Or(in.Mode, "overwrite") {
	case "overwrite":
		flag |= os.O_TRUNC
	case "append":
		flag |= os.O_APPEND
	case "create":
		flag |= os.O_EXCL
	default:
		return nil, FsWriteOutput{}, fmt.Errorf("unknown mode %q", in.Mode)
	}
	if in.Offset != nil {
		// A write at an offset replaces only that range without truncating the existing content.
		switch {
		case *in.Offset < 0:
			return nil, FsWriteOutput{}, fmt.Errorf("offset must not be negative: %d", *in.Offset)
		case flag&os.O_APPEND != 0:
			return nil, FsWriteOutput{}, fmt.Errorf("offset cannot be combined with mode append")
		}
		flag &^= os.O_TRUNC
	}
	data, err := in.data()
	if err != nil {
		return nil, FsWriteOutput{}, err
	}

	perm := fs.FileMode(0o644)
	if in.Perm != "" {
		p, err := strconv.ParseUint(in.Perm, 8, 32)
		if err != nil {
			return nil, FsWriteOutput{}, fmt.Errorf("invalid perm %q", in.Perm)
		}
		perm = fs.FileMode(p)
	}
	if in.MakeDirs {
		if err := os.MkdirAll(filepath.Dir(in.Path), 0o755); err != nil {
			return nil, FsWriteOutput{}, err
		}
	}

	if in.Atomic {
		if in.Mode != "" && in.Mode != "overwrite" || in.Offset != nil {
			return nil, FsWriteOutput{}, errors.New("atomic works only with mode overwrite and without offset")
		}
		if err := replaceAtomically(in.Path, data, perm); err != nil {
			return nil, FsWriteOutput{}, err
		}
		return nil, FsWriteOutput{Path: absPath(in.Path), BytesWritten: len(data), Size: int64(len(data))}, nil
	}
	f, err := os.OpenFile(in.Path, flag, perm)
	if err != nil {
		return nil, FsWriteOutput{}, err
	}
	var n int
	if in.Offset != nil {
		n, err = f.WriteAt(data, *in.Offset)
	} else {
		n, err = f.Write(data)
	}
	var size int64
	if err == nil {
		var info fs.FileInfo
		if info, err = f.Stat(); err == nil {
			size = info.Size()
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, FsWriteOutput{}, err
	}
	return nil, FsWriteOutput{Path: absPath(in.Path), BytesWritten: n, Size: size}, nil
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
