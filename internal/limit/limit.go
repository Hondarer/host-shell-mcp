// Package limit bounds tool output sizes, keeping the head and tail when truncating.
package limit

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// Buffer is an io.Writer that retains up to headMax + tailMax bytes, discarding the middle
// of oversized streams without holding the entire output in memory.
type Buffer struct {
	mu      sync.Mutex
	headMax int
	tailMax int
	head    []byte
	tail    []byte
	total   int64
}

// NewBuffer creates a Buffer that retains up to max bytes across head and tail.
func NewBuffer(max int) *Buffer {
	h := max / 2
	return &Buffer{headMax: h, tailMax: max - h}
}

// Write implements io.Writer.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.total += int64(n)
	if room := b.headMax - len(b.head); room > 0 {
		k := min(room, len(p))
		b.head = append(b.head, p[:k]...)
		p = p[k:]
	}
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if len(b.tail) > 2*b.tailMax {
			b.tail = append([]byte(nil), b.tail[len(b.tail)-b.tailMax:]...)
		}
	}
	return n, nil
}

// Total returns the number of bytes written.
func (b *Buffer) Total() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

// Truncated reports whether the output exceeded the limit and was truncated.
func (b *Buffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.omitted() > 0
}

func (b *Buffer) omitted() int64 {
	return b.total - int64(len(b.head)) - int64(min(len(b.tail), b.tailMax))
}

// String returns the buffered content, inserting Marker(omitted) between head and tail when truncated.
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	omitted := b.omitted()
	if omitted <= 0 {
		return strings.ToValidUTF8(string(b.head)+string(b.tail), "\uFFFD")
	}
	// Trim UTF-8 runes split at the truncation boundary instead of emitting replacement characters.
	head := b.head[:len(b.head)-textenc.IncompleteTail(b.head)]
	tail := b.tail[len(b.tail)-b.tailMax:]
	for i := 0; i < utf8.UTFMax-1 && len(tail) > 0 && !utf8.RuneStart(tail[0]); i++ {
		tail = tail[1:]
	}
	return strings.ToValidUTF8(string(head), "\uFFFD") +
		Marker(omitted) +
		strings.ToValidUTF8(string(tail), "\uFFFD")
}

// Marker returns the truncation notice for omitted bytes.
func Marker(omitted int64) string {
	return fmt.Sprintf("\n... [host-shell: %d bytes omitted] ...\n", omitted)
}

// Text truncates s to at most max bytes and reports whether it was truncated.
func Text(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	b := NewBuffer(max)
	b.Write([]byte(s))
	return b.String(), true
}

// Ring is an io.Writer that retains the trailing window of a stream for incremental
// reads by absolute byte offset.
type Ring struct {
	mu   sync.Mutex
	max  int
	base int64 // absolute offset of buf[0]
	buf  []byte
}

// NewRing creates a Ring that retains at least max bytes.
func NewRing(max int) *Ring {
	return &Ring{max: max}
}

// Write implements io.Writer.
func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > 2*r.max {
		drop := len(r.buf) - r.max
		r.base += int64(drop)
		r.buf = append([]byte(nil), r.buf[drop:]...)
	}
	return len(p), nil
}

// Total returns the total bytes written (the next write offset).
func (r *Ring) Total() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base + int64(len(r.buf))
}

// Read returns up to n bytes starting at absolute offset off, the next offset, and
// the number of bytes already evicted before off.
func (r *Ring) Read(off int64, n int) (data string, next int64, dropped int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if off < r.base {
		dropped = r.base - off
		off = r.base
	}
	start := min(int(off-r.base), len(r.buf))
	end := min(start+n, len(r.buf))
	// Hold back any trailing incomplete UTF-8 rune for the next read.
	if t := textenc.IncompleteTail(r.buf[start:end]); end-t > start {
		end -= t
	}
	return strings.ToValidUTF8(string(r.buf[start:end]), "\uFFFD"), r.base + int64(end), dropped
}
