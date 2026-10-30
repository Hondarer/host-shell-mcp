package textenc

import (
	"bytes"
	"io"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/transform"
)

// maxPending は、改行が来ないときに判定を保留しておく最大のバイト数です。
const maxPending = 4096

// Writer は、子プロセスの出力を UTF-8 に変換しながら w に書く io.Writer です。
//
// 自動判定では行 (\n または \r まで) ごとに、UTF-8 として正しければそのまま、
// 正しくなければ代わりの文字コードとみなして変換します。
// UTF-8 とそれ以外が混ざった出力も、行単位で正しく変換できます。
// 出力の先頭に UTF-16 の BOM がある場合は、出力全体を UTF-16 として変換します。
type Writer struct {
	mu       sync.Mutex
	w        io.Writer
	auto     bool
	fallback Encoding
	conv     io.WriteCloser // 出力全体を変換するときの変換器
	started  bool
	pending  []byte
	used     string
}

// NewWriter は c に従って変換する Writer を作ります。
func NewWriter(w io.Writer, c Choice) *Writer {
	if c.Enc.enc == nil {
		c.Enc = UTF8
	}
	tw := &Writer{w: w, fallback: c.Enc, auto: c.Auto && !c.Enc.IsUTF8()}
	if !c.Auto && !c.Enc.IsUTF8() {
		tw.startConv(c.Enc)
	}
	// 代わりの文字コードが UTF-8 の場合も、UTF-16 の BOM だけは調べる。
	tw.started = !c.Auto
	return tw
}

func (w *Writer) startConv(e Encoding) {
	w.conv = transform.NewWriter(w.w, e.NewDecoder())
	w.used = e.Name
}

// Write は io.Writer を実装します。
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if w.conv != nil {
		_, err := w.conv.Write(p)
		return n, err
	}
	if w.started && !w.auto {
		_, err := w.w.Write(p)
		return n, err
	}
	w.pending = append(w.pending, p...)
	if !w.started {
		if len(w.pending) < 2 {
			return n, nil
		}
		w.started = true
		if e, l, ok := DetectBOM(w.pending); ok && !e.ASCIICompatible() {
			w.pending = w.pending[l:]
			w.startConv(e)
			_, err := w.conv.Write(w.pending)
			w.pending = nil
			return n, err
		}
		if !w.auto {
			_, err := w.w.Write(w.pending)
			w.pending = nil
			return n, err
		}
	}
	return n, w.flushLines(false)
}

// flushLines は保留中の出力を行ごとに変換して書きます。
func (w *Writer) flushLines(final bool) error {
	for {
		i := bytes.IndexAny(w.pending, "\n\r")
		if i < 0 {
			break
		}
		if _, err := w.emit(w.pending[:i+1], true); err != nil {
			return err
		}
		w.pending = w.pending[i+1:]
	}
	if final || len(w.pending) > maxPending {
		k, err := w.emit(w.pending, final)
		if err != nil {
			return err
		}
		w.pending = w.pending[k:]
	}
	w.pending = append([]byte(nil), w.pending...)
	return nil
}

// emit は 1 行 (または行の一部) を変換して書き、処理したバイト数を返します。
func (w *Writer) emit(b []byte, final bool) (int, error) {
	n := len(b)
	if !final {
		n -= IncompleteTail(b)
	}
	if utf8.Valid(b[:n]) {
		_, err := w.w.Write(b[:n])
		return n, err
	}
	s, consumed, _ := w.fallback.DecodePrefix(b, len(b)*3+utf8.UTFMax, final)
	w.used = w.fallback.Name
	_, err := io.WriteString(w.w, s)
	return consumed, err
}

// Flush は、改行を待たずに保留中の出力を書き出します。文字の途中で切れている末尾だけは残します。
// 改行のないプロンプト (「Password: 」など) を、行が終わる前に読めるようにするために使います。
// 自動判定では、書き出した部分と行の残りを別々に判定します。
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conv != nil || !w.started || len(w.pending) == 0 {
		return nil
	}
	if !w.auto {
		_, err := w.w.Write(w.pending)
		w.pending = nil
		return err
	}
	k, err := w.emit(w.pending, false)
	w.pending = append([]byte(nil), w.pending[k:]...)
	return err
}

// Close は保留中の出力を書き出します。子プロセスの終了後に呼びます。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conv != nil {
		return w.conv.Close()
	}
	if len(w.pending) == 0 {
		return nil
	}
	if !w.auto {
		_, err := w.w.Write(w.pending)
		w.pending = nil
		return err
	}
	return w.flushLines(true)
}

// Converted は、UTF-8 以外から変換した場合にその文字コードの名前を返します。変換していなければ空文字です。
func (w *Writer) Converted() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.used
}
