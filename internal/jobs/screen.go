package jobs

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"

	"github.com/hondarer/host-shell-mcp/internal/pty"
)

// screen maintains a virtual terminal emulator fed by a PTY job's output so full-screen
// programs (top, vi, less) can be read as rendered text.
type screen struct {
	mu       sync.Mutex // serializes Write with String and CursorPosition
	em       *vt.Emulator
	stopping atomic.Bool
	stopped  chan struct{}
}

// newScreen creates a terminal screen of size cols x rows and forwards terminal query
// responses (cursor position, device attributes) back to t.
func newScreen(cols, rows int, t *pty.Terminal) *screen {
	if cols <= 0 {
		cols = pty.DefaultCols
	}
	if rows <= 0 {
		rows = pty.DefaultRows
	}
	s := &screen{em: vt.NewEmulator(cols, rows), stopped: make(chan struct{})}
	go s.answer(t)
	return s
}

// answer forwards emulator responses to t until stopped, closing em on the reader goroutine
// because vt.Emulator's Read and Close are not concurrency-safe.
func (s *screen) answer(t io.Writer) {
	defer close(s.stopped)
	buf := make([]byte, 1024)
	for {
		n, err := s.em.Read(buf)
		if err != nil {
			return
		}
		if s.stopping.Load() {
			s.em.Close()
			return
		}
		t.Write(buf[:n])
	}
}

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.em.Write(p)
}

func (s *screen) resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols <= 0 {
		cols = s.em.Width()
	}
	if rows <= 0 {
		rows = s.em.Height()
	}
	s.em.Resize(cols, rows)
}

func (s *screen) text() (text string, row, col int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := strings.Split(s.em.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	pos := s.em.CursorPosition()
	return strings.Join(lines, "\n"), pos.Y, pos.X
}

// close stops the answer loop after job output ends by sending a cursor position query (CSI 6n)
// that wakes em.Read.
func (s *screen) close() {
	s.stopping.Store(true)
	go s.Write([]byte("\x1b[6n"))
	select {
	case <-s.stopped:
	case <-time.After(2 * time.Second): // never block the end of the job on the emulator
	}
}
