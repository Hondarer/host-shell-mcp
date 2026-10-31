package clientproxy

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// limitedListener also bounds unauthenticated and idle HTTP connections.
type limitedListener struct {
	net.Listener
	active atomic.Int32
	reject func()
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		if l.active.Add(1) > maxStreams {
			l.active.Add(-1)
			c.SetWriteDeadline(time.Now().Add(time.Second))
			io.WriteString(c, "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			c.Close()
			if l.reject != nil {
				l.reject()
			}
			continue
		}
		return &countedConn{Conn: c, release: func() { l.active.Add(-1) }}, nil
	}
}

type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *countedConn) Close() error { e := c.Conn.Close(); c.once.Do(c.release); return e }
