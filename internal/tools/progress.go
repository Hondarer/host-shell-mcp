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

// progressInterval は進み具合の通知の最小の間隔です。
const progressInterval = time.Second

// progress は、クライアントが進み具合の通知 (progressToken) を求めた要求に notifications/progress を送ります。
// 求められていない場合は何もしません。nil でも使えます。
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

// report は進み具合を送ります。前回から progressInterval 経っていない場合は送りません (force を除く)。
// value が 0 以下の場合は、呼ばれた回数を進み具合とします。
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

// outputProgress は、実行中のコマンドの出力の量と最後の行を、一定の間隔で進み具合として送ります。
type outputProgress struct {
	p     *progress
	start time.Time

	mu    sync.Mutex
	bytes int64
	tail  []byte
	stop  chan struct{}
}

// watchOutput は p が nil でなければ定期的な通知を始めます。戻り値の tap に出力を渡し、終わったら stop を呼びます。
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

// lastLine は出力の最後の空でない行を、表示に適した長さで返します。
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
