package remotedns

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/hondarer/host-shell-mcp/internal/channel"
)

// stubTTL keeps answers short-lived in the OS resolver cache, since the bridge's view may change.
const stubTTL = 30

// askFunc resolves host for family ("4" or "6") and returns the bridge's line-protocol reply.
type askFunc func(ctx context.Context, family, host string) (string, error)

// stub is a loopback DNS server (UDP and TCP) that answers A and AAAA queries through the bridge, for
// targets whose resolver cannot be hooked per process (Windows routes queries to it with an NRPT rule).
type stub struct {
	addr string
	udp  net.PacketConn
	tcp  net.Listener
	ask  askFunc
	once sync.Once
	// trace logs each exchange to standard error (HOST_SHELL_DNS_TRACE set on host-shell).
	trace bool
}

func listenStub(addr string, ask askFunc) (*stub, error) {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, fmt.Errorf("--remote-dns-listen %q: use a loopback IP and port such as 127.0.0.1:53", addr)
	}
	if !ap.Addr().IsLoopback() {
		return nil, fmt.Errorf("--remote-dns-listen %q: only loopback addresses are allowed", addr)
	}
	// Port 0 (tests) takes the port UDP received for TCP too. UDP goes first: on Windows, TCP hands out
	// ephemeral ports in sequence, so they tend to fall into one of the UDP ranges Hyper-V reserves,
	// while the UDP allocator skips them. The TCP side may still be taken, so try a few.
	attempts := 1
	if ap.Port() == 0 {
		attempts = 10
	}
	for i := 0; ; i++ {
		udp, err := net.ListenPacket("udp", ap.String())
		if err != nil {
			return nil, fmt.Errorf("remote DNS listen: %w", err)
		}
		bound := netip.AddrPortFrom(ap.Addr(), uint16(udp.LocalAddr().(*net.UDPAddr).Port))
		tcp, err := net.Listen("tcp", bound.String())
		if err == nil {
			return &stub{addr: bound.String(), udp: udp, tcp: tcp, ask: ask, trace: os.Getenv("HOST_SHELL_DNS_TRACE") != ""}, nil
		}
		udp.Close()
		if i+1 >= attempts {
			return nil, fmt.Errorf("remote DNS listen: %w", err)
		}
	}
}

func (s *stub) close() {
	s.once.Do(func() {
		s.udp.Close()
		s.tcp.Close()
	})
}

// serve answers queries until ctx ends or the stub is closed.
func (s *stub) serve(ctx context.Context, wg *sync.WaitGroup) {
	slots := make(chan struct{}, 64)
	stopClose := context.AfterFunc(ctx, s.close)
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer stopClose()
		buf := make([]byte, 1500)
		for {
			n, from, err := s.udp.ReadFrom(buf)
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				continue // drop when busy; the client retries
			}
			query := append([]byte(nil), buf[:n]...)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				if reply := s.answer(ctx, query, 512); reply != nil {
					s.udp.WriteTo(reply, from)
				}
			}()
		}
	}()
	go func() {
		defer wg.Done()
		for {
			c, err := s.tcp.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				c.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				defer c.Close()
				stop := context.AfterFunc(ctx, func() { c.Close() })
				defer stop()
				s.serveTCP(ctx, c)
			}()
		}
	}()
}

// serveTCP answers length-prefixed queries on one connection (RFC 1035 4.2.2).
func (s *stub) serveTCP(ctx context.Context, c net.Conn) {
	for {
		c.SetDeadline(time.Now().Add(2 * timeout))
		var size [2]byte
		if _, err := io.ReadFull(c, size[:]); err != nil {
			return
		}
		query := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(c, query); err != nil {
			return
		}
		reply := s.answer(ctx, query, 65535)
		if reply == nil {
			return
		}
		out := binary.BigEndian.AppendUint16(nil, uint16(len(reply)))
		if _, err := c.Write(append(out, reply...)); err != nil {
			return
		}
	}
}

// answer builds the response to one query; limit is the transport's message size (512 for UDP without
// EDNS). It returns nil for messages that are not answerable queries.
func (s *stub) answer(ctx context.Context, query []byte, limit int) []byte {
	reply := s.respond(ctx, query, limit)
	if s.trace {
		traceExchange(query, reply)
	}
	return reply
}

// traceExchange logs a query and its response (HOST_SHELL_DNS_TRACE) to standard error.
func traceExchange(query, reply []byte) {
	var q, r dnsmessage.Message
	if q.Unpack(query) != nil || len(q.Questions) == 0 {
		log.Printf("remote DNS: unparsable query (%d bytes)", len(query))
		return
	}
	if reply == nil || r.Unpack(reply) != nil {
		log.Printf("remote DNS: %s %s -> no reply", q.Questions[0].Type, q.Questions[0].Name)
		return
	}
	log.Printf("remote DNS: %s %s -> %s answers=%d truncated=%t", q.Questions[0].Type, q.Questions[0].Name, r.Header.RCode, len(r.Answers), r.Header.Truncated)
}

