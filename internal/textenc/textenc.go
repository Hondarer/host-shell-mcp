// Package textenc converts between the target's encodings and UTF-8.
//
// MCP messages are always UTF-8. When command output or files on the target are not UTF-8,
// this package converts them to UTF-8 (reading) or from UTF-8 (writing).
package textenc

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Encoding is an encoding together with its canonical name.
type Encoding struct {
	Name string
	enc  encoding.Encoding
}

var (
	// UTF8 is UTF-8. It converts nothing.
	UTF8    = Encoding{Name: "utf-8", enc: unicode.UTF8}
	utf16le = Encoding{Name: "utf-16le", enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)}
	utf16be = Encoding{Name: "utf-16be", enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)}
)

// IsUTF8 reports whether the encoding is UTF-8.
func (e Encoding) IsUTF8() bool { return e.Name == UTF8.Name }

// ASCIICompatible reports whether the encoding represents the ASCII range in single bytes (anything but UTF-16).
func (e Encoding) ASCIICompatible() bool { return !strings.HasPrefix(e.Name, "utf-16") }

// NewDecoder returns a converter from this encoding to UTF-8.
// Invalid byte sequences become the replacement character (U+FFFD).
func (e Encoding) NewDecoder() *encoding.Decoder { return e.enc.NewDecoder() }

// Decode converts b to a UTF-8 string.
func (e Encoding) Decode(b []byte) string {
	if e.IsUTF8() {
		return strings.ToValidUTF8(string(b), "\uFFFD")
	}
	out, err := e.enc.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "\uFFFD")
	}
	return string(out)
}

// DecodePrefix converts to UTF-8 the longest prefix of src whose converted size fits in max bytes.
// When atEOF is false, an incomplete character at the end is left unconverted.
// consumed is the number of src bytes converted; full reports whether it stopped at max.
func (e Encoding) DecodePrefix(src []byte, max int, atEOF bool) (s string, consumed int, full bool) {
	dst := make([]byte, max)
	nDst, nSrc, err := e.enc.NewDecoder().Transform(dst, src, atEOF)
	return string(dst[:nDst]), nSrc, err == transform.ErrShortDst
}

// Encode converts the UTF-8 string s to bytes in this encoding.
// It returns an error when s contains characters this encoding cannot represent.
func (e Encoding) Encode(s string) ([]byte, error) {
	if e.IsUTF8() {
		return []byte(s), nil
	}
	b, err := e.enc.NewEncoder().Bytes([]byte(s))
	if err == nil {
		return b, nil
	}
	for i, r := range s {
		if _, err := e.enc.NewEncoder().String(string(r)); err != nil {
			return nil, fmt.Errorf("character %q at byte %d cannot be encoded in %s", r, i, e.Name)
		}
	}
	return nil, fmt.Errorf("encode to %s: %w", e.Name, err)
}

// BOM returns the byte order mark of this encoding, or nil for encodings without one.
func (e Encoding) BOM() []byte {
	switch e.Name {
	case UTF8.Name:
		return []byte{0xEF, 0xBB, 0xBF}
	case utf16le.Name:
		return []byte{0xFF, 0xFE}
	case utf16be.Name:
		return []byte{0xFE, 0xFF}
	}
	return nil
}

// DetectBOM looks for a byte order mark at the start of b and returns the encoding and the BOM length.
func DetectBOM(b []byte) (Encoding, int, bool) {
	for _, e := range []Encoding{UTF8, utf16le, utf16be} {
		if bom := e.BOM(); bytes.HasPrefix(b, bom) {
			return e, len(bom), true
		}
	}
	return Encoding{}, 0, false
}

// special lists names handled differently from the htmlindex default.
var special = map[string]Encoding{
	"utf-16":   utf16le,
	"utf-16le": utf16le,
	"utf16le":  utf16le,
	"utf-16be": utf16be,
	"utf16be":  utf16be,
}

// aliases are common alternative names htmlindex does not know.
var aliases = map[string]string{
	"eucjp":    "euc-jp",
	"euc_jp":   "euc-jp",
	"ujis":     "euc-jp",
	"shiftjis": "shift_jis",
	"euckr":    "euc-kr",
	"euc_kr":   "euc-kr",
}

