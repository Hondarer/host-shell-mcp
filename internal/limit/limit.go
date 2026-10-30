// Package limit handles output size limits.
//
// Every tool output is capped; longer output keeps its head and tail and is marked as truncated.
package limit

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// Buffer is an io.Writer that keeps only the head and the tail.
// It summarizes long output within the limit without holding all of it in memory.
type Buffer struct {
	mu      sync.Mutex
	headMax int
	tailMax int
	head    []byte
	tail    []byte
	total   int64
}

// NewBuffer creates a Buffer that keeps up to max bytes in total.
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
		// The tail is compacted when it reaches twice the limit.
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

// String returns the kept content. When truncated, the number of omitted bytes is inserted between head and tail.
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	omitted := b.omitted()
	if omitted <= 0 {
		return strings.ToValidUTF8(string(b.head)+string(b.tail), "\uFFFD")
	}
	// Drop characters split at the truncation boundaries instead of turning them into replacement characters.
	head := b.head[:len(b.head)-textenc.IncompleteTail(b.head)]
	tail := b.tail[len(b.tail)-b.tailMax:]
	for i := 0; i < utf8.UTFMax-1 && len(tail) > 0 && !utf8.RuneStart(tail[0]); i++ {
		tail = tail[1:]
	}
	return strings.ToValidUTF8(string(head), "\uFFFD") +
		Marker(omitted) +
		strings.ToValidUTF8(string(tail), "\uFFFD")
}

// Marker is inserted where content was omitted.
func Marker(omitted int64) string {
	return fmt.Sprintf("\n... [host-shell: %d bytes omitted] ...\n", omitted)
}

// Text fits s within max bytes.
func Text(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	b := NewBuffer(max)
	b.Write([]byte(s))
	return b.String(), true
}

// Ring is an io.Writer that keeps only the last part of the output and reads by absolute offset from the start.
// The agent uses it to fetch the output of long-running jobs bit by bit.
type Ring struct {
	mu   sync.Mutex
	max  int
	base int64 // absolute offset of buf[0]
	buf  []byte
}

// NewRing creates a Ring that keeps at least max bytes.
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

// Total returns the number of bytes written (the offset of the next write).
func (r *Ring) Total() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base + int64(len(r.buf))
}

// Read returns up to n bytes from absolute offset off.
// If off has already been dropped, dropped is the number of bytes that could not be read.
func (r *Ring) Read(off int64, n int) (data string, next int64, dropped int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if off < r.base {
		dropped = r.base - off
		off = r.base
	}
	start := min(int(off-r.base), len(r.buf))
	end := min(start+n, len(r.buf))
	// Leave an incomplete character at the end for the next read, so reads never split a character.
	if t := textenc.IncompleteTail(r.buf[start:end]); end-t > start {
		end -= t
	}
	return strings.ToValidUTF8(string(r.buf[start:end]), "\uFFFD"), r.base + int64(end), dropped
}
