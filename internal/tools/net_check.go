package tools

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- net_check ----

// NetCheckInput is the input of net_check.
type NetCheckInput struct {
	Targets   []string `json:"targets" jsonschema:"targets to check: host:port (DNS + TCP), host (DNS only), or URL (https implies tls); IPv6 as [::1]:443"`
	TLS       bool     `json:"tls,omitempty" jsonschema:"perform a TLS handshake on each port and report certificate details"`
	TimeoutMs int      `json:"timeout_ms,omitempty" jsonschema:"per-target timeout in ms (default 5000)"`
}

// NetCheckResult is the result for one destination.
type NetCheckResult struct {
	Target    string   `json:"target"`
	Host      string   `json:"host"`
	Port      int      `json:"port,omitempty"`
	Addresses []string `json:"addresses,omitempty" jsonschema:"resolved IP addresses"`
	DNSMs     int64    `json:"dns_ms"`
	Connected *bool    `json:"connected,omitempty" jsonschema:"TCP connection succeeded (absent for DNS-only targets)"`
	ConnectMs int64    `json:"connect_ms,omitempty"`
	TLS       *TLSInfo `json:"tls,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// NetCheckOutput is the output of net_check.
type NetCheckOutput struct {
	Results []NetCheckResult `json:"results"`
}

func (d *Deps) netCheck(ctx context.Context, _ *mcp.CallToolRequest, in NetCheckInput) (*mcp.CallToolResult, NetCheckOutput, error) {
	if len(in.Targets) == 0 {
		return nil, NetCheckOutput{}, errors.New("targets is required")
	}
	if len(in.Targets) > 100 {
		return nil, NetCheckOutput{}, errors.New("at most 100 targets")
	}
	timeout := time.Duration(orDefaultInt(in.TimeoutMs, 5000)) * time.Millisecond
	out := NetCheckOutput{Results: make([]NetCheckResult, len(in.Targets))}
	done := make(chan struct{})
	for i, t := range in.Targets {
		go func() {
			out.Results[i] = d.checkTarget(ctx, t, in.TLS, timeout)
			done <- struct{}{}
		}()
	}
	for range in.Targets {
		<-done
	}
	return nil, out, nil
}

func (d *Deps) checkTarget(ctx context.Context, target string, useTLS bool, timeout time.Duration) NetCheckResult {
	r := NetCheckResult{Target: target}
	host, port, isTLS, err := parseTarget(target)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Host, r.Port = host, port
	useTLS = useTLS || isTLS
	if err := d.Policy.CheckHost(host, port); err != nil {
		r.Error = err.Error()
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	r.DNSMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Addresses = addrs
	if port == 0 {
		return r
	}
	start = time.Now()
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	r.ConnectMs = time.Since(start).Milliseconds()
	ok := err == nil
	r.Connected = &ok
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer conn.Close()
	if useTLS {
		tc := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: host})
		if err := tc.HandshakeContext(ctx); err != nil {
			r.Error = "tls handshake: " + err.Error()
			return r
		}
		st := tc.ConnectionState()
		r.TLS = tlsInfo(&st, host, nil)
	}
	return r
}

// parseTarget splits a net_check destination into host, port (0 if absent) and whether to use TLS.
func parseTarget(t string) (host string, port int, isTLS bool, err error) {
	if strings.Contains(t, "://") {
		u, err := url.Parse(t)
		if err != nil {
			return "", 0, false, err
		}
		host = u.Hostname()
		ps := u.Port()
		switch {
		case ps != "":
		case u.Scheme == "https":
			ps = "443"
		case u.Scheme == "http":
			ps = "80"
		default:
			if p, err := net.LookupPort("tcp", u.Scheme); err == nil {
				ps = strconv.Itoa(p)
			}
		}
		port, _ = strconv.Atoi(ps)
		return host, port, u.Scheme == "https", nil
	}
	if h, p, err := net.SplitHostPort(t); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil {
			if n, err = net.LookupPort("tcp", p); err != nil {
				return "", 0, false, fmt.Errorf("bad port %q", p)
			}
		}
		return h, n, false, nil
	}
	return strings.Trim(t, "[]"), 0, false, nil
}