// codePages maps Windows code page numbers to encodings.
var codePages = map[int]encoding.Encoding{
	437:   charmap.CodePage437,
	850:   charmap.CodePage850,
	852:   charmap.CodePage852,
	855:   charmap.CodePage855,
	858:   charmap.CodePage858,
	860:   charmap.CodePage860,
	862:   charmap.CodePage862,
	863:   charmap.CodePage863,
	865:   charmap.CodePage865,
	866:   charmap.CodePage866,
	874:   charmap.Windows874,
	932:   japanese.ShiftJIS,
	936:   simplifiedchinese.GBK,
	949:   korean.EUCKR,
	950:   traditionalchinese.Big5,
	1250:  charmap.Windows1250,
	1251:  charmap.Windows1251,
	1252:  charmap.Windows1252,
	1253:  charmap.Windows1253,
	1254:  charmap.Windows1254,
	1255:  charmap.Windows1255,
	1256:  charmap.Windows1256,
	1257:  charmap.Windows1257,
	1258:  charmap.Windows1258,
	20932: japanese.EUCJP,
	28591: charmap.ISO8859_1,
	51932: japanese.EUCJP,
	54936: simplifiedchinese.GB18030,
}

// FromCodePage returns the encoding for a Windows code page number.
func FromCodePage(cp int) (Encoding, bool) {
	switch cp {
	case 65001:
		return UTF8, true
	case 1200:
		return utf16le, true
	case 1201:
		return utf16be, true
	}
	enc, ok := codePages[cp]
	if !ok {
		return Encoding{}, false
	}
	name, err := htmlindex.Name(enc)
	if err != nil {
		name = "cp" + strconv.Itoa(cp)
	}
	return Encoding{Name: name, enc: enc}, true
}

// Lookup finds an encoding by name.
// It accepts WHATWG names and aliases (shift_jis, sjis, euc-jp, gbk, windows-1252 and so on),
// utf-16le, utf-16be and Windows code pages (cp932, 932 and so on).
func Lookup(name string) (Encoding, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if e, ok := special[n]; ok {
		return e, nil
	}
	if a, ok := aliases[n]; ok {
		n = a
	}
	if cp, err := strconv.Atoi(strings.TrimPrefix(n, "cp")); err == nil {
		if e, ok := FromCodePage(cp); ok {
			return e, nil
		}
	}
	enc, err := htmlindex.Get(n)
	if err != nil {
		return Encoding{}, fmt.Errorf("unknown charset %q", name)
	}
	canonical, err := htmlindex.Name(enc)
	if err != nil {
		canonical = n
	}
	if canonical == UTF8.Name {
		return UTF8, nil
	}
	return Encoding{Name: canonical, enc: enc}, nil
}

// Choice is the resolved encoding the agent specified.
type Choice struct {
	// Auto means: use the bytes as is when they are valid UTF-8, otherwise treat them as Enc.
	Auto bool
	Enc  Encoding
}

// Parse resolves an encoding name given in a tool argument.
// An empty string and "auto" mean automatic detection; "system" is the target's default encoding returned by system.
// When allowAuto is false, an empty string means def and "auto" is an error.
func Parse(name string, system func() Encoding, allowAuto bool, def Encoding) (Choice, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		if allowAuto {
			return Choice{Auto: true, Enc: system()}, nil
		}
		return Choice{Enc: def}, nil
	case "auto":
		if !allowAuto {
			return Choice{}, fmt.Errorf("charset auto is not allowed here")
		}
		return Choice{Auto: true, Enc: system()}, nil
	case "system":
		return Choice{Enc: system()}, nil
	}
	e, err := Lookup(name)
	return Choice{Enc: e}, err
}

// IncompleteTail returns the number of bytes of an incomplete UTF-8 character at the end of b.
func IncompleteTail(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if !utf8.RuneStart(b[i]) {
			continue
		}
		if utf8.FullRune(b[i:]) {
			return 0
		}
		return len(b) - i
	}
	return 0
}

// LooksUTF8 reports whether b is valid UTF-8.
// Characters split at the start and end of the range are tolerated.
func LooksUTF8(b []byte) bool {
	start := 0
	for start < len(b) && start < utf8.UTFMax-1 && !utf8.RuneStart(b[start]) {
		start++
	}
	return utf8.Valid(b[start : len(b)-IncompleteTail(b[start:])])
}

// Newline returns the newline style of s: lf, crlf, cr or mixed, or an empty string when s has no newline.
func Newline(s string) string {
	crlf := strings.Count(s, "\r\n")
	lf := strings.Count(s, "\n") - crlf
	cr := strings.Count(s, "\r") - crlf
	kinds := 0
	result := ""
	for _, k := range []struct {
		n    int
		name string
	}{{crlf, "crlf"}, {lf, "lf"}, {cr, "cr"}} {
		if k.n > 0 {
			kinds++
			result = k.name
		}
	}
	if kinds > 1 {
		return "mixed"
	}
	return result
}

// ConvertNewline converts the newlines of s according to mode (keep, lf, crlf).
func ConvertNewline(s, mode string) (string, error) {
	switch mode {
	case "", "keep":
		return s, nil
	case "lf":
		return strings.ReplaceAll(s, "\r\n", "\n"), nil
	case "crlf":
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n"), nil
	}
	return "", fmt.Errorf("unknown newline %q (use keep, lf or crlf)", mode)
}