func (s *stub) respond(ctx context.Context, query []byte, limit int) []byte {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil || h.Response {
		return nil
	}
	reply := dnsmessage.Header{ID: h.ID, Response: true, OpCode: h.OpCode, RecursionDesired: h.RecursionDesired, RecursionAvailable: true}
	q, err := p.Question()
	if err != nil || h.OpCode != 0 {
		reply.RCode = dnsmessage.RCodeFormatError
		if h.OpCode != 0 {
			reply.RCode = dnsmessage.RCodeNotImplemented
		}
		return pack(reply, nil, nil, limit)
	}
	if udpLimit := ednsSize(&p); udpLimit > limit && limit == 512 {
		limit = udpLimit
	}
	name := strings.TrimSuffix(q.Name.String(), ".")
	family := map[dnsmessage.Type]string{dnsmessage.TypeA: "4", dnsmessage.TypeAAAA: "6"}[q.Type]
	if q.Class != dnsmessage.ClassINET || !channel.ValidDestination(name, 53) {
		reply.RCode = dnsmessage.RCodeRefused
		return pack(reply, &q, nil, limit)
	}
	if family == "" {
		// Other types (HTTPS, SVCB, PTR, MX, ...) get an empty answer so clients fall back to A/AAAA.
		return pack(reply, &q, nil, limit)
	}
	text, err := s.ask(ctx, family, name)
	if err != nil {
		reply.RCode = dnsmessage.RCodeServerFailure
		return pack(reply, &q, nil, limit)
	}
	canonical, addrs, code := parseReply(text)
	switch code {
	case "":
	case "NONAME":
		// A miss for one family may be a name with only the other family; NXDOMAIN would make the OS
		// cache the whole name as missing, so answer NODATA unless an any-family lookup also misses.
		if other, err := s.ask(ctx, "0", name); err == nil {
			if _, _, code := parseReply(other); code == "NONAME" {
				reply.RCode = dnsmessage.RCodeNameError
			}
		}
		return pack(reply, &q, nil, limit)
	default:
		reply.RCode = dnsmessage.RCodeServerFailure
		return pack(reply, &q, nil, limit)
	}
	var answers []dnsmessage.Resource
	owner := q.Name
	if canonical != "" && !strings.EqualFold(canonical, name) {
		target, err := dnsmessage.NewName(canonical + ".")
		if err == nil {
			answers = append(answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: stubTTL},
				Body:   &dnsmessage.CNAMEResource{CNAME: target},
			})
			owner = target
		}
	}
	for _, a := range addrs {
		rh := dnsmessage.ResourceHeader{Name: owner, Class: dnsmessage.ClassINET, TTL: stubTTL}
		if a.Is4() && family == "4" {
			rh.Type = dnsmessage.TypeA
			answers = append(answers, dnsmessage.Resource{Header: rh, Body: &dnsmessage.AResource{A: a.As4()}})
		} else if a.Is6() && !a.Is4In6() && family == "6" {
			rh.Type = dnsmessage.TypeAAAA
			answers = append(answers, dnsmessage.Resource{Header: rh, Body: &dnsmessage.AAAAResource{AAAA: a.As16()}})
		}
	}
	return pack(reply, &q, answers, limit)
}

// ednsSize returns the UDP payload size advertised by an EDNS(0) OPT record, or 0.
func ednsSize(p *dnsmessage.Parser) int {
	if err := p.SkipAllQuestions(); err != nil {
		return 0
	}
	if err := p.SkipAllAnswers(); err != nil {
		return 0
	}
	if err := p.SkipAllAuthorities(); err != nil {
		return 0
	}
	for {
		h, err := p.AdditionalHeader()
		if err != nil {
			return 0
		}
		if h.Type == dnsmessage.TypeOPT {
			return min(int(h.Class), 4096)
		}
		if err := p.SkipAdditional(); err != nil {
			return 0
		}
	}
}

// pack encodes a response, setting the truncation bit (without answers) when it exceeds limit.
func pack(h dnsmessage.Header, q *dnsmessage.Question, answers []dnsmessage.Resource, limit int) []byte {
	m := dnsmessage.Message{Header: h, Answers: answers}
	if q != nil {
		m.Questions = []dnsmessage.Question{*q}
	}
	b, err := m.Pack()
	if err != nil {
		return nil
	}
	if len(b) > limit {
		m.Header.Truncated, m.Answers = true, nil
		if b, err = m.Pack(); err != nil {
			return nil
		}
	}
	return b
}

// parseReply reads the bridge's line protocol: "OK <n> <canonical>" with address lines, or "ERR <code>".
func parseReply(text string) (canonical string, addrs []netip.Addr, code string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	head := strings.Fields(lines[0])
	if len(head) == 2 && head[0] == "ERR" {
		return "", nil, head[1]
	}
	if len(head) != 3 || head[0] != "OK" {
		return "", nil, "FAIL"
	}
	for _, line := range lines[1:] {
		_, ip, ok := strings.Cut(line, " ")
		if a, err := netip.ParseAddr(ip); ok && err == nil {
			addrs = append(addrs, a)
		}
	}
	if len(addrs) == 0 {
		return "", nil, "NONAME"
	}
	return head[2], addrs, ""
}
