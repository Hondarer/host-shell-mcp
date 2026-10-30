package textenc

import (
	"bytes"
	"testing"
)

// sjis は「日本語」の Shift_JIS のバイト列です。
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
	s, n, full := e.DecodePrefix(sjis, 6, true) // 6 バイトには 2 文字まで
	if s != "日本" || n != 4 || !full {
		t.Fatalf("DecodePrefix(max 6) = %q, %d, %v", s, n, full)
	}
}

func TestWriterAutoPerLine(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, Choice{Auto: true, Enc: mustLookup(t, "shift_jis")})
	w.Write([]byte("utf-8: 日本語\nsjis: "))
	w.Write(sjis[:3]) // 文字の途中で分断する
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
	if !LooksUTF8(b[1:8]) { // 先頭と末尾が文字の途中
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
	// 文字の途中で切れた末尾は残す。
	b, _ := sjis.Encode("日本")
	w.Write(append([]byte("x"), b[:3]...))
	w.Flush()
	w.Write(b[3:])
	w.Write([]byte("\n"))
	if buf.String() != "Password: x日本\n" {
		t.Errorf("result: %q", buf.String())
	}
}
