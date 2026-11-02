package textenc

import (
	"bytes"
	"compress/gzip"
	"image"
	pngenc "image/png"
	"os"
	"strings"
	"testing"
)

// sjis is "日本語" in Shift_JIS.
var sjis = []byte{0x93, 0xfa, 0x96, 0x7b, 0x8c, 0xea}

func mustLookup(t *testing.T, name string) Encoding {
	t.Helper()
	e, err := Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLookup(t *testing.T) {
	for name, want := range map[string]string{
		"UTF8":        "utf-8",
		"sjis":        "shift_jis",
		"cp932":       "shift_jis",
		"932":         "shift_jis",
		"eucJP":       "euc-jp",
		"utf-16":      "utf-16le",
		"UTF-16BE":    "utf-16be",
		"cp437":       "cp437",
		"latin1":      "windows-1252",
		"65001":       "utf-8",
		"iso-2022-jp": "iso-2022-jp",
	} {
		if got := mustLookup(t, name).Name; got != want {
			t.Errorf("Lookup(%q) = %q, want %q", name, got, want)
		}
	}
	if _, err := Lookup("no-such-charset"); err == nil {
		t.Error("Lookup(no-such-charset) succeeded")
	}
}

func TestEncodeDecode(t *testing.T) {
	e := mustLookup(t, "shift_jis")
	b, err := e.Encode("日本語")
	if err != nil || !bytes.Equal(b, sjis) {
		t.Fatalf("Encode = %x, %v", b, err)
	}
	if s := e.Decode(sjis); s != "日本語" {
		t.Fatalf("Decode = %q", s)
	}
	if _, err := e.Encode("😀"); err == nil {
		t.Fatal("Encode of an unsupported character succeeded")
	}
}

func TestDecodePrefixKeepsIncompleteChar(t *testing.T) {
	e := mustLookup(t, "shift_jis")
	s, n, _ := e.DecodePrefix(sjis[:5], 100, false)
	if s != "日本" || n != 4 {
		t.Fatalf("DecodePrefix = %q, %d", s, n)
	}
	s, n, full := e.DecodePrefix(sjis, 6, true) // 6 bytes hold at most 2 characters
	if s != "日本" || n != 4 || !full {
		t.Fatalf("DecodePrefix(max 6) = %q, %d, %v", s, n, full)
	}
}

func TestWriterAutoPerLine(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, Choice{Auto: true, Enc: mustLookup(t, "shift_jis")})
	w.Write([]byte("utf-8: 日本語\nsjis: "))
	w.Write(sjis[:3]) // split in the middle of a character
	w.Write(append(sjis[3:], '\n'))
	w.Write([]byte("tail"))
	w.Close()
	if got := out.String(); got != "utf-8: 日本語\nsjis: 日本語\ntail" {
		t.Fatalf("output = %q", got)
	}
	if w.Converted() != "shift_jis" {
		t.Fatalf("Converted() = %q", w.Converted())
	}
}

func TestWriterUTF16BOM(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, Choice{Auto: true, Enc: UTF8})
	w.Write([]byte{0xff, 0xfe, 'o', 0, 'k', 0})
	w.Close()
	if out.String() != "ok" || w.Converted() != "utf-16le" {
		t.Fatalf("output = %q, converted = %q", out.String(), w.Converted())
	}
}

func TestWriterExplicit(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, Choice{Enc: mustLookup(t, "euc-jp")})
	b, _ := mustLookup(t, "euc-jp").Encode("漢字\n")
	w.Write(b[:3])
	w.Write(b[3:])
	w.Close()
	if out.String() != "漢字\n" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestNewline(t *testing.T) {
	for s, want := range map[string]string{"a\nb\n": "lf", "a\r\nb\r\n": "crlf", "a\r\nb\n": "mixed", "ab": ""} {
		if got := Newline(s); got != want {
			t.Errorf("Newline(%q) = %q, want %q", s, got, want)
		}
	}
	if got, _ := ConvertNewline("a\r\nb\nc", "crlf"); got != "a\r\nb\r\nc" {
		t.Errorf("ConvertNewline crlf = %q", got)
	}
	if got, _ := ConvertNewline("a\r\nb\nc", "lf"); got != "a\nb\nc" {
		t.Errorf("ConvertNewline lf = %q", got)
	}
}

func TestLooksUTF8(t *testing.T) {
	b := []byte("日本語")
	if !LooksUTF8(b[1:8]) { // both ends cut in the middle of a character
		t.Error("LooksUTF8 rejected a range cut inside characters")
	}
	if LooksUTF8(sjis) {
		t.Error("LooksUTF8 accepted Shift_JIS")
	}
}

func TestWriterFlushPrompt(t *testing.T) {
	sjis, err := Lookup("shift_jis")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := NewWriter(&buf, Choice{Auto: true, Enc: sjis})
	w.Write([]byte("Password: "))
	if buf.Len() != 0 {
		t.Fatalf("written before flush: %q", buf.String())
	}
	w.Flush()
	if buf.String() != "Password: " {
		t.Errorf("after flush: %q", buf.String())
	}
	// A tail cut in the middle of a character is kept.
	b, _ := sjis.Encode("日本")
	w.Write(append([]byte("x"), b[:3]...))
	w.Flush()
	w.Write(b[3:])
	w.Write([]byte("\n"))
	if buf.String() != "Password: x日本\n" {
		t.Errorf("result: %q", buf.String())
	}
}

