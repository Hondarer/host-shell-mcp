//go:build !windows

package remotedns

import (
	"context"
	"fmt"
	"net/netip"
	"runtime"
)

func configureResolver(context.Context, netip.Addr) (func(), []string, error) {
	return nil, nil, fmt.Errorf("--remote-dns-configure is supported on Windows targets only (this is %s)", runtime.GOOS)
}
