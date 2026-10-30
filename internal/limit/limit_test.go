package limit

import (
	"strings"
	"testing"
)

func TestBufferWithinLimit(t *testing.T) {
	b := NewBuffer(16)
	b.Write([]byte("hello "))
	b.Write([]byte("world"))
	if got := b.String(); got != "hello world" {
		t.Fatalf("String() = %q", got)
	}
	if b.Truncated() {
		t.Fatal("Truncated() = true, want false")
	}
}

func TestBufferKeepsHeadAndTail(t *testing.T) {
	b := NewBuffer(10)
	for i := 0; i < 100; i++ {
		b.Write([]byte("0123456789"))
	}
	got := b.String()
	if !b.Truncated() {
		t.Fatal("Truncated() = false, want true")
	}
	if !strings.HasPrefix(got, "01234") || !strings.HasSuffix(got, "56789") {
		t.Fatalf("String() = %q", got)
	}
	if !strings.Contains(got, Marker(990)) {
		t.Fatalf("String() = %q, want marker for 990 bytes", got)
	}
}

func TestRingRead(t *testing.T) {
	r := NewRing(4)
	r.Write([]byte("abcdefghij")) // 2*max を超えるので先頭が破棄される
	data, next, dropped := r.Read(0, 100)
	if data != "ghij" || next != 10 || dropped != 6 {
		t.Fatalf("Read(0) = %q, %d, %d", data, next, dropped)
	}
	data, next, dropped = r.Read(next, 100)
	if data != "" || next != 10 || dropped != 0 {
		t.Fatalf("Read(10) = %q, %d, %d", data, next, dropped)
	}
}

func TestRingReadKeepsCharacters(t *testing.T) {
	r := NewRing(100)
	r.Write([]byte("あい")) // 6 バイト
	data, next, _ := r.Read(0, 4)
	if data != "あ" || next != 3 {
		t.Fatalf("Read(0, 4) = %q, %d", data, next)
	}
	data, next, _ = r.Read(next, 4)
	if data != "い" || next != 6 {
		t.Fatalf("Read(3, 4) = %q, %d", data, next)
	}
}
