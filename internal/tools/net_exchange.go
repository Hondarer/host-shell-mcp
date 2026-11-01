package tools

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// ---- net_exchange ----

// NetExchangeInput is the input of net_exchange.
type NetExchangeInput struct {
	Host            string `json:"host,omitempty" jsonschema:"host name or IP address (default localhost)"`
	Port            int    `json:"port,omitempty" jsonschema:"TCP port (required unless unix_socket is set)"`
	UnixSocket      string `json:"unix_socket,omitempty" jsonschema:"Unix domain socket path to connect to instead of host:port"`
	TLS             bool   `json:"tls,omitempty" jsonschema:"use TLS"`
	Insecure        bool   `json:"insecure,omitempty" jsonschema:"with tls: skip certificate verification"`
	Send            string `json:"send,omitempty" jsonschema:"data to send after connecting, e.g. \"PING\\r\\n\""`
	SendEncoding    string `json:"send_encoding,omitempty" jsonschema:"text (default) or base64"`
	CloseWrite      bool   `json:"close_write,omitempty" jsonschema:"half-close the write side after sending"`
	Until           string `json:"until,omitempty" jsonschema:"stop reading once received data matches this regex"`
	IdleMs          int    `json:"idle_ms,omitempty" jsonschema:"stop reading after this many ms of silence (default 1000)"`
	TimeoutSec      int    `json:"timeout_sec,omitempty" jsonschema:"overall timeout in seconds (default 10)"`
	MaxBytes        int    `json:"max_bytes,omitempty" jsonschema:"max bytes to receive (default and cap: server output limit)"`
	ReceiveEncoding string `json:"receive_encoding,omitempty" jsonschema:"auto (default: text if valid UTF-8, else base64), text or base64"`
}

// AuditArgs redacts Send for audit logging.
func (in NetExchangeInput) AuditArgs() any {
	in.Send = redactedLen(in.Send)
	return in
}

// NetExchangeOutput is the output of net_exchange.
type NetExchangeOutput struct {
	RemoteAddr      string   `json:"remote_addr"`
	Sent            int      `json:"sent_bytes"`
	Received        string   `json:"received"`
	ReceiveEncoding string   `json:"receive_encoding"`
	ReceivedBytes   int      `json:"received_bytes"`
	Matched         bool     `json:"matched,omitempty" jsonschema:"until matched"`
	ClosedByPeer    bool     `json:"closed_by_peer,omitempty"`
	Truncated       bool     `json:"truncated,omitempty" jsonschema:"stopped at max_bytes"`
	TLS             *TLSInfo `json:"tls,omitempty"`
	DurationMs      int64    `json:"duration_ms"`
}

func (d *Deps) netExchange(ctx context.Context, _ *mcp.CallToolRequest, in NetExchangeInput) (*mcp.CallToolResult, NetExchangeOutput, error) {
	if d.Policy.ReadOnly && in.Send != "" {
		return nil, NetExchangeOutput{}, errors.New("policy: read-only mode does not allow sending data (only reading what the server sends first)")
	}
	until, err := compileUntil(in.Until)
	if err != nil {
		return nil, NetExchangeOutput{}, err
	}
	send, err := decodeData("send", in.SendEncoding, in.Send)
	if err != nil {
		return nil, NetExchangeOutput{}, err
	}

	timeout := time.Duration(orDefaultInt(in.TimeoutSec, 10)) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	network, addr, host := "tcp", "", cmp.Or(in.Host, "localhost")
	if in.UnixSocket != "" {
		network, addr = "unix", in.UnixSocket
		if err := d.Policy.CheckUnixSocket(addr); err != nil {
			return nil, NetExchangeOutput{}, err
		}
	} else {
		if in.Port <= 0 || in.Port > 65535 {
			return nil, NetExchangeOutput{}, errors.New("port is required (1-65535) unless unix_socket")
		}
		if err := d.Policy.CheckHost(host, in.Port); err != nil {
			return nil, NetExchangeOutput{}, err
		}
		addr = net.JoinHostPort(host, strconv.Itoa(in.Port))
	}

	start := time.Now()
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, network, addr)
	if err != nil {
		return nil, NetExchangeOutput{}, err
	}
	defer conn.Close()
	out := NetExchangeOutput{RemoteAddr: conn.RemoteAddr().String()}
	if in.TLS {
		tc, info, err := handshakeTLS(ctx, conn, host, in.Insecure)
		if err != nil {
			return nil, NetExchangeOutput{}, err
		}
		out.TLS = info
		conn = tc
	}
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })
	defer stop()
	if len(send) > 0 {
		n, err := conn.Write(send)
		out.Sent = n
		if err != nil {
			return nil, NetExchangeOutput{}, fmt.Errorf("send: %w", err)
		}
	}
	if in.CloseWrite {
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
	}

	max := clampMax(d.base64Budget(), in.MaxBytes)
	idle := time.Duration(orDefaultInt(in.IdleMs, 1000)) * time.Millisecond
	var recv []byte
	buf := make([]byte, 32<<10)
	for len(recv) < max {
		conn.SetReadDeadline(time.Now().Add(idle))
		if ctx.Err() != nil {
			break
		}
		n, err := conn.Read(buf[:min(len(buf), max-len(recv))])
		recv = append(recv, buf[:n]...)
		if until != nil && until.Match(recv) {
			out.Matched = true
			break
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				out.ClosedByPeer = true
			}
			break // idle timeout or overall timeout
		}
	}
	out.Truncated = len(recv) >= max
	out.ReceivedBytes = len(recv)
	enc := cmp.Or(in.ReceiveEncoding, "auto")
	if enc == "auto" {
		enc = "base64"
		if textenc.ValidTruncated(recv) {
			enc = "text"
		}
	}
	switch enc {
	case "text":
		out.Received = strings.ToValidUTF8(string(recv), "�")
	case "base64":
		out.Received = base64.StdEncoding.EncodeToString(recv)
	default:
		return nil, NetExchangeOutput{}, fmt.Errorf("unknown receive_encoding %q", in.ReceiveEncoding)
	}
	out.ReceiveEncoding = enc
	out.DurationMs = time.Since(start).Milliseconds()
	return nil, out, nil
}
