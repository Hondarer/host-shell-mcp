package remotedns

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/hondarer/host-shell-mcp/internal/channel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubResolver answers like fixedResolver, plus a name with only IPv6 and a name whose CNAME is itself.
type stubResolver struct{ fixedResolver }

func (stubResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	switch host {
	case "v6only.invalid":
		if network == "ip4" {
			return nil, &net.DNSError{IsNotFound: true}
		}
		return []net.IP{net.ParseIP("2001:db8::6")}, nil
	case "plain.invalid":
		return []net.IP{net.ParseIP("192.0.2.7")}, nil
	}
	return fixedResolver{}.LookupIP(ctx, network, host)
}

func (stubResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if host == "example.invalid" {
		return "canonical.example.", nil
	}
	return host + ".", nil
}

// startStubSession connects a target with a loopback DNS server to a bridge whose resolver is stubResolver.
func startStubSession(t *testing.T) *Target {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	// Built directly so the test covers the DNS server alone on every OS (no preload adapter).
	target := &Target{}
	target.ctx, target.cancel = context.WithCancel(context.Background())
	s, err := listenStub("127.0.0.1:0", target.ask)
	if err != nil {
		t.Fatal(err)
	}
	target.stub, target.Listen = s, s.addr
	t.Cleanup(target.Close)
	var to, bo channel.Options
	target.Register(&to)
	RegisterGateway(&bo)
	bo.Handle(channel.KindDNS, func(ctx context.Context, in *channel.Incoming) {
		c, err := in.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(timeout))
		serve(ctx, c, in.Host, stubResolver{})
	})
	a, b := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { _, e := channel.Target(a, to).Connect(ctx); done <- e }()
	bridge, err := channel.Client(b, bo).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bridge.Close() })
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	return target
}

func query(t *testing.T, name string, qtype dnsmessage.Type) []byte {
	t.Helper()
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: 0x1234, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(name), Type: qtype, Class: dnsmessage.ClassINET}}}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t *testing.T, b []byte) dnsmessage.Message {
	t.Helper()
	var m dnsmessage.Message
	if err := m.Unpack(b); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStubUDPAnswers(t *testing.T) {
	target := startStubSession(t)
	c, err := net.Dial("udp", target.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ask := func(name string, qtype dnsmessage.Type) dnsmessage.Message {
		c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Write(query(t, name, qtype)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 1500)
		n, err := c.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return decode(t, buf[:n])
	}

	m := ask("example.invalid.", dnsmessage.TypeA)
	if m.Header.ID != 0x1234 || !m.Header.Response || m.Header.RCode != dnsmessage.RCodeSuccess || len(m.Answers) != 2 {
		t.Fatalf("A: %+v", m)
	}
	if cn, ok := m.Answers[0].Body.(*dnsmessage.CNAMEResource); !ok || cn.CNAME.String() != "canonical.example." {
		t.Fatalf("CNAME: %+v", m.Answers[0])
	}
	if a, ok := m.Answers[1].Body.(*dnsmessage.AResource); !ok || a.A != [4]byte{192, 0, 2, 1} || m.Answers[1].Header.Name.String() != "canonical.example." {
		t.Fatalf("A record: %+v", m.Answers[1])
	}

	m = ask("plain.invalid.", dnsmessage.TypeA)
	if len(m.Answers) != 1 || m.Answers[0].Header.Type != dnsmessage.TypeA {
		t.Fatalf("plain A without CNAME: %+v", m.Answers)
	}
	m = ask("example.invalid.", dnsmessage.TypeAAAA)
	if aaaa, ok := m.Answers[len(m.Answers)-1].Body.(*dnsmessage.AAAAResource); !ok || net.IP(aaaa.AAAA[:]).String() != "2001:db8::1" {
		t.Fatalf("AAAA: %+v", m.Answers)
	}
	if m = ask("missing.invalid.", dnsmessage.TypeA); m.Header.RCode != dnsmessage.RCodeNameError || len(m.Answers) != 0 {
		t.Fatalf("NXDOMAIN: %+v", m.Header)
	}
	// An IPv6-only name must not be reported as missing for A, or the OS would cache it as absent.
	if m = ask("v6only.invalid.", dnsmessage.TypeA); m.Header.RCode != dnsmessage.RCodeSuccess || len(m.Answers) != 0 {
		t.Fatalf("NODATA for A of an IPv6-only name: %+v", m)
	}
	if m = ask("example.invalid.", dnsmessage.TypeMX); m.Header.RCode != dnsmessage.RCodeSuccess || len(m.Answers) != 0 {
		t.Fatalf("other types get NODATA: %+v", m)
	}
	if m = ask("bad name.invalid.", dnsmessage.TypeA); m.Header.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("invalid name: %+v", m.Header)
	}
}

func TestStubTCPAndClose(t *testing.T) {
	target := startStubSession(t)
	c, err := net.Dial("tcp", target.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	for range 2 { // two queries on one connection
		q := query(t, "example.invalid.", dnsmessage.TypeA)
		if _, err := c.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(q))), q...)); err != nil {
			t.Fatal(err)
		}
		var size [2]byte
		if _, err := io.ReadFull(c, size[:]); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(c, b); err != nil {
			t.Fatal(err)
		}
		if m := decode(t, b); len(m.Answers) != 2 {
			t.Fatalf("TCP answer: %+v", m)
		}
	}
	addr := target.Listen
	target.Close()
	if l, err := net.Listen("tcp", addr); err != nil {
		t.Fatalf("listener not released after close: %v", err)
	} else {
		l.Close()
	}
}

func TestStubRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:53", "192.0.2.1:53", "localhost:53", "127.0.0.1"} {
		if s, err := listenStub(addr, nil); err == nil {
			s.close()
			t.Fatalf("accepted %s", addr)
		}
	}
}

func TestStubBeforeAttachIsServerFailure(t *testing.T) {
	target := &Target{}
	s := &stub{ask: target.ask}
	m := decode(t, s.answer(context.Background(), query(t, "example.invalid.", dnsmessage.TypeA), 512))
	if m.Header.RCode != dnsmessage.RCodeServerFailure {
		t.Fatalf("%+v", m.Header)
	}
}
