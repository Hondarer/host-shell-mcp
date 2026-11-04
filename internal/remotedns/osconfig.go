package remotedns

import (
	"fmt"
	"net/netip"
)

// ruleComment marks the resolver rules host-shell manages; the manual procedure in the documentation uses it too.
const ruleComment = "host-shell-remote-dns"

// resolverAddress returns the IP the OS resolver must query for a loopback DNS server on addr. OS resolver
// rules (Windows NRPT) name servers without a port, so the server must listen on port 53.
func resolverAddress(addr string) (netip.Addr, error) {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil || !ap.Addr().IsLoopback() || ap.Port() != 53 {
		return netip.Addr{}, fmt.Errorf("--remote-dns-configure needs --remote-dns-listen on a loopback address with port 53 (got %q)", addr)
	}
	return ap.Addr().Unmap(), nil
}
