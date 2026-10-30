package textenc

import (
	"bytes"
	"io"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/transform"
)

// maxPending is the most bytes held for detection while no newline arrives.
const maxPending = 4096

// detectPending is how much output is held at the start to detect UTF-16 (with or without a BOM).
const detectPending = 64

// Writer is an io.Writer that converts child process output to UTF-8 while writing it to w.
//
// With automatic detection, each line (up to \n or \r) is passed through when it is valid UTF-8,
// and otherwise converted from the fallback encoding.
// Output mixing UTF-8 and other encodings is converted correctly line by line.
// When the output starts as UTF-16 (with a BOM, or detected by DetectUTF16), the whole output is
// converted as UTF-16.
type Writer struct {
	mu       sync.Mutex
	w        io.Writer
	auto     bool
	fallback Encoding
	conv     io.WriteCloser // converter used when the whole output is converted
	started  bool
	pending  []byte
	used     string
}

// NewWriter creates a Writer that converts according to c.
func NewWriter(w io.Writer, c Choice) *Writer {
	if c.Enc.enc == nil {
		c.Enc = UTF8
	}
	tw := &Writer{w: w, fallback: c.Enc, auto: c.Auto && !c.Enc.IsUTF8()}
	if !c.Auto && !c.Enc.IsUTF8() {
		tw.startConv(c.Enc)
	}
	// Even when the fallback encoding is UTF-8, check for a UTF-16 BOM.
	tw.started = !c.Auto
	return tw
}

func (w *Writer) startConv(e Encoding) {
	w.conv = transform.NewWriter(w.w, e.NewDecoder())
	w.used = e.Name
}

// Write implements io.Writer.
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
		if len(w.pending) < detectPending {
			return n, nil
		}
		if done, err := w.start(); done {
			return n, err
		}
	}
	return n, w.flushLines(false)
}

// start decides how to convert from the output held so far. done reports that the held output
// has been written and nothing is left for line-by-line conversion.
func (w *Writer) start() (done bool, err error) {
	w.started = true
	if e, l, ok := DetectUnicode(w.pending); ok && !e.ASCIICompatible() {
		w.startConv(e)
		_, err := w.conv.Write(w.pending[l:])
		w.pending = nil
		return true, err
	}
	if !w.auto {
		_, err := w.w.Write(w.pending)
		w.pending = nil
		return true, err
	}
	return false, nil
}

// flushLines converts and writes the pending output line by line.
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

// emit converts and writes one line (or part of one) and returns the number of bytes processed.
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

// Flush writes out the pending output without waiting for a newline, keeping only a tail cut in the middle of a character.
// It makes prompts without a newline (such as "Password: ") readable before the line ends.
// With automatic detection, the part written out and the rest of the line are detected separately.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conv != nil || len(w.pending) == 0 {
		return nil
	}
	if !w.started {
		if done, err := w.start(); done {
			return err
		}
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

// Close writes out the pending output. Call it after the child process ends.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conv != nil {
		return w.conv.Close()
	}
	if len(w.pending) == 0 {
		return nil
	}
	if !w.started {
		if done, err := w.start(); done {
			if w.conv != nil {
				return w.conv.Close()
			}
			return err
		}
	}
	if !w.auto {
		_, err := w.w.Write(w.pending)
		w.pending = nil
		return err
	}
	return w.flushLines(true)
}

// Converted returns the name of the encoding converted from, or an empty string when nothing was converted.
func (w *Writer) Converted() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.used
}
