// Package textenc は接続先の文字コードと UTF-8 の相互変換を扱います。
//
// MCP の通信は常に UTF-8 です。接続先のコマンドの出力やファイルが UTF-8 以外の場合に、
// ここで UTF-8 へ変換 (読み取り) し、または UTF-8 から変換 (書き込み) します。
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

// Encoding は文字コードと、その正式な名前の組です。
type Encoding struct {
	Name string
	enc  encoding.Encoding
}

var (
	// UTF8 は UTF-8 です。変換を行いません。
	UTF8    = Encoding{Name: "utf-8", enc: unicode.UTF8}
	utf16le = Encoding{Name: "utf-16le", enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)}
	utf16be = Encoding{Name: "utf-16be", enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)}
)

// IsUTF8 は UTF-8 かどうかを返します。
func (e Encoding) IsUTF8() bool { return e.Name == UTF8.Name }

// ASCIICompatible は ASCII の範囲を 1 バイトで表す文字コードかどうかを返します (UTF-16 以外)。
func (e Encoding) ASCIICompatible() bool { return !strings.HasPrefix(e.Name, "utf-16") }

// NewDecoder は、この文字コードから UTF-8 への変換器を返します。
// 正しくないバイト列は置換文字 (U+FFFD) に置き換えます。
func (e Encoding) NewDecoder() *encoding.Decoder { return e.enc.NewDecoder() }

// Decode は b を UTF-8 の文字列に変換します。
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

// DecodePrefix は src の先頭から、変換後の大きさが max バイトに収まる範囲を UTF-8 に変換します。
// atEOF が false の場合、末尾の不完全な文字は変換せずに残します。
// consumed は変換した src のバイト数、full は max に達して打ち切ったかどうかです。
func (e Encoding) DecodePrefix(src []byte, max int, atEOF bool) (s string, consumed int, full bool) {
	dst := make([]byte, max)
	nDst, nSrc, err := e.enc.NewDecoder().Transform(dst, src, atEOF)
	return string(dst[:nDst]), nSrc, err == transform.ErrShortDst
}

// Encode は UTF-8 の文字列 s を、この文字コードのバイト列に変換します。
// この文字コードで表せない文字がある場合はエラーを返します。
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

// BOM はこの文字コードのバイト順マークを返します。BOM のない文字コードでは nil です。
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

// DetectBOM は b の先頭のバイト順マークを調べ、文字コードと BOM の長さを返します。
func DetectBOM(b []byte) (Encoding, int, bool) {
	for _, e := range []Encoding{UTF8, utf16le, utf16be} {
		if bom := e.BOM(); bytes.HasPrefix(b, bom) {
			return e, len(bom), true
		}
	}
	return Encoding{}, 0, false
}

// special は htmlindex の既定と異なる扱いにする名前です。
var special = map[string]Encoding{
	"utf-16":   utf16le,
	"utf-16le": utf16le,
	"utf16le":  utf16le,
	"utf-16be": utf16be,
	"utf16be":  utf16be,
}

// aliases は htmlindex が知らない、よく使われる別名です。
var aliases = map[string]string{
	"eucjp":    "euc-jp",
	"euc_jp":   "euc-jp",
	"ujis":     "euc-jp",
	"shiftjis": "shift_jis",
	"euckr":    "euc-kr",
	"euc_kr":   "euc-kr",
}

// codePages は Windows のコード ページ番号と文字コードの対応です。
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

// FromCodePage は Windows のコード ページ番号に対応する文字コードを返します。
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

// Lookup は名前から文字コードを探します。
// WHATWG の名前と別名 (shift_jis、sjis、euc-jp、gbk、windows-1252 など)、
// utf-16le、utf-16be、Windows のコード ページ (cp932、932 など) を受け付けます。
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

// Choice はエージェントが指定した文字コードの解決結果です。
type Choice struct {
	// Auto は、UTF-8 として正しければそのまま使い、正しくなければ Enc とみなすことを表します。
	Auto bool
	Enc  Encoding
}

// Parse は、ツールの引数で指定された文字コードの名前を解決します。
// 空文字と "auto" は自動判定、"system" は system が返す接続先の既定の文字コードです。
// allowAuto が false の場合、空文字は def とし、"auto" はエラーにします。
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

// IncompleteTail は、b の末尾にある不完全な UTF-8 の文字のバイト数を返します。
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

// LooksUTF8 は、b が UTF-8 として正しいかどうかを返します。
// 範囲の境界で分断された先頭と末尾の文字は許容します。
func LooksUTF8(b []byte) bool {
	start := 0
	for start < len(b) && start < utf8.UTFMax-1 && !utf8.RuneStart(b[start]) {
		start++
	}
	return utf8.Valid(b[start : len(b)-IncompleteTail(b[start:])])
}

// Newline は s の改行の種類を返します。lf、crlf、cr、mixed のいずれかで、改行がなければ空文字です。
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

// ConvertNewline は s の改行を mode (keep、lf、crlf) に変換します。
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
