package channel

import (
	"context"
	"slices"
)

// Options configures one side of the channel. Features are requested on the bridge and permitted on the target.
type Options struct {
	Features []string
	// open handles peer-initiated streams by kind; each call runs in its own goroutine and must Accept or Reject.
	open map[string]func(context.Context, *Incoming)
	// report receives limit and auth_failed notices from the target (bridge only).
	report []func(kind string, f Frame)
	attach []func(*Conn)
	// Listen opens a remote forward listener (target only) and returns the bound port.
	Listen func(c *Conn, forward uint64, bind string, port int) (int, error)
	// Unlisten closes a remote forward listener (target only).
	Unlisten func(forward uint64)
	// Setup runs on the bridge after hello and before ready, for example to listen persistent remote forwards.
	Setup func(ctx context.Context, c *Conn) error
}

// Enable adds a feature.
func (o *Options) Enable(feature string) {
	if !slices.Contains(o.Features, feature) {
		o.Features = append(o.Features, feature)
	}
}

// Handle registers the handler for peer-initiated streams of kind.
func (o *Options) Handle(kind string, fn func(context.Context, *Incoming)) {
	if o.open == nil {
		o.open = map[string]func(context.Context, *Incoming){}
	}
	o.open[kind] = fn
}

// OnReport adds a receiver of target notices.
func (o *Options) OnReport(fn func(kind string, f Frame)) { o.report = append(o.report, fn) }

// OnAttach adds a callback run once the channel is ready.
func (o *Options) OnAttach(fn func(*Conn)) { o.attach = append(o.attach, fn) }
