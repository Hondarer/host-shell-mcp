// Package remotedns resolves target getaddrinfo requests on the bridge's network.
package remotedns

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

const timeout = 3 * time.Second

// RegisterGateway enables DNS resolution on the connecting machine. This does not
// grant outbound TCP access; HTTP proxy destination rules still apply separately.
func RegisterGateway(opt *channel.Options) {
	opt.Enable(channel.RemoteDNS)
	opt.Handle(channel.KindDNS, func(ctx context.Context, in *channel.Incoming) {
		if !channel.ValidDestination(in.Host, in.Port) || in.Port != 53 || in.Forward != 0 {
			in.Reject(channel.CodeNotAllowed)
			return
		}
		c, err := in.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(timeout))
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		serve(ctx, c, in.Host, net.DefaultResolver)
	})
}

type resolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
	LookupCNAME(context.Context, string) (string, error)
}

func serve(ctx context.Context, c net.Conn, host string, r resolver) {
	line, err := bufio.NewReader(io.LimitReader(c, 8)).ReadString('\n')
	if err != nil {
		return
	}
	network := map[string]string{"0\n": "ip", "4\n": "ip4", "6\n": "ip6"}[line]
	if network == "" {
		io.WriteString(c, "ERR FAMILY\n")
		return
	}
	ips, err := r.LookupIP(ctx, network, host)
	if err != nil {
		code := "AGAIN"
		if e, ok := err.(*net.DNSError); ok && e.IsNotFound {
			code = "NONAME"
		}
		fmt.Fprintf(c, "ERR %s\n", code)
		return
	}
	canonical := host
	if cname, err := r.LookupCNAME(ctx, host); err == nil && channel.ValidDestination(cname, 53) {
		canonical = cname
	}
	canonical = strings.TrimSuffix(canonical, ".")
	seen := map[string]bool{}
	lines := []string{}
	for _, ip := range ips {
		if len(lines) == 64 {
			break
		}
		s := ip.String()
		if seen[s] || ip.To16() == nil {
			continue
		}
		seen[s] = true
		family := 6
		if ip.To4() != nil {
			family = 4
		}
		lines = append(lines, fmt.Sprintf("%d %s\n", family, s))
	}
	if len(lines) == 0 {
		io.WriteString(c, "ERR NONAME\n")
		return
	}
	fmt.Fprintf(c, "OK %d %s\n%s", len(lines), canonical, strings.Join(lines, ""))
}
