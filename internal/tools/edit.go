package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// maxEditBytes is the largest file fs_edit handles.
const maxEditBytes = 32 << 20

// EditOp is one replacement.
type EditOp struct {
	OldString  string `json:"old_string" jsonschema:"exact text to replace, including whitespace and indentation. Must occur exactly once unless replace_all is set"`
	NewString  string `json:"new_string" jsonschema:"replacement text (empty to delete old_string)"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"replace every occurrence instead of requiring a unique match"`
}

// FsEditInput is the input of fs_edit.
type FsEditInput struct {
	Path       string   `json:"path"`
	OldString  string   `json:"old_string,omitempty" jsonschema:"single edit: exact text to replace (copy it from fs_read output, without line number prefixes). Must occur exactly once unless replace_all is set"`
	NewString  string   `json:"new_string,omitempty" jsonschema:"single edit: replacement text (empty to delete old_string)"`
	ReplaceAll bool     `json:"replace_all,omitempty" jsonschema:"single edit: replace every occurrence"`
	Edits      []EditOp `json:"edits,omitempty" jsonschema:"several edits applied in order, each to the result of the previous one. All succeed or the file is left unchanged. Use instead of old_string/new_string"`
	Charset    string   `json:"charset,omitempty" jsonschema:"encoding of the file: auto (default: BOM, then UTF-8 if valid, otherwise the host file code page), utf-8, system, or a charset name. The file is written back in the same encoding and BOM"`
	DryRun     bool     `json:"dry_run,omitempty" jsonschema:"check the edits and return the snippet without writing the file"`
}

// AuditArgs logs the lengths of the replacements instead of their content.
func (in FsEditInput) AuditArgs() any {
	in.OldString, in.NewString = redactedLen(in.OldString), redactedLen(in.NewString)
	edits := make([]EditOp, len(in.Edits))
	for i, e := range in.Edits {
		edits[i] = EditOp{OldString: redactedLen(e.OldString), NewString: redactedLen(e.NewString), ReplaceAll: e.ReplaceAll}
	}
	in.Edits = edits
	return in
}

// FsEditOutput is the output of fs_edit.
type FsEditOutput struct {
	Path         string `json:"path"`
	Replacements int    `json:"replacements" jsonschema:"total number of replaced occurrences"`
	Lines        []int  `json:"lines" jsonschema:"line numbers (in the edited file) where replacements start"`
	Snippet      string `json:"snippet" jsonschema:"edited regions with a few lines of context, prefixed with line numbers (not part of the file)"`
	Charset      string `json:"charset" jsonschema:"encoding the file was read and written in"`
	BOM          bool   `json:"bom,omitempty" jsonschema:"the file has a byte order mark (kept)"`
	Newline      string `json:"newline,omitempty" jsonschema:"line endings of the file: lf, crlf, cr or mixed"`
	Size         int64  `json:"size" jsonschema:"file size after the edit, in bytes"`
	SHA256       string `json:"sha256" jsonschema:"SHA-256 of the file after the edit"`
	DryRun       bool   `json:"dry_run,omitempty" jsonschema:"the file was not written"`
}

