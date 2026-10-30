package tools

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// listeners parses listening sockets from netstat -ano and resolves process names via tasklist.
func listeners(ctx context.Context) ([]Listener, string, error) {
	opt := proc.Options{Output: textenc.Choice{Auto: true, Enc: textenc.Console()}, Timeout: 30 * time.Second, MaxOutput: 16 << 20}
	res, err := proc.Run(ctx, proc.Spec{Argv: []string{"netstat", "-ano"}}, opt)
	if err != nil {
		return nil, "", err
	}
	names := processNames(ctx)
	var out []Listener
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		proto := strings.ToLower(f[0])
		var pidField string
		switch {
		case proto == "tcp" && len(f) >= 5 && f[3] == "LISTENING":
			pidField = f[4]
		case proto == "udp":
			pidField = f[len(f)-1]
		default:
			continue
		}
		host, ps, err := net.SplitHostPort(f[1])
		if err != nil {
			continue
		}
		port, _ := strconv.Atoi(ps)
		pid, _ := strconv.Atoi(pidField)
		host = strings.Trim(host, "[]")
		if i := strings.IndexByte(host, '%'); i >= 0 {
			host = host[:i]
		}
		if strings.Contains(host, ":") {
			proto += "6"
		}
		out = append(out, Listener{Protocol: proto, Address: host, Port: port, PID: pid, Process: names[pid]})
	}
	return out, "", nil
}

// processNames maps process IDs to names from tasklist.
func processNames(ctx context.Context) map[int]string {
	opt := proc.Options{Output: textenc.Choice{Auto: true, Enc: textenc.Console()}, Timeout: 30 * time.Second, MaxOutput: 16 << 20}
	res, err := proc.Run(ctx, proc.Spec{Argv: []string{"tasklist", "/fo", "csv", "/nh"}}, opt)
	out := map[int]string{}
	if err != nil {
		return out
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Split(strings.TrimSpace(line), `","`)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.Trim(f[1], `"`))
		if err == nil {
			out[pid] = strings.Trim(f[0], `"`)
		}
	}
	return out
}
