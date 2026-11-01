// Package forward implements TCP port forwarding over the client channel.
//
// Local forwards and SOCKS5 listen on the bridge and connect from the target; remote forwards
// listen on the target and connect from the bridge directly, without upstream proxies. Both
// sides listen on loopback only. The bridge requests directions with its forward options and
// the target permits them with --allow-forward-local and --allow-forward-remote; the channel
// negotiation rejects a request the target does not permit. The target applies --allow-host
// to the destinations it connects to and records listeners and connections in its audit log.
package forward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// Directions.
const (
	Local  = "local"
	Remote = "remote"
	SOCKS  = "socks"
)

// Spec is a forward definition: Bind:Port listens and Host:HostPort is the destination (empty for SOCKS).
type Spec struct {
	Direction string
	Bind      string
	Port      int
	Host      string
	HostPort  int
}

// ParseSpec parses [BIND:]PORT:HOST:HOSTPORT for local and remote forwards, or [BIND:]PORT for SOCKS.
// BIND is 127.0.0.1 (default) or [::1]; an IPv6 HOST is bracketed.
func ParseSpec(direction, v string) (Spec, error) {
	fields, err := splitFields(v)
	bad := func(reason string) (Spec, error) {
		return Spec{}, fmt.Errorf("invalid --forward-%s %q: %s", direction, v, reason)
	}
	if err != nil {
		return bad(err.Error())
	}
	want := 3
	if direction == SOCKS {
		want = 1
	}
	if len(fields) != want && len(fields) != want+1 {
		if direction == SOCKS {
			return bad("want [BIND:]PORT")
		}
		return bad("want [BIND:]PORT:HOST:HOSTPORT")
	}
	s := Spec{Direction: direction, Bind: "127.0.0.1"}
	if len(fields) == want+1 {
		if s.Bind, err = listenAddress(fields[0]); err != nil {
			return bad(err.Error())
		}
		fields = fields[1:]
	}
	if s.Port, err = parsePort(fields[0], true); err != nil {
		return bad(err.Error())
	}
	if direction == SOCKS {
		return s, nil
	}
	if s.Host, err = parseHost(fields[1]); err != nil {
		return bad(err.Error())
	}
	if s.HostPort, err = parsePort(fields[2], false); err != nil {
		return bad(err.Error())
	}
	return s, nil
}

// splitFields splits on colons, keeping bracketed IPv6 addresses whole.
func splitFields(v string) ([]string, error) {
	var out []string
	for {
		if strings.HasPrefix(v, "[") {
			i := strings.IndexByte(v, ']')
			if i < 0 {
				return nil, errors.New("unclosed [")
			}
			out = append(out, v[:i+1])
			v = v[i+1:]
			if v == "" {
				return out, nil
			}
			if v[0] != ':' {
				return nil, errors.New("expected : after ]")
			}
			v = v[1:]
			continue
		}
		f, rest, found := strings.Cut(v, ":")
		out = append(out, f)
		if !found {
			return out, nil
		}
		v = rest
	}
}

// listenAddress accepts the loopback addresses forwards may listen on.
func listenAddress(v string) (string, error) {
	switch v {
	case "", "127.0.0.1":
		return "127.0.0.1", nil
	case "::1", "[::1]":
		return "::1", nil
	}
	return "", fmt.Errorf("listen address %q is not 127.0.0.1 or [::1]", v)
}

func parsePort(v string, listen bool) (int, error) {
	p, err := strconv.Atoi(v)
	low := 1
	if listen {
		low = 0
	}
	if err != nil || p < low || p > 65535 {
		return 0, fmt.Errorf("invalid port %q", v)
	}
	return p, nil
}

func parseHost(v string) (string, error) {
	if strings.HasPrefix(v, "[") {
		h := strings.Trim(v, "[]")
		if ip := net.ParseIP(h); ip == nil || ip.To4() != nil {
			return "", fmt.Errorf("invalid IPv6 host %q", v)
		}
		return h, nil
	}
	if !validHost(v) || strings.Contains(v, ":") {
		return "", fmt.Errorf("invalid host %q", v)
	}
	return v, nil
}

func validHost(h string) bool {
	return h != "" && len(h) <= 253 && !strings.ContainsAny(h, "/\\\r\n\t @[]")
}

// Address formats a listen address for messages and status.
func (s Spec) Address() string { return net.JoinHostPort(s.Bind, strconv.Itoa(s.Port)) }

func (s Spec) destination() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.HostPort)) }

// dialCode classifies a dial failure into a fixed channel code.
func dialCode(err error) string {
	var dns *net.DNSError
	var errno syscall.Errno
	switch {
	case errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err):
		return channel.CodeTimeout
	case errors.As(err, &dns):
		return channel.CodeUnreachable
	case errors.As(err, &errno):
		// 10061, 10051 and 10065 are the Windows WSAECONNREFUSED, WSAENETUNREACH and WSAEHOSTUNREACH.
		switch errno {
		case syscall.ECONNREFUSED, 10061:
			return channel.CodeRefused
		case syscall.ENETUNREACH, syscall.EHOSTUNREACH, 10051, 10065:
			return channel.CodeUnreachable
		}
	}
	return channel.CodeFailed
}

// listenCode classifies a listen failure into a fixed channel code.
func listenCode(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// 10048 and 10013 are the Windows WSAEADDRINUSE and WSAEACCES.
		switch errno {
		case syscall.EADDRINUSE, 10048:
			return channel.CodeInUse
		case syscall.EACCES, 10013:
			return channel.CodePermission
		}
	}
	return channel.CodeFailed
}

// openFailure describes the failure of a stream the bridge opened to the target. not_allowed then
// comes from the target's --allow-host, which the reason says to tell it from the bridge's rules.
func openFailure(code string) (result, reason string) {
	result, reason = channel.Describe(code)
	if code == channel.CodeNotAllowed {
		reason += " by target"
	}
	return result, reason
}

// dialFailure describes a failed dial; its codes never deny.
func dialFailure(code string) string {
	_, reason := channel.Describe(code)
	return reason
}

// pipe relays a forwarded connection until both directions end. Forwarded connections have no
// idle timeout; closing either connection ends both.
func pipe(a, b net.Conn) { channel.Relay(a, b, channel.RelayOptions{}) }
