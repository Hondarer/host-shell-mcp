package channel

import (
	"sync"
	"time"
)

// Parts shared by the bridge's status tools (proxy_status, forward_status) and by the audit
// records of the features that ride on the channel.

// Results recorded for a connection attempt in diagnostics and audit logs.
const (
	Allowed = "allowed"
	Denied  = "denied"
	Failed  = "failed"
)

// Fixed reasons shared by port forwarding and the client proxy. Reasons never carry raw network
// errors, which could contain credentials.
const (
	ReasonEstablished = "connection established"
	ReasonNotAllowed  = "destination not allowed"
	ReasonLimit       = "connection limit reached"
)

// Describe maps a failure code to the result and the fixed reason recorded for it.
func Describe(code string) (result, reason string) {
	switch code {
	case CodeNotAllowed:
		return Denied, ReasonNotAllowed
	case CodeLimit:
		return Denied, ReasonLimit
	case CodeClosed:
		return Denied, "forward closed"
	case CodeRefused:
		return Failed, "connection refused"
	case CodeUnreachable:
		return Failed, "host unreachable"
	case CodeTimeout:
		return Failed, "connection timeout"
	}
	return Failed, "connection failed"
}

// EventHead starts every status event with its sequence number and time.
type EventHead struct {
	Seq  uint64 `json:"seq"`
	Time string `json:"time"`
}

// NewEventHead stamps the event numbered seq with the current time.
func NewEventHead(seq uint64) EventHead {
	return EventHead{Seq: seq, Time: time.Now().Format(time.RFC3339)}
}

// Outcome is the destination and result of one connection attempt (see Describe).
type Outcome struct {
	Host   string `json:"host,omitempty"`
	Port   int    `json:"port,omitempty"`
	Status string `json:"status" jsonschema:"allowed, denied or failed"`
	Reason string `json:"reason"`
}

// Recent ends a bridge status tool's output: open streams and a page of recent events.
type Recent[T any] struct {
	ActiveConnections int    `json:"active_connections" jsonschema:"open channel streams of the session, all features included"`
	Events            []T    `json:"events"`
	NextSeq           uint64 `json:"next_seq"`
	Truncated         bool   `json:"truncated"`
}

// Snapshot pages h after in.AfterSeq and reports the session state of p.
func Snapshot[T any](h *History[T], p *Peer, in PageInput) (state string, r Recent[T], err error) {
	if r.Events, r.NextSeq, r.Truncated, err = h.Page(in.AfterSeq, in.Limit); err != nil {
		return "", Recent[T]{}, err
	}
	state, r.ActiveConnections = p.State()
	return state, r, nil
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
