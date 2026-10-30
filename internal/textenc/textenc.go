// Package textenc converts between target character encodings and UTF-8 for MCP I/O.
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

// Encoding pairs a character encoding with its canonical name.
type Encoding struct {
	Name string
	enc  encoding.Encoding
}

var (
	// UTF8 is the no-op UTF-8 encoding.
	UTF8    = Encoding{Name: "utf-8", enc: unicode.UTF8}
	utf16le = Encoding{Name: "utf-16le", enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)}
	utf16be = Encoding{Name: "utf-16be", enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)}
)

// IsUTF8 reports whether the encoding is UTF-8.
func (e Encoding) IsUTF8() bool { return e.Name == UTF8.Name }

// ASCIICompatible reports whether ASCII bytes represent themselves (true for all supported encodings except UTF-16).
func (e Encoding) ASCIICompatible() bool { return !strings.HasPrefix(e.Name, "utf-16") }

// NewDecoder returns a decoder to UTF-8 that replaces invalid byte sequences with U+FFFD.
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

// DecodePrefix decodes the longest prefix of src that fits within max UTF-8 bytes,
// leaving any trailing incomplete rune unconverted when atEOF is false.
// full reports whether conversion stopped because dst reached max bytes.
func (e Encoding) DecodePrefix(src []byte, max int, atEOF bool) (s string, consumed int, full bool) {
	dst := make([]byte, max)
	nDst, nSrc, err := e.enc.NewDecoder().Transform(dst, src, atEOF)
	return string(dst[:nDst]), nSrc, err == transform.ErrShortDst
}

// Encode converts the UTF-8 string s to e, returning an error if s contains unrepresentable runes.
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

// BOM returns the byte order mark for e, or nil if e has none.
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

// DetectBOM checks for a UTF-8 or UTF-16 BOM at the start of b and returns the encoding and BOM length.
func DetectBOM(b []byte) (Encoding, int, bool) {
	for _, e := range []Encoding{UTF8, utf16le, utf16be} {
		if bom := e.BOM(); bytes.HasPrefix(b, bom) {
			return e, len(bom), true
		}
	}
	return Encoding{}, 0, false
}

// detectUTF16Bytes is the maximum prefix length inspected by DetectUTF16.
const detectUTF16Bytes = 4096

// DetectUTF16 reports whether BOM-less data b appears to be UTF-16 text and returns its byte order.
//
// UTF-16 text has a zero byte in one half of every ASCII code unit (including line breaks) and
// rarely contains U+0000, whereas binary data has either many all-zero units or few zero bytes
// on either side. DetectUTF16
// requires <=1% U+0000 units, >=5% zero bytes on one side, <=2% (+1 unit for short text) on the
// other, and valid surrogate pairing.
func DetectUTF16(b []byte) (Encoding, bool) {
	if len(b) > detectUTF16Bytes {
		b = b[:detectUTF16Bytes]
	}
	n := len(b) / 2
	if n < 2 {
		return Encoding{}, false
	}
	var first, second, both int // units with zero in first byte only, second byte only, or both
	for i := 0; i < n; i++ {
		switch x, y := b[2*i], b[2*i+1]; {
		case x == 0 && y == 0:
			both++
		case x == 0:
			first++
		case y == 0:
			second++
		}
	}
	if both*100 > n {
		return Encoding{}, false
	}
	e, zero, other := utf16le, second, first
	if first > second {
		e, zero, other = utf16be, first, second
	}
	if zero*20 < n || other > n/50+1 || !validUTF16(b[:2*n], e.Name == utf16le.Name) {
		return Encoding{}, false
	}
	return e, true
}

// validUTF16 reports whether b contains no unpaired surrogates (allowing a trailing high surrogate at the end of b).
func validUTF16(b []byte, little bool) bool {
	n := len(b) / 2
	for i := 0; i < n; i++ {
		u := uint16(b[2*i])<<8 | uint16(b[2*i+1])
		if little {
			u = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
		switch {
		case u >= 0xD800 && u < 0xDC00:
			if i == n-1 {
				return true
			}
			next := uint16(b[2*i+2])<<8 | uint16(b[2*i+3])
			if little {
				next = uint16(b[2*i+3])<<8 | uint16(b[2*i+2])
			}
			if next < 0xDC00 || next >= 0xE000 {
				return false
			}
			i++
		case u >= 0xDC00 && u < 0xE000:
			return false
		}
	}
	return true
}

// DetectUnicode detects a BOM or BOM-less UTF-16 at the start of b, returning the encoding and BOM length.
func DetectUnicode(b []byte) (Encoding, int, bool) {
	if e, n, ok := DetectBOM(b); ok {
		return e, n, true
	}
	if e, ok := DetectUTF16(b); ok {
		return e, 0, true
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

// Lookup resolves an encoding by WHATWG name/alias (shift_jis, sjis, euc-jp, gbk, windows-1252, etc.),
// utf-16le/utf-16be, or Windows code page (cp932, 932, etc.).
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

// Choice represents a resolved encoding option.
type Choice struct {
	// Auto passes valid UTF-8 through as-is and decodes non-UTF-8 bytes using Enc.
	Auto bool
	Enc  Encoding
}

// Parse resolves a tool's encoding argument: "" and "auto" select automatic detection (or def when
// allowAuto is false, where "auto" is rejected), and "system" selects system().
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

// IncompleteTail returns the length in bytes of any trailing incomplete UTF-8 rune in b.
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

// LooksUTF8 reports whether b is valid UTF-8, ignoring split runes at the start and end of b.
func LooksUTF8(b []byte) bool {
	start := 0
	for start < len(b) && start < utf8.UTFMax-1 && !utf8.RuneStart(b[start]) {
		start++
	}
	return utf8.Valid(b[start : len(b)-IncompleteTail(b[start:])])
}

// Newline returns the newline style of s ("lf", "crlf", "cr", "mixed", or "" if s has no line breaks).
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