func (d *Deps) fsEdit(_ context.Context, _ *mcp.CallToolRequest, in FsEditInput) (*mcp.CallToolResult, FsEditOutput, error) {
	ops := in.Edits
	switch {
	case len(ops) > 0 && (in.OldString != "" || in.NewString != "" || in.ReplaceAll):
		return nil, FsEditOutput{}, errors.New("use either old_string/new_string or edits, not both")
	case len(ops) == 0 && in.OldString == "":
		return nil, FsEditOutput{}, errors.New("old_string is required (to create or overwrite a whole file, use fs_write)")
	case len(ops) == 0:
		ops = []EditOp{{OldString: in.OldString, NewString: in.NewString, ReplaceAll: in.ReplaceAll}}
	}

	tf, err := loadText(in.Path, in.Charset)
	if err != nil {
		return nil, FsEditOutput{}, err
	}
	nl := textenc.Newline(tf.text)
	text := tf.text
	var spans []span
	total := 0
	for i, op := range ops {
		where := ""
		if len(ops) > 1 {
			where = fmt.Sprintf("edits[%d]: ", i)
		}
		if op.OldString == "" {
			return nil, FsEditOutput{}, fmt.Errorf("%sold_string must not be empty", where)
		}
		old, repl, n := matchEdit(text, op, nl)
		switch {
		case old == repl:
			return nil, FsEditOutput{}, fmt.Errorf("%sold_string and new_string are identical", where)
		case n == 0:
			return nil, FsEditOutput{}, fmt.Errorf("%s%s", where, notFoundMessage(in.Path, text, op.OldString))
		case n > 1 && !op.ReplaceAll:
			return nil, FsEditOutput{}, fmt.Errorf("%sold_string occurs %d times in %s (at lines %s). Include more surrounding lines to make it unique, or set replace_all",
				where, n, in.Path, joinInts(occurrenceLines(text, old, 10)))
		}
		text, spans = replaceTracked(text, old, repl, spans)
		total += n
	}

	b, err := tf.enc.Encode(text)
	if err != nil {
		return nil, FsEditOutput{}, fmt.Errorf("new text cannot be written as %s: %w", tf.enc.Name, err)
	}
	data := append(append([]byte{}, tf.bom...), b...)
	if !in.DryRun {
		if err := overwriteFile(in.Path, data); err != nil {
			return nil, FsEditOutput{}, err
		}
	}
	sum := sha256.Sum256(data)
	out := FsEditOutput{
		Path:         absPath(in.Path),
		Replacements: total,
		Lines:        spanLines(text, spans, 50),
		Snippet:      snippet(text, spans, 2, 80),
		Charset:      tf.enc.Name,
		BOM:          len(tf.bom) > 0,
		Newline:      textenc.Newline(text),
		Size:         int64(len(data)),
		SHA256:       hex.EncodeToString(sum[:]),
		DryRun:       in.DryRun,
	}
	return nil, out, nil
}

// textFile is a text file read and converted to UTF-8.
type textFile struct {
	enc  textenc.Encoding
	bom  []byte
	text string
}

// loadText reads the whole file, detects its encoding and converts it to UTF-8.
// It fails when writing back could not reproduce the original bytes (the encoding was detected wrongly).
func loadText(path, charset string) (textFile, error) {
	ch, err := textenc.Parse(charset, textenc.File, true, textenc.UTF8)
	if err != nil {
		return textFile{}, fmt.Errorf("charset: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return textFile{}, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return textFile{}, err
	} else if info.IsDir() {
		return textFile{}, fmt.Errorf("%s is a directory", path)
	} else if info.Size() > maxEditBytes {
		return textFile{}, fmt.Errorf("%s is too large to edit (%d bytes, limit %d); use fs_write with offset", path, info.Size(), maxEditBytes)
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return textFile{}, err
	}

	var tf textFile
	body := raw
	if bomEnc, n, ok := textenc.DetectBOM(raw); ok && (ch.Auto || ch.Enc.Name == bomEnc.Name) {
		tf.enc, tf.bom, body = bomEnc, raw[:n], raw[n:]
	} else if e, ok := textenc.DetectUTF16(raw); ok && ch.Auto {
		tf.enc = e
	} else if ch.Auto && utf8.Valid(raw) {
		tf.enc = textenc.UTF8
	} else {
		tf.enc = ch.Enc
	}
	if tf.enc.IsUTF8() {
		if !utf8.Valid(body) {
			return textFile{}, fmt.Errorf("%s is not valid UTF-8; specify its charset", path)
		}
		tf.text = string(body)
		return tf, nil
	}
	tf.text = tf.enc.Decode(body)
	if back, err := tf.enc.Encode(tf.text); err != nil || string(back) != string(body) {
		return textFile{}, fmt.Errorf("%s cannot be decoded as %s without loss; specify the correct charset", path, tf.enc.Name)
	}
	return tf, nil
}

// overwriteFile replaces the content of an existing file, keeping its owner and permissions.
func overwriteFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// matchEdit returns the strings before and after replacement and the number of occurrences.
// Agents often send LF newlines, so in CRLF files the newlines are made CRLF before matching.
func matchEdit(text string, op EditOp, nl string) (old, repl string, n int) {
	crlf := func(s string) string { s, _ = textenc.ConvertNewline(s, "crlf"); return s }
	plain := func() (string, string, int) {
		return op.OldString, op.NewString, strings.Count(text, op.OldString)
	}
	converted := func() (string, string, int) {
		o := crlf(op.OldString)
		return o, crlf(op.NewString), strings.Count(text, o)
	}
	if nl == "crlf" {
		if old, repl, n = converted(); n > 0 {
			return
		}
		return plain()
	}
	if old, repl, n = plain(); n > 0 || !strings.Contains(text, "\r\n") || !strings.Contains(op.OldString, "\n") {
		return
	}
	if o, r, m := converted(); m > 0 {
		return o, r, m
	}
	return
}

// notFoundMessage explains that old_string was not found, showing the lines of candidates that differ only in whitespace.
func notFoundMessage(path, text, old string) string {
	msg := fmt.Sprintf("old_string not found in %s. It must match the file exactly, including whitespace, indentation and line endings; re-read the file with fs_read (line_numbers: true) and copy the text without the line number prefixes", path)
	if line := fuzzyLine(text, old); line > 0 {
		msg += fmt.Sprintf(". A block that differs only in whitespace starts at line %d", line)
	}
	return msg
}

// fuzzyLine returns the line number where old matches when leading and trailing whitespace of each line is ignored, or 0.
func fuzzyLine(text, old string) int {
	want := trimLines(strings.TrimRight(old, "\r\n"))
	if len(want) == 0 || (len(want) == 1 && want[0] == "") {
		return 0
	}
	lines := trimLines(text)
	for i := 0; i+len(want) <= len(lines); i++ {
		ok := true
		for j, w := range want {
			// The first and last lines may give only part of a line, so they match partially.
			l := lines[i+j]
			switch {
			case len(want) == 1:
				ok = strings.Contains(l, w)
			case j == 0:
				ok = strings.HasSuffix(l, w)
			case j == len(want)-1:
				ok = strings.HasPrefix(l, w)
			default:
				ok = l == w
			}
			if !ok {
				break
			}
		}
		if ok {
			return i + 1
		}
	}
	return 0
}

func trimLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return lines
}

