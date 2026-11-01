package channel

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Prefix is the method prefix of channel notifications.
const Prefix = "host-shell/channel/"

// legacyPrefix identifies peers built before the channel was shared by all features.
const legacyPrefix = "host-shell/proxy/"

const version = 1
const frameSize = 16 * 1024
const windowSize = 64 * 1024
const negotiationTimeout = 30 * time.Second

// MaxStreams bounds concurrent streams per session across all features, pending opens included.
const MaxStreams = 64

// MaxForwards bounds listeners per session, persistent and tool-created alike.
const MaxForwards = 16

// Features negotiated in hello.
const (
	HTTPProxy     = "http_proxy"
	ForwardLocal  = "forward_local"
	ForwardRemote = "forward_remote"
)

// Stream kinds carried by open.
const (
	KindHTTP    = "http"    // target to bridge: HTTP proxy request
	KindConnect = "connect" // target to bridge: HTTP proxy CONNECT
	KindForward = "forward" // bridge to target: local forward; target to bridge: remote forward
	KindSOCKS   = "socks"   // bridge to target: SOCKS5 CONNECT
)

// Fixed failure codes in open_result and listen_result. Raw network errors never cross the channel.
const (
	CodeNotAllowed  = "not_allowed"
	CodeLimit       = "limit"
	CodeRefused     = "refused"
	CodeUnreachable = "unreachable"
	CodeTimeout     = "timeout"
	CodeFailed      = "failed"
	CodeClosed      = "closed"
	CodeInUse       = "address_in_use"
	CodePermission  = "permission_denied"
)

// Error is a failure reported with a fixed code.
type Error struct{ Code string }

func (e *Error) Error() string { return strings.ReplaceAll(e.Code, "_", " ") }

// ErrLimit is returned by Dial when this side has no stream left; the peer was not asked.
var ErrLimit = &Error{Code: CodeLimit}

// Code returns the fixed code of err.
func Code(err error) string {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout
	}
	return CodeFailed
}

// Frame is the payload of a channel notification.
type Frame struct {
	ID       uint64   `json:"id,omitempty"`
	Version  int      `json:"version,omitempty"`
	Features []string `json:"features,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Forward  uint64   `json:"forward,omitempty"`
	Bind     string   `json:"bind,omitempty"`
	Data     string   `json:"data,omitempty"`
	Bytes    int      `json:"bytes,omitempty"`
	Error    string   `json:"error,omitempty"`
}
