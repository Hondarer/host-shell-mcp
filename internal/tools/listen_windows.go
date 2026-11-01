package tools

import (
	"context"
	"net"
	"strconv"
	"strings"
)

// listeners parses listening sockets from netstat -ano and resolves process names via tasklist.
func (d *Deps) listeners(ctx context.Context) ([]Listener, string, error) {
	res, err := d.runTool(ctx, []string{"netstat", "-ano"}, false)
	if err != nil {
		return nil, "", err
	}
	names := d.processNames(ctx)
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
func (d *Deps) processNames(ctx context.Context) map[int]string {
	res, err := d.runTool(ctx, []string{"tasklist", "/fo", "csv", "/nh"}, false)
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