// occurrenceLines returns up to max line numbers where old occurs.
func occurrenceLines(text, old string, max int) []int {
	var lines []int
	pos := 0
	for len(lines) < max {
		i := strings.Index(text[pos:], old)
		if i < 0 {
			break
		}
		pos += i
		lines = append(lines, lineAt(text, pos))
		pos += len(old)
	}
	return lines
}

// span is the range (byte offsets) of a string inserted by a replacement.
type span struct{ start, end int }

// replaceTracked replaces every old with repl and records the replaced ranges.
// Ranges recorded by earlier replacements are shifted to follow the replacement.
func replaceTracked(text, old, repl string, spans []span) (string, []span) {
	var sb strings.Builder
	var added []span
	delta := len(repl) - len(old)
	var cuts []int // replaced offsets (in the coordinates before replacement)
	pos := 0
	for {
		i := strings.Index(text[pos:], old)
		if i < 0 {
			break
		}
		i += pos
		sb.WriteString(text[pos:i])
		start := sb.Len()
		sb.WriteString(repl)
		added = append(added, span{start, sb.Len()})
		cuts = append(cuts, i)
		pos = i + len(old)
	}
	sb.WriteString(text[pos:])

	shift := func(p int) int {
		n := 0
		for _, c := range cuts {
			if c+len(old) <= p {
				n++
			} else if c < p {
				// A range inside the replaced text moves to the start of the new range.
				return c + n*delta
			}
		}
		return p + n*delta
	}
	moved := make([]span, 0, len(spans)+len(added))
	for _, s := range spans {
		moved = append(moved, span{shift(s.start), shift(s.end)})
	}
	return sb.String(), append(moved, added...)
}

// lineAt returns the number (from 1) of the line containing byte offset pos.
func lineAt(text string, pos int) int {
	return strings.Count(text[:min(pos, len(text))], "\n") + 1
}

func spanLines(text string, spans []span, max int) []int {
	lines := []int{}
	for _, s := range spans {
		if len(lines) >= max {
			break
		}
		lines = append(lines, lineAt(text, s.start))
	}
	return lines
}

// snippet returns the replaced range with ctx lines around it, numbered, at most maxLines lines in all.
func snippet(text string, spans []span, ctx, maxLines int) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	show := make([]bool, len(lines))
	for _, s := range spans {
		first := lineAt(text, s.start) - 1
		last := lineAt(text, max(s.end-1, s.start)) - 1
		for i := max(first-ctx, 0); i <= min(last+ctx, len(lines)-1); i++ {
			show[i] = true
		}
	}
	var sb strings.Builder
	shown, prev := 0, -2
	for i, ok := range show {
		if !ok {
			continue
		}
		if shown >= maxLines {
			sb.WriteString("...\n")
			break
		}
		if prev >= 0 && i != prev+1 {
			sb.WriteString("...\n")
		}
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, strings.TrimSuffix(lines[i], "\r"))
		shown, prev = shown+1, i
	}
	return sb.String()
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}
