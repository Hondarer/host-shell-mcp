package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// prompter asks the user in the terminal of a foreground session whether a tool may run.
//
// The answer must come from the user. host-shell call is run by the agent itself, so the
// session never takes the answer from its clients (see bridge.Local).
type prompter struct {
	in   io.Reader
	out  io.Writer
	name string

	mu    sync.Mutex // one question at a time
	once  sync.Once
	lines chan string
}

func newPrompter(name string, in io.Reader, out io.Writer) *prompter {
	return &prompter{in: in, out: out, name: name}
}

func (p *prompter) start() {
	p.lines = make(chan string)
	go func() {
		sc := bufio.NewScanner(p.in)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
	}()
}

// confirm shows the target's question and waits for y or n. Anything but y or yes declines;
// a cancelled call withdraws its question.
func (p *prompter) confirm(ctx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.once.Do(p.start)
	// Discard lines typed while no question was shown.
	for drained := false; !drained; {
		select {
		case _, ok := <-p.lines:
			if !ok {
				return &mcp.ElicitResult{Action: "decline"}, nil
			}
		default:
			drained = true
		}
	}
	fmt.Fprintf(p.out, "\n[host-shell session %s] %s\nAllow? [y/N]: ", p.name, params.Message)
	select {
	case line, ok := <-p.lines:
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			if ok {
				return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": true}}, nil
			}
		}
		fmt.Fprintln(p.out, "Declined.")
		return &mcp.ElicitResult{Action: "decline"}, nil
	case <-ctx.Done():
		fmt.Fprintln(p.out, "(withdrawn: the call was cancelled)")
		return nil, ctx.Err()
	}
}
