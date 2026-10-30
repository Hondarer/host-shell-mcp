package tools

import (
	"bufio"
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

	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/transform"

	"github.com/hondarer/host-shell-mcp/internal/limit"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

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
	path := orDefault(in.Path, ".")
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, FsListOutput{}, err
	}
	max := orDefaultInt(in.MaxEntries, 1000)
	out := FsListOutput{Path: absPath(path), Entries: []Entry{}}
	for _, e := range ents {
		if len(out.Entries) >= max {
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
	Path       string `json:"path"`
	Entry      Entry  `json:"entry"`
	LinkTarget string `json:"link_target,omitempty"`
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
	return nil, out, nil
}

// FsReadInput is the input of fs_read.
type FsReadInput struct {
	Path        string `json:"path"`
	Encoding    string `json:"encoding,omitempty" jsonschema:"text (default) or base64"`
	Charset     string `json:"charset,omitempty" jsonschema:"text mode: encoding of the file: auto (default: BOM, then UTF-8 if valid, otherwise the host file code page), utf-8, system, or a charset name such as shift_jis, euc-jp, utf-16le"`
	LineStart   int    `json:"line_start,omitempty" jsonschema:"text mode: first line to read (1-based). When set, offset/length are ignored"`
	LineCount   int    `json:"line_count,omitempty" jsonschema:"text mode: number of lines to read (default 2000)"`
	LineNumbers bool   `json:"line_numbers,omitempty" jsonschema:"line mode: prefix each line with its number and a tab, like cat -n. Useful before fs_edit; do not copy the prefixes into old_string"`
	Offset      int64  `json:"offset,omitempty" jsonschema:"byte offset to start reading"`
	Length      int    `json:"length,omitempty" jsonschema:"maximum bytes to read (capped by the server output limit)"`
}

// FsReadOutput is the output of fs_read.
type FsReadOutput struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Encoding   string `json:"encoding"`
	Charset    string `json:"charset,omitempty" jsonschema:"text mode: encoding used to decode the content. Pass it to fs_write to keep the file encoding"`
	BOM        bool   `json:"bom,omitempty" jsonschema:"text mode: the file starts with a byte order mark (not included in content)"`
	Newline    string `json:"newline,omitempty" jsonschema:"text mode: line endings found in content: lf, crlf, cr or mixed"`
	Content    string `json:"content"`
	EOF        bool   `json:"eof"`
	NextOffset int64  `json:"next_offset,omitempty" jsonschema:"byte mode: offset to continue reading"`
	NextLine   int    `json:"next_line,omitempty" jsonschema:"line mode: line number to continue reading"`
	Truncated  bool   `json:"truncated,omitempty" jsonschema:"output limit was reached before the requested range"`
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
	out := FsReadOutput{Path: absPath(in.Path), Size: info.Size(), Encoding: orDefault(in.Encoding, "text")}

	switch out.Encoding {
	case "text":
		err = d.readText(f, in, &out)
	case "base64":
		if in.Charset != "" {
			return nil, FsReadOutput{}, errors.New("charset applies to encoding text only")
		}
		limit := d.Cfg.MaxOutputBytes / 4 * 3 // stay within the limit after base64 expansion
		if fromBridge(req) {
			// Bridge transfers do not pass through the agent's context, so read in large chunks.
			limit = TransferChunkBytes
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
	ch, err := textenc.Parse(in.Charset, textenc.File, true, textenc.UTF8)
	if err != nil {
		return fmt.Errorf("charset: %w", err)
	}
	// Check the start of the file for a BOM, or for UTF-16 without one, regardless of the range read.
	head := make([]byte, 4096)
	k, _ := f.ReadAt(head, 0)
	skip := 0
	if bomEnc, n, ok := textenc.DetectBOM(head[:k]); ok && (ch.Auto || ch.Enc.Name == bomEnc.Name) {
		ch = textenc.Choice{Enc: bomEnc}
		skip = n
		out.BOM = true
	} else if e, ok := textenc.DetectUTF16(head[:k]); ok && ch.Auto {
		ch = textenc.Choice{Enc: e}
	}
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
			if utf8.Valid(b) {
				return string(b)
			}
			converted = true
			return ch.Enc.Decode(b)
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

// BridgeClientName is the client name host-shell bridge connects with.
const BridgeClientName = "host-shell-bridge"

// TransferChunkBytes is the size of each read or write when the bridge transfers files.
const TransferChunkBytes = 1 << 20

// fromBridge reports whether the request comes from host-shell bridge.
func fromBridge(req *mcp.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	p := req.Session.InitializeParams()
	return p != nil && p.ClientInfo != nil && p.ClientInfo.Name == BridgeClientName
}

// FsWriteInput is the input of fs_write.
type FsWriteInput struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty" jsonschema:"text (default) or base64"`
	Mode     string `json:"mode,omitempty" jsonschema:"overwrite (default), append or create (fail if the file exists)"`
	Offset   *int64 `json:"offset,omitempty" jsonschema:"write at this byte offset without truncating the file (the file is created if missing). Not allowed with mode append"`
	MakeDirs bool   `json:"make_dirs,omitempty" jsonschema:"create parent directories if missing"`
	Perm     string `json:"perm,omitempty" jsonschema:"octal permission for a new file, e.g. 0644 (ignored on Windows)"`
	Charset  string `json:"charset,omitempty" jsonschema:"text: encoding to write: utf-8 (default), system (host file code page), or a charset name such as shift_jis, euc-jp, utf-16le. Characters that cannot be represented cause an error"`
	Newline  string `json:"newline,omitempty" jsonschema:"text: convert line endings before writing: keep (default), lf or crlf"`
	Atomic   bool   `json:"atomic,omitempty" jsonschema:"overwrite mode only: write a temporary file beside the target and rename it over the target, so readers never see a partial file (for config files read by running services). Keeps permissions and owner (fails if the owner cannot be kept), follows symbolic links, but not ACLs, extended attributes or hard links"`
	BOM      bool   `json:"bom,omitempty" jsonschema:"text: prepend a byte order mark (utf-8, utf-16le, utf-16be only). Only when writing at the start of the file"`
}

// AuditArgs logs the length and hash instead of the written content.
func (in FsWriteInput) AuditArgs() any {
	sum := sha256.Sum256([]byte(in.Content))
	in.Content = fmt.Sprintf("<%d chars, sha256 %s>", len(in.Content), hex.EncodeToString(sum[:]))
	return in
}

// data builds the bytes to write. Text gets its newlines and encoding converted, and a BOM if requested.
func (in FsWriteInput) data() ([]byte, error) {
	switch orDefault(in.Encoding, "text") {
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
	ch, err := textenc.Parse(in.Charset, textenc.File, false, textenc.UTF8)
	if err != nil {
		return nil, fmt.Errorf("charset: %w", err)
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
	switch orDefault(in.Mode, "overwrite") {
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

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orDefaultInt(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

func redactedLen(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprintf("<%d chars>", len(s))
}
