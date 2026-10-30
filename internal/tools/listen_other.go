//go:build !linux && !windows

package tools

import (
	"context"
	"errors"
)

func listeners(context.Context) ([]Listener, string, error) {
	return nil, "", errors.New("net_listen is supported on Linux and Windows; use exec_run with lsof -iTCP -sTCP:LISTEN -nP or netstat")
}
