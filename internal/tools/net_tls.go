package tools

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// TLSInfo describes the TLS connection and the peer's certificate.
type TLSInfo struct {
	Version     string   `json:"version"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	DNSNames    []string `json:"dns_names,omitempty"`
	NotBefore   string   `json:"not_before,omitempty"`
	NotAfter    string   `json:"not_after,omitempty"`
	DaysLeft    int      `json:"days_left,omitempty"`
	Verified    bool     `json:"verified"`
	VerifyError string   `json:"verify_error,omitempty"`
}

func clientTLS(insecure bool, caFile, certFile, keyFile string) (*tls.Config, error) {
	c := &tls.Config{InsecureSkipVerify: insecure}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("ca_cert: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_cert: no certificates found in %s", caFile)
		}
		c.RootCAs = pool
	}
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("client_cert and client_key must be given together")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		c.Certificates = []tls.Certificate{cert}
	}
	return c, nil
}

// handshakeTLS runs a TLS client handshake over conn and summarizes the result.
func handshakeTLS(ctx context.Context, conn net.Conn, host string, insecure bool) (*tls.Conn, *TLSInfo, error) {
	tc := tls.Client(conn, &tls.Config{InsecureSkipVerify: insecure, ServerName: host})
	if err := tc.HandshakeContext(ctx); err != nil {
		return nil, nil, fmt.Errorf("tls handshake: %w", err)
	}
	st := tc.ConnectionState()
	return tc, tlsInfo(&st, host, nil), nil
}

// defaultPort returns the port for http and https URLs without an explicit one, otherwise "".
func defaultPort(scheme string) string {
	switch scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

// tlsInfo summarizes the TLS connection. Even when verification was skipped, it shows what verification would have found.
func tlsInfo(cs *tls.ConnectionState, host string, conf *tls.Config) *TLSInfo {
	info := &TLSInfo{Version: tls.VersionName(cs.Version)}
	if len(cs.PeerCertificates) == 0 {
		return info
	}
	leaf := cs.PeerCertificates[0]
	info.Subject = leaf.Subject.String()
	info.Issuer = leaf.Issuer.String()
	info.DNSNames = leaf.DNSNames
	info.NotBefore = leaf.NotBefore.Format(time.RFC3339)
	info.NotAfter = leaf.NotAfter.Format(time.RFC3339)
	info.DaysLeft = int(time.Until(leaf.NotAfter).Hours() / 24)
	opts := x509.VerifyOptions{DNSName: host, Intermediates: x509.NewCertPool()}
	if conf != nil {
		opts.Roots = conf.RootCAs
	}
	for _, c := range cs.PeerCertificates[1:] {
		opts.Intermediates.AddCert(c)
	}
	if _, err := leaf.Verify(opts); err != nil {
		info.VerifyError = err.Error()
	} else {
		info.Verified = true
	}
	return info
}
