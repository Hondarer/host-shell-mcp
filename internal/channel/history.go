package channel

import (
	"errors"
	"net"
	"sync"
)

const historySize = 100

// History keeps the latest diagnostic events for the bridge's status tools.
type History[T any] struct {
	mu    sync.Mutex
	seq   uint64
	seqs  []uint64
	items []T
}

// Add appends the event built for the next sequence number.
func (h *History[T]) Add(build func(seq uint64) T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	h.seqs = append(h.seqs, h.seq)
	h.items = append(h.items, build(h.seq))
	if len(h.items) > historySize {
		h.seqs = append([]uint64{}, h.seqs[len(h.seqs)-historySize:]...)
		h.items = append([]T{}, h.items[len(h.items)-historySize:]...)
	}
}

// Merge lets fn update the latest event in place and reports whether it did.
func (h *History[T]) Merge(fn func(last *T) bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.items) > 0 && fn(&h.items[len(h.items)-1])
}

// Page returns events after after in ascending order. next is the last returned sequence,
// or after when nothing is returned; truncated reports dropped or unreturned events.
func (h *History[T]) Page(after uint64, limit int) (events []T, next uint64, truncated bool, err error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > historySize {
		return nil, 0, false, errors.New("limit must be 1 to 100")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	events, next = []T{}, after
	if len(h.seqs) > 0 && after < h.seqs[0]-1 {
		truncated = true
	}
	for i, seq := range h.seqs {
		if seq > after {
			if len(events) == limit {
				truncated = true
				break
			}
			events = append(events, h.items[i])
			next = seq
		}
	}
	return events, next, truncated, nil
}

// PageInput is the paging input of the bridge's status tools.
type PageInput struct {
	AfterSeq uint64 `json:"after_seq,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// Peer holds the connection a bridge-side component was attached to, for its status tools.
type Peer struct {
	mu sync.Mutex
	c  *Conn
}

// Attach records the session connection.
func (p *Peer) Attach(c *Conn) {
	p.mu.Lock()
	p.c = c
	p.mu.Unlock()
}

// Conn returns the attached connection, or nil before the session is ready.
func (p *Peer) Conn() *Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.c
}

// State reports "ready" and the open stream count while the session is up, else "disconnected".
func (p *Peer) State() (state string, active int) {
	if c := p.Conn(); c != nil {
		select {
		case <-c.Done():
		default:
			return "ready", c.Active()
		}
	}
	return "disconnected", 0
}

// CloseWrite half-closes c when it supports that (TCP connections and channel streams) and
// reports whether it did.
func CloseWrite(c net.Conn) bool {
	cw, ok := c.(interface{ CloseWrite() error })
	if ok {
		cw.CloseWrite()
	}
	return ok
}
