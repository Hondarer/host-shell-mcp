package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// progressInterval is the minimum interval between progress notifications.
const progressInterval = time.Second

// progress sends notifications/progress when the client provides a progressToken (no-op when nil).
type progress struct {
	ctx   context.Context
	ss    *mcp.ServerSession
	token any

	mu    sync.Mutex
	last  time.Time
	count float64
}

func newProgress(ctx context.Context, req *mcp.CallToolRequest) *progress {
	if req == nil || req.Params == nil || req.Session == nil {
		return nil
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	return &progress{ctx: ctx, ss: req.Session, token: token}
}

// report sends a progress notification, rate-limited to progressInterval unless force is true.
// If value <= 0, the notification count is used as the progress value.
func (p *progress) report(value, total float64, msg string, force bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	now := time.Now()
	if !force && now.Sub(p.last) < progressInterval {
		p.mu.Unlock()
		return
	}
	p.last = now
	p.count++
	if value <= 0 {
		value = p.count
	}
	p.mu.Unlock()
	_ = p.ss.NotifyProgress(p.ctx, &mcp.ProgressNotificationParams{
		ProgressToken: p.token, Progress: value, Total: total, Message: msg,
	})
}

// outputProgress periodically reports the amount of output and the last line of a running command as progress.
type outputProgress struct {
	p     *progress
	start time.Time

	mu    sync.Mutex
	bytes int64
	tail  []byte
	stop  chan struct{}
}

// watchOutput starts periodic notifications if p is not nil. Feed output to the returned tap and call stop when done.
func watchOutput(p *progress) (tap func([]byte), stop func()) {
	if p == nil {
		return nil, func() {}
	}
	o := &outputProgress{p: p, start: time.Now(), stop: make(chan struct{})}
	go o.loop()
	var once sync.Once
	return o.write, func() { once.Do(func() { close(o.stop) }) }
}

func (o *outputProgress) write(b []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.bytes += int64(len(b))
	o.tail = append(o.tail, b...)
	if len(o.tail) > 4096 {
		o.tail = o.tail[len(o.tail)-4096:]
	}
}

func (o *outputProgress) loop() {
	t := time.NewTicker(progressInterval)
	defer t.Stop()
	for {
		select {
		case <-o.stop:
			return
		case <-t.C:
			o.mu.Lock()
			msg := fmt.Sprintf("running %s, %d bytes of output", time.Since(o.start).Round(time.Second), o.bytes)
			if l := lastLine(o.tail); l != "" {
				msg += "; last line: " + l
			}
			o.mu.Unlock()
			o.p.report(time.Since(o.start).Seconds(), 0, msg, true)
		}
	}
}

// lastLine returns the last non-empty line of the output, shortened for display.
func lastLine(b []byte) string {
	s := strings.ToValidUTF8(string(b), "?")
	s, _ = stripTerminal(s)
	lines := strings.Split(strings.TrimRight(s, "\r\n\t "), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if utf8.RuneCountInString(l) > 200 {
		l = string([]rune(l)[:200]) + "..."
	}
	return l
}
