package textenc

import (
	"bytes"
	"io"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/transform"
)

// maxPending caps bytes buffered while waiting for a newline during auto-detection.
const maxPending = 4096

// detectPending is the initial byte threshold buffered before checking for UTF-16.
const detectPending = 64

// Writer converts child process output to UTF-8 before forwarding it to w.
//
// In auto mode, each line (up to \n or \r) is passed through if valid UTF-8 or decoded using
// fallback otherwise, handling mixed-encoding output line by line. Output starting with UTF-16
// (via BOM or DetectUTF16) is converted as UTF-16 for the entire stream.
type Writer struct {
	mu       sync.Mutex
	w        io.Writer
	auto     bool
	fallback Encoding
	conv     io.WriteCloser // whole-stream converter (explicit encoding or detected UTF-16)
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
	// Even when the fallback is UTF-8, inspect initial output for UTF-16.
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

// start inspects the initial buffered bytes for UTF-16 or non-auto passthrough, returning true if pending was flushed.
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

// flushLines converts and writes pending output line by line.
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

// emit converts and writes a line or chunk and returns the number of source bytes consumed.
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

// Flush writes buffered bytes without waiting for a newline (retaining only a trailing split rune)
// so newline-less prompts such as "Password: " can be read immediately.
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

// Close flushes any remaining buffered bytes after the child process exits.
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

// Converted returns the source encoding name if conversion occurred, or "" if all output was UTF-8.
func (w *Writer) Converted() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.used
}
