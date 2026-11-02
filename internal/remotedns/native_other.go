//go:build !linux

package remotedns

import (
	"context"
	"errors"
)

func prepare(context.Context, string, string) (string, error) {
	return "", errors.New("--remote-dns target requires Linux/glibc; the bridge supports all platforms")
}
