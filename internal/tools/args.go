package tools

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/hondarer/host-shell-mcp/internal/bytesize"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// Shared helpers for interpreting tool arguments.

func orDefaultInt(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// clampMax returns requested when it is positive and below limit, otherwise limit.
func clampMax(limit, requested int) int {
	if requested > 0 && requested < limit {
		return requested
	}
	return limit
}

// base64Budget is the raw byte count whose base64 form stays within the output limit.
func (d *Deps) base64Budget() int { return d.Cfg.MaxOutputBytes / 4 * 3 }

func redactedLen(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprintf("<%d chars>", len(s))
}

// readCharset resolves the charset argument of a file read, which defaults to detection.
func readCharset(name string) (textenc.Choice, error) {
	return charsetArg(textenc.ParseAuto(name, textenc.File))
}

// writeCharset resolves the charset argument of a file write, which defaults to UTF-8.
func writeCharset(name string) (textenc.Choice, error) {
	return charsetArg(textenc.ParseFixed(name, textenc.File, textenc.UTF8))
}

func charsetArg(ch textenc.Choice, err error) (textenc.Choice, error) {
	if err != nil {
		return textenc.Choice{}, fmt.Errorf("charset: %w", err)
	}
	return ch, nil
}

// decodeData decodes the argument field according to enc ("text", the default, or "base64").
// Errors name the field and its companion field_encoding argument.
func decodeData(field, enc, s string) ([]byte, error) {
	switch enc {
	case "", "text":
		return []byte(s), nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("unknown %s_encoding %q", field, enc)
}

// compileUntil compiles an optional until pattern.
func compileUntil(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("until: %w", err)
	}
	return re, nil
}

// checkSpace fails when size bytes would not fit on the file system that is to hold path (a
// file that may not exist yet); an unknown size or free space passes.
func checkSpace(path string, size int64) error {
	if size <= 0 {
		return nil
	}
	dir := filepath.Dir(absPath(path))
	avail, ok := availableBytes(dir)
	if !ok || uint64(size) <= avail {
		return nil
	}
	return fmt.Errorf("not enough space for %s: %s needed, %s available on its file system", path, bytesize.Format(uint64(size)), bytesize.Format(avail))
}
