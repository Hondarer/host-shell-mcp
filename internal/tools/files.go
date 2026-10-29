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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/limit"
)

// Entry はディレクトリの 1 項目です。
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

// FsListInput は fs_list の入力です。
type FsListInput struct {
	Path       string `json:"path,omitempty" jsonschema:"directory path (default: current directory)"`
	MaxEntries int    `json:"max_entries,omitempty" jsonschema:"maximum entries to return (default 1000)"`
}

// FsListOutput は fs_list の出力です。
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
			continue // 列挙後に削除された項目は無視する。
		}
		out.Entries = append(out.Entries, newEntry(e.Name(), info))
	}
	return nil, out, nil
}

// FsStatInput は fs_stat の入力です。
type FsStatInput struct {
	Path string `json:"path"`
}

// FsStatOutput は fs_stat の出力です。
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

// FsReadInput は fs_read の入力です。
type FsReadInput struct {
	Path      string `json:"path"`
	Encoding  string `json:"encoding,omitempty" jsonschema:"text (default) or base64"`
	LineStart int    `json:"line_start,omitempty" jsonschema:"text mode: first line to read (1-based). When set, offset/length are ignored"`
	LineCount int    `json:"line_count,omitempty" jsonschema:"text mode: number of lines to read (default 2000)"`
	Offset    int64  `json:"offset,omitempty" jsonschema:"byte offset to start reading"`
	Length    int    `json:"length,omitempty" jsonschema:"maximum bytes to read (capped by the server output limit)"`
}

// FsReadOutput は fs_read の出力です。
type FsReadOutput struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Encoding   string `json:"encoding"`
	Content    string `json:"content"`
	EOF        bool   `json:"eof"`
	NextOffset int64  `json:"next_offset,omitempty" jsonschema:"byte mode: offset to continue reading"`
	NextLine   int    `json:"next_line,omitempty" jsonschema:"line mode: line number to continue reading"`
	Truncated  bool   `json:"truncated,omitempty" jsonschema:"output limit was reached before the requested range"`
}

func (d *Deps) fsRead(_ context.Context, _ *mcp.CallToolRequest, in FsReadInput) (*mcp.CallToolResult, FsReadOutput, error) {
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

	switch {
	case out.Encoding == "text" && in.LineStart > 0:
		err = d.readLines(f, in, &out)
	case out.Encoding == "text" || out.Encoding == "base64":
		err = d.readBytes(f, in, &out)
	default:
		err = fmt.Errorf("unknown encoding %q", in.Encoding)
	}
	if err != nil {
		return nil, FsReadOutput{}, err
	}
	return nil, out, nil
}

func (d *Deps) readLines(f *os.File, in FsReadInput, out *FsReadOutput) error {
	count := orDefaultInt(in.LineCount, 2000)
	r := bufio.NewReader(f)
	var sb strings.Builder
	line := 1
	for ; line < in.LineStart+count; line++ {
		s, err := r.ReadString('\n')
		if line >= in.LineStart {
			if sb.Len()+len(s) > d.Cfg.MaxOutputBytes {
				out.Truncated = true
				if sb.Len() == 0 {
					// 1 行だけで上限を超える場合は、その行を切り詰めて返し、次の行へ進める。
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
	return nil
}

func (d *Deps) readBytes(f *os.File, in FsReadInput, out *FsReadOutput) error {
	n := d.Cfg.MaxOutputBytes
	if out.Encoding == "base64" {
		n = n / 4 * 3 // base64 で膨らんだ後に上限に収める。
	}
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
	buf = buf[:k]
	out.NextOffset = in.Offset + int64(k)
	out.EOF = out.NextOffset >= out.Size
	if out.Encoding == "base64" {
		out.Content = base64.StdEncoding.EncodeToString(buf)
	} else {
		out.Content = strings.ToValidUTF8(string(buf), "�")
	}
	return nil
}

// FsWriteInput は fs_write の入力です。
type FsWriteInput struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty" jsonschema:"text (default) or base64"`
	Mode     string `json:"mode,omitempty" jsonschema:"overwrite (default), append or create (fail if the file exists)"`
	MakeDirs bool   `json:"make_dirs,omitempty" jsonschema:"create parent directories if missing"`
	Perm     string `json:"perm,omitempty" jsonschema:"octal permission for a new file, e.g. 0644 (ignored on Windows)"`
}

// AuditArgs は書き込み内容の代わりに長さとハッシュをログに残します。
func (in FsWriteInput) AuditArgs() any {
	sum := sha256.Sum256([]byte(in.Content))
	in.Content = fmt.Sprintf("<%d chars, sha256 %s>", len(in.Content), hex.EncodeToString(sum[:]))
	return in
}

// FsWriteOutput は fs_write の出力です。
type FsWriteOutput struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
}

func (d *Deps) fsWrite(_ context.Context, _ *mcp.CallToolRequest, in FsWriteInput) (*mcp.CallToolResult, FsWriteOutput, error) {
	data := []byte(in.Content)
	switch orDefault(in.Encoding, "text") {
	case "text":
	case "base64":
		var err error
		if data, err = base64.StdEncoding.DecodeString(in.Content); err != nil {
			return nil, FsWriteOutput{}, fmt.Errorf("decode base64: %w", err)
		}
	default:
		return nil, FsWriteOutput{}, fmt.Errorf("unknown encoding %q", in.Encoding)
	}

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

	// TODO: 上書き時に一時ファイルと rename で原子的に置き換えるか検討する (所有者や属性の維持と相反する)。
	f, err := os.OpenFile(in.Path, flag, perm)
	if err != nil {
		return nil, FsWriteOutput{}, err
	}
	n, err := f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, FsWriteOutput{}, err
	}
	return nil, FsWriteOutput{Path: absPath(in.Path), BytesWritten: n}, nil
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
