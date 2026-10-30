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

// screen keeps the state of a PTY job's terminal: a terminal emulator that is fed the job's output,
// so that full-screen programs (top, vi, less) can be read as the screen they draw.
type screen struct {
	mu       sync.Mutex // the emulator's own String and CursorPosition are not safe with Write
	em       *vt.Emulator
	stopping atomic.Bool
	stopped  chan struct{}
}

// newScreen creates a screen of the terminal size of the job. Answers to queries the program sends
// to the terminal (cursor position, device attributes) are written back to the terminal, as a real
// terminal would do.
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

// answer passes the emulator's answers to the terminal until close. The emulator is closed here,
// in the goroutine that reads it, because its Close and Read are not safe to call concurrently.
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

// close stops the screen after the job's output has ended. A cursor position query makes the
// emulator answer, which wakes answer up to close it.
func (s *screen) close() {
	s.stopping.Store(true)
	go s.Write([]byte("\x1b[6n"))
	select {
	case <-s.stopped:
	case <-time.After(2 * time.Second): // never block the end of the job on the emulator
	}
}
