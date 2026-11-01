package forward

import (
	"io"
	"net"
	"slices"
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

// A rejected SOCKS connection is kept for at most this long, reading at most this much,
// so that its unread request bytes can be drained before it closes (see lingerClose).
const (
	socksLingerTimeout = time.Second
	socksLingerLimit   = 64 << 10
)

// lingerClose closes c after a rejection reply. Closing a TCP connection with unread input makes
// the OS send RST instead of FIN, and Windows then discards the reply before the client reads it.
// So the write side is closed first (the client sees the reply and then EOF), and the remaining
// input is drained until the client closes, socksLingerTimeout passes or socksLingerLimit bytes
// are read.
func lingerClose(c net.Conn) {
	channel.CloseWrite(c)
	c.SetReadDeadline(time.Now().Add(socksLingerTimeout))
	io.Copy(io.Discard, io.LimitReader(c, socksLingerLimit))
	c.Close()
}

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

// socksOutcome is how a SOCKS handshake ended.
type socksOutcome int

const (
	socksReady    socksOutcome = iota // a valid CONNECT request was read
	socksGone                         // the client went away or broke the protocol; close silently
	socksRejected                     // a rejection reply was written; record it and linger
	socksIgnored                      // not SOCKS5 or 4; record it and close without a reply
)

// socks serves one SOCKS5 CONNECT without authentication; the target resolves domain names.
// Every path that writes a rejection reply ends with lingerClose so that the reply arrives.
func (m *Manager) socks(f *forward, id uint64, c net.Conn) {
	c.SetDeadline(time.Now().Add(m.timeout))
	reply := func(code byte) error {
		c.SetWriteDeadline(time.Now().Add(m.timeout))
		_, err := c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
		return err
	}
	host, port, outcome := socksHandshake(c, reply)
	switch outcome {
	case socksGone:
		return
	case socksIgnored, socksRejected:
		m.record(f, id, host, port, "denied", "unsupported SOCKS request")
		if outcome == socksRejected {
			lingerClose(c)
		}
		return
	}
	if !m.allowLocal.Allow(host, port) {
		reply(socksNotAllowed)
		m.record(f, id, host, port, "denied", "destination not allowed")
		lingerClose(c)
		return
	}
	// The handshake deadline must not cut the connect wait short, or a timeout could not be replied.
	c.SetDeadline(time.Time{})
	remote, err := m.open(f, channel.KindSOCKS, host, port)
	if err != nil {
		code := channel.Code(err)
		reply(socksReply(code))
		m.record(f, id, host, port, status(code), reason(code))
		lingerClose(c)
		return
	}
	if !f.track(remote) {
		remote.Close()
		reply(socksFailure)
		lingerClose(c)
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

// socksHandshake negotiates no authentication and reads a CONNECT request (RFC 1928). It writes
// the rejection replies of the handshake itself; host and port are set once the request is read.
func socksHandshake(c net.Conn, reply func(byte) error) (host string, port int, outcome socksOutcome) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil {
		return "", 0, socksGone
	}
	switch head[0] {
	case 5:
	case 4:
		// SOCKS4 and 4a: "request rejected or failed".
		c.Write([]byte{0, 0x5b, 0, 0, 0, 0, 0, 0})
		return "", 0, socksRejected
	default:
		return "", 0, socksIgnored
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return "", 0, socksGone
	}
	if !slices.Contains(methods, 0) {
		c.Write([]byte{5, 0xff})
		return "", 0, socksRejected
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return "", 0, socksGone
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != 5 {
		return "", 0, socksGone
	}
	host, ok := readSOCKSHost(c, req[3])
	if !ok {
		return "", 0, socksGone
	}
	if host == "" && req[3] != 3 {
		reply(socksAddressUnsupport)
		return "", 0, socksRejected
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return "", 0, socksGone
	}
	port = int(pb[0])<<8 | int(pb[1])
	if req[1] != 1 {
		reply(socksCommandUnsupport)
		return host, port, socksRejected
	}
	if !validHost(host) || port == 0 {
		reply(socksFailure)
		return host, port, socksRejected
	}
	return host, port, socksReady
}

// readSOCKSHost reads the destination address of type atyp. It returns "" with ok true for an
// unsupported address type, and ok false when the client went away.
func readSOCKSHost(c net.Conn, atyp byte) (host string, ok bool) {
	switch atyp {
	case 1, 4:
		ip := make(net.IP, 4)
		if atyp == 4 {
			ip = make(net.IP, 16)
		}
		if _, err := io.ReadFull(c, ip); err != nil {
			return "", false
		}
		return ip.String(), true
	case 3:
		n := make([]byte, 1)
		if _, err := io.ReadFull(c, n); err != nil {
			return "", false
		}
		name := make([]byte, n[0])
		if _, err := io.ReadFull(c, name); err != nil {
			return "", false
		}
		return string(name), true
	}
	return "", true
}
