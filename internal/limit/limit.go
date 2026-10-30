// Package limit は出力サイズの上限を扱います。
//
// すべてのツールの出力に上限を設け、超えた場合は先頭と末尾を残して、切り詰めたことを示します。
package limit

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// Buffer は先頭と末尾だけを保持する io.Writer です。
// 長大な出力を全部メモリに載せずに、上限内の要約を作るために使います。
type Buffer struct {
	mu      sync.Mutex
	headMax int
	tailMax int
	head    []byte
	tail    []byte
	total   int64
}

// NewBuffer は合計 max バイトまでを保持する Buffer を作ります。
func NewBuffer(max int) *Buffer {
	h := max / 2
	return &Buffer{headMax: h, tailMax: max - h}
}

// Write は io.Writer を実装します。
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
		// 末尾側は上限の 2 倍に達したときにまとめて詰め直す。
		if len(b.tail) > 2*b.tailMax {
			b.tail = append([]byte(nil), b.tail[len(b.tail)-b.tailMax:]...)
		}
	}
	return n, nil
}

// Total は書き込まれた合計バイト数を返します。
func (b *Buffer) Total() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

// Truncated は上限を超えて切り詰めたかどうかを返します。
func (b *Buffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.omitted() > 0
}

func (b *Buffer) omitted() int64 {
	return b.total - int64(len(b.head)) - int64(min(len(b.tail), b.tailMax))
}

// String は保持している内容を返します。切り詰めた場合は省略したバイト数を間に挟みます。
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	omitted := b.omitted()
	if omitted <= 0 {
		return strings.ToValidUTF8(string(b.head)+string(b.tail), "\uFFFD")
	}
	// 切り詰めた境界で分断された文字は、置換文字にせずに取り除く。
	head := b.head[:len(b.head)-textenc.IncompleteTail(b.head)]
	tail := b.tail[len(b.tail)-b.tailMax:]
	for i := 0; i < utf8.UTFMax-1 && len(tail) > 0 && !utf8.RuneStart(tail[0]); i++ {
		tail = tail[1:]
	}
	return strings.ToValidUTF8(string(head), "\uFFFD") +
		Marker(omitted) +
		strings.ToValidUTF8(string(tail), "\uFFFD")
}

// Marker は切り詰めた箇所に挿入する文字列です。
func Marker(omitted int64) string {
	return fmt.Sprintf("\n... [host-shell: %d bytes omitted] ...\n", omitted)
}

// Text は s を max バイト以内に収めます。
func Text(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	b := NewBuffer(max)
	b.Write([]byte(s))
	return b.String(), true
}

// Ring は末尾の一定量だけを保持し、先頭からの絶対位置で読み出せる io.Writer です。
// 長時間ジョブの出力を、エージェントが少しずつ取得するために使います。
type Ring struct {
	mu   sync.Mutex
	max  int
	base int64 // buf[0] の絶対位置
	buf  []byte
}

// NewRing は少なくとも max バイトを保持する Ring を作ります。
func NewRing(max int) *Ring {
	return &Ring{max: max}
}

// Write は io.Writer を実装します。
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

// Total は書き込まれた合計バイト数 (次に書かれる位置) を返します。
func (r *Ring) Total() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base + int64(len(r.buf))
}

// Read は絶対位置 off から最大 n バイトを返します。
// off の位置がすでに破棄されていた場合、dropped に読めなかったバイト数を返します。
func (r *Ring) Read(off int64, n int) (data string, next int64, dropped int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if off < r.base {
		dropped = r.base - off
		off = r.base
	}
	start := min(int(off-r.base), len(r.buf))
	end := min(start+n, len(r.buf))
	// 読み取りの境界で文字を分断しないよう、末尾の不完全な文字は次の読み取りに回す。
	if t := textenc.IncompleteTail(r.buf[start:end]); end-t > start {
		end -= t
	}
	return strings.ToValidUTF8(string(r.buf[start:end]), "\uFFFD"), r.base + int64(end), dropped
}