func TestDetectUTF16(t *testing.T) {
	le, be := mustLookup(t, "utf-16le"), mustLookup(t, "utf-16be")
	enc := func(e Encoding, s string) []byte {
		b, err := e.Encode(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, c := range []struct {
		name string
		data []byte
		want string
	}{
		{"english le", enc(le, "package main\r\nfunc main() {}\r\n"), "utf-16le"},
		{"english be", enc(be, "package main\nfunc main() {}\n"), "utf-16be"},
		{"japanese le", enc(le, "日本語の文章です。\r\nEnglish words too.\r\n"), "utf-16le"},
		{"kanji lines le", enc(le, strings.Repeat("日本語の文章だけで書かれた行です。改行もあります。\r\n", 20)), "utf-16le"},
		{"short le", enc(le, "ok\r\n"), "utf-16le"},
		{"surrogate pair le", enc(le, "emoji 😀 here\n"), "utf-16le"},
		{"ascii", []byte("plain ASCII text\n"), ""},
		{"utf-8 japanese", []byte("日本語の文章です。\n"), ""},
		{"all zero", make([]byte, 64), ""},
		{"lone low surrogate", []byte{'a', 0, 'b', 0, 0x00, 0xDC, 'c', 0}, ""},
		{"one byte", []byte{'a'}, ""},
	} {
		e, ok := detectUTF16(c.data)
		if got := map[bool]string{true: e.Name}[ok]; got != c.want {
			t.Errorf("%s: detectUTF16 = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDetectUTF16Binary checks that common binary formats are not taken for UTF-16.
func TestDetectUTF16Binary(t *testing.T) {
	exe, err := os.Executable() // the test binary itself
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	samples := map[string][]byte{"executable": b, "executable (middle)": b[len(b)/2:]}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(bytes.Repeat([]byte("compressible text 0123456789\n"), 500))
	zw.Close()
	samples["gzip"] = gz.Bytes()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var png bytes.Buffer
	if err := pngenc.Encode(&png, img); err != nil {
		t.Fatal(err)
	}
	samples["png"] = png.Bytes()
	rnd := make([]byte, 4096)
	for i := range rnd {
		rnd[i] = byte((i*7919 + i*i*31) >> 3)
	}
	samples["pseudo-random"] = rnd
	for name, data := range samples {
		if e, ok := detectUTF16(data); ok {
			t.Errorf("%s was detected as %s", name, e.Name)
		}
	}
}

func TestWriterUTF16NoBOM(t *testing.T) {
	le := mustLookup(t, "utf-16le")
	for _, s := range []string{"ok\r\n", strings.Repeat("日本語の出力です。line\r\n", 10)} {
		b, _ := le.Encode(s)
		var out bytes.Buffer
		w := NewWriter(&out, Choice{Auto: true, Enc: UTF8})
		for i := 0; i < len(b); i += 7 { // in small pieces
			w.Write(b[i:min(i+7, len(b))])
		}
		w.Close()
		if out.String() != s || w.Converted() != "utf-16le" {
			t.Errorf("output = %q, converted = %q, want %q", out.String(), w.Converted(), s)
		}
	}
	// A short ASCII prompt is still flushed as is.
	var out bytes.Buffer
	w := NewWriter(&out, Choice{Auto: true, Enc: UTF8})
	w.Write([]byte("name? "))
	w.Flush()
	if out.String() != "name? " {
		t.Errorf("prompt = %q", out.String())
	}
}

func TestChoiceDetect(t *testing.T) {
	sj := mustLookup(t, "shift_jis")
	le := mustLookup(t, "utf-16le")
	bom := []byte{0xEF, 0xBB, 0xBF, 'a'}
	utf16 := append([]byte{}, "p\x00a\x00c\x00k\x00a\x00g\x00e\x00 \x00m\x00a\x00i\x00n\x00\r\x00\n\x00"...)
	for _, c := range []struct {
		name   string
		ch     Choice
		head   []byte
		want   string
		bomLen int
		ok     bool
	}{
		{"auto bom", Choice{Auto: true, Enc: sj}, bom, "utf-8", 3, true},
		{"explicit matching bom", Choice{Enc: UTF8}, bom, "utf-8", 3, true},
		{"explicit other bom", Choice{Enc: sj}, bom, "shift_jis", 0, false},
		{"auto utf-16", Choice{Auto: true, Enc: sj}, utf16, "utf-16le", 0, true},
		{"explicit ignores utf-16", Choice{Enc: sj}, utf16, "shift_jis", 0, false},
		{"plain", Choice{Auto: true, Enc: le}, []byte("plain"), "utf-16le", 0, false},
	} {
		got, n, ok := c.ch.Detect(c.head)
		if got.Enc.Name != c.want || n != c.bomLen || ok != c.ok {
			t.Errorf("%s: got (%s, %d, %v), want (%s, %d, %v)", c.name, got.Enc.Name, n, ok, c.want, c.bomLen, c.ok)
		}
		if ok && got.Auto {
			t.Errorf("%s: a detected encoding should not stay automatic", c.name)
		}
	}
}

func TestDecodeUnlessUTF8(t *testing.T) {
	sj := mustLookup(t, "shift_jis")
	if s, decoded := sj.DecodeUnlessUTF8([]byte("日本語")); s != "日本語" || decoded {
		t.Errorf("UTF-8 input: got %q, %v", s, decoded)
	}
	if s, decoded := sj.DecodeUnlessUTF8(sjis); s != "日本語" || !decoded {
		t.Errorf("Shift_JIS input: got %q, %v", s, decoded)
	}
}

func TestValidTruncated(t *testing.T) {
	b := []byte("日本")
	for _, c := range []struct {
		b    []byte
		want bool
	}{
		{b, true}, {b[:4], true}, {b[1:], false}, {sjis, false}, {nil, true},
	} {
		if got := ValidTruncated(c.b); got != c.want {
			t.Errorf("ValidTruncated(% x) = %v, want %v", c.b, got, c.want)
		}
	}
}
