package forward

import (
	"io"
	"net"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// SOCKS5 (RFC 1928) reply codes.
const (
	socksSucceeded        = 0x00
	socksFailure          = 0x01
	socksNotAllowed       = 0x02
	socksHostUnreachable  = 0x04
	socksRefused          = 0x05
	socksTTLExpired       = 0x06
	socksCommandUnsupport = 0x07
	socksAddressUnsupport = 0x08
)

func socksReply(code string) byte {
	switch code {
	case channel.CodeNotAllowed:
		return socksNotAllowed
	case channel.CodeUnreachable:
		return socksHostUnreachable
	case channel.CodeRefused:
		return socksRefused
	case channel.CodeTimeout:
		return socksTTLExpired
	}
	return socksFailure
}

// socks serves one SOCKS5 CONNECT without authentication; the target resolves domain names.
func (m *Manager) socks(f *forward, id uint64, c net.Conn) {
	c.SetDeadline(time.Now().Add(m.timeout))
	unsupported := func(host string, port int) {
		m.record(f, id, host, port, "denied", "unsupported SOCKS request")
	}
	reply := func(code byte) error {
		c.SetWriteDeadline(time.Now().Add(m.timeout))
		_, err := c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
		return err
	}
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil {
		return
	}
	switch head[0] {
	case 5:
	case 4:
		// SOCKS4 and 4a: "request rejected or failed".
		c.Write([]byte{0, 0x5b, 0, 0, 0, 0, 0, 0})
		unsupported("", 0)
		return
	default:
		unsupported("", 0)
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	noAuth := false
	for _, b := range methods {
		noAuth = noAuth || b == 0
	}
	if !noAuth {
		c.Write([]byte{5, 0xff})
		unsupported("", 0)
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != 5 {
		return
	}
	var host string
	switch req[3] {
	case 1, 4:
		ip := make(net.IP, 4)
		if req[3] == 4 {
			ip = make(net.IP, 16)
		}
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = ip.String()
	case 3:
		n := make([]byte, 1)
		if _, err := io.ReadFull(c, n); err != nil {
			return
		}
		name := make([]byte, n[0])
		if _, err := io.ReadFull(c, name); err != nil {
			return
		}
		host = string(name)
	default:
		reply(socksAddressUnsupport)
		unsupported("", 0)
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return
	}
	port := int(pb[0])<<8 | int(pb[1])
	if req[1] != 1 {
		reply(socksCommandUnsupport)
		unsupported(host, port)
		return
	}
	if !validHost(host) || port == 0 {
		reply(socksFailure)
		unsupported(host, port)
		return
	}
	if !m.allowLocal.Allow(host, port) {
		reply(socksNotAllowed)
		m.record(f, id, host, port, "denied", "destination not allowed")
		return
	}
	// The handshake deadline must not cut the connect wait short, or a timeout could not be replied.
	c.SetDeadline(time.Time{})
	remote, err := m.open(f, channel.KindSOCKS, host, port)
	if err != nil {
		code := channel.Code(err)
		reply(socksReply(code))
		m.record(f, id, host, port, status(code), reason(code))
		return
	}
	if !f.track(remote) {
		remote.Close()
		reply(socksFailure)
		return
	}
	defer f.untrack(remote)
	if reply(socksSucceeded) != nil {
		remote.Close()
		return
	}
	c.SetDeadline(time.Time{})
	m.record(f, id, host, port, "allowed", "connection established")
	pipe(c, remote)
}
