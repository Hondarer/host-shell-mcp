package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// ---- net_request_http ----

// NetRequestHTTPInput is the input of net_request_http.
type NetRequestHTTPInput struct {
	URL              string            `json:"url" jsonschema:"request URL, e.g. http://localhost:8080/health (host is ignored when unix_socket is set)"`
	Method           string            `json:"method,omitempty" jsonschema:"HTTP method (default GET)"`
	Headers          map[string]string `json:"headers,omitempty" jsonschema:"request headers, e.g. {\"Authorization\": \"Bearer ...\", \"Content-Type\": \"application/json\"}"`
	Body             string            `json:"body,omitempty" jsonschema:"request body"`
	BodyEncoding     string            `json:"body_encoding,omitempty" jsonschema:"text (default, UTF-8) or base64"`
	BodyFile         string            `json:"body_file,omitempty" jsonschema:"remote file path to send as the request body (mutually exclusive with body)"`
	UnixSocket       string            `json:"unix_socket,omitempty" jsonschema:"Unix domain socket path to connect to instead of the URL host, e.g. /var/run/docker.sock"`
	TimeoutSec       int               `json:"timeout_sec,omitempty" jsonschema:"overall timeout in seconds (default: server command timeout)"`
	NoRedirects      bool              `json:"no_redirects,omitempty" jsonschema:"return 3xx responses without following redirects"`
	Insecure         bool              `json:"insecure,omitempty" jsonschema:"skip TLS certificate verification"`
	CACert           string            `json:"ca_cert,omitempty" jsonschema:"remote PEM file with additional trusted CA certificates"`
	ClientCert       string            `json:"client_cert,omitempty" jsonschema:"remote PEM client certificate for mTLS (requires client_key)"`
	ClientKey        string            `json:"client_key,omitempty" jsonschema:"remote PEM private key for client_cert"`
	Proxy            string            `json:"proxy,omitempty" jsonschema:"proxy URL, or none for direct connection (default: HTTP_PROXY/HTTPS_PROXY/NO_PROXY)"`
	ResponseEncoding string            `json:"response_encoding,omitempty" jsonschema:"auto (default: text for textual media types or valid UTF-8, else base64), text or base64"`
	MaxBytes         int               `json:"max_bytes,omitempty" jsonschema:"max response body bytes to return (default and cap: server output limit; ignored with save_to)"`
	SaveTo           string            `json:"save_to,omitempty" jsonschema:"save response body to this remote file path instead of returning it"`
}

// AuditArgs redacts the body and sensitive headers for audit logging.
func (in NetRequestHTTPInput) AuditArgs() any {
	in.Body = redactedLen(in.Body)
	h := map[string]string{}
	for k, v := range in.Headers {
		switch strings.ToLower(k) {
		case "authorization", "proxy-authorization", "cookie", "x-api-key":
			v = redactedLen(v)
		}
		h[k] = v
	}
	in.Headers = h
	return in
}

// PathArgs passes the target files read and written to the policy check.
func (in NetRequestHTTPInput) PathArgs() (read, write []string) {
	for _, p := range []string{in.BodyFile, in.CACert, in.ClientCert, in.ClientKey} {
		if p != "" {
			read = append(read, p)
		}
	}
	if in.SaveTo != "" {
		write = []string{in.SaveTo}
	}
	return read, write
}

// NetRequestHTTPOutput is the output of net_request_http.
type NetRequestHTTPOutput struct {
	Status        int               `json:"status"`
	StatusText    string            `json:"status_text"`
	Proto         string            `json:"proto"`
	Headers       map[string]string `json:"headers" jsonschema:"response headers (multiple values joined with , )"`
	Body          string            `json:"body,omitempty"`
	BodyEncoding  string            `json:"body_encoding,omitempty" jsonschema:"text or base64"`
	BodyCharset   string            `json:"body_charset,omitempty" jsonschema:"set when the text body was converted to UTF-8 from this charset"`
	BodyBytes     int64             `json:"body_bytes" jsonschema:"bytes of body received"`
	BodyTruncated bool              `json:"body_truncated,omitempty" jsonschema:"only the first max_bytes were returned"`
	SavedTo       string            `json:"saved_to,omitempty"`
	FinalURL      string            `json:"final_url,omitempty" jsonschema:"set when redirects were followed"`
	Redirects     []string          `json:"redirects,omitempty"`
	TLS           *TLSInfo          `json:"tls,omitempty"`
	DurationMs    int64             `json:"duration_ms"`
}

func (d *Deps) netRequestHTTP(ctx context.Context, req *mcp.CallToolRequest, in NetRequestHTTPInput) (*mcp.CallToolResult, NetRequestHTTPOutput, error) {
	method := strings.ToUpper(orDefault(in.Method, http.MethodGet))
	if d.Policy.ReadOnly && (!slices.Contains([]string{"GET", "HEAD", "OPTIONS"}, method) || in.SaveTo != "") {
		return nil, NetRequestHTTPOutput{}, errors.New("policy: read-only mode allows only GET, HEAD and OPTIONS without save_to")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Host == "" && in.UnixSocket == "") {
		return nil, NetRequestHTTPOutput{}, fmt.Errorf("url must be an absolute http or https URL: %q", in.URL)
	}
	if err := d.checkURL(u, in.UnixSocket); err != nil {
		return nil, NetRequestHTTPOutput{}, err
	}

	tlsConf, err := clientTLS(in.Insecure, in.CACert, in.ClientCert, in.ClientKey)
	if err != nil {
		return nil, NetRequestHTTPOutput{}, err
	}
	tr := &http.Transport{TLSClientConfig: tlsConf, Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
	if d.Cfg.Proxy != nil {
		tr.Proxy = d.Cfg.Proxy.Proxy
	}
	switch in.Proxy {
	case "":
	case "none":
		tr.Proxy = nil
	default:
		pu, err := url.Parse(in.Proxy)
		if err != nil {
			return nil, NetRequestHTTPOutput{}, fmt.Errorf("proxy: %w", err)
		}
		tr.Proxy = http.ProxyURL(pu)
	}
	if in.UnixSocket != "" {
		tr.Proxy = nil
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dl net.Dialer
			return dl.DialContext(ctx, "unix", in.UnixSocket)
		}
	}
	defer tr.CloseIdleConnections()

	var out NetRequestHTTPOutput
	client := &http.Client{Transport: tr, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if in.NoRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if err := d.checkURL(r.URL, in.UnixSocket); err != nil {
			return err
		}
		out.Redirects = append(out.Redirects, r.URL.String())
		return nil
	}}

	timeout := d.Cfg.ClampTimeout(in.TimeoutSec)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := requestBody(in)
	if err != nil {
		return nil, NetRequestHTTPOutput{}, err
	}
	if c, ok := body.(io.Closer); ok {
		defer c.Close()
	}
	hreq, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, NetRequestHTTPOutput{}, err
	}
	for k, v := range in.Headers {
		if strings.EqualFold(k, "Host") {
			hreq.Host = v
			continue
		}
		hreq.Header.Set(k, v)
	}
	if f, ok := body.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			hreq.ContentLength = st.Size()
		}
	}

	start := time.Now()
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, NetRequestHTTPOutput{}, err
	}
	defer resp.Body.Close()
	out.Status, out.StatusText, out.Proto = resp.StatusCode, http.StatusText(resp.StatusCode), resp.Proto
	out.Headers = map[string]string{}
	for k, v := range resp.Header {
		out.Headers[k] = strings.Join(v, ", ")
	}
	if len(out.Redirects) > 0 {
		out.FinalURL = resp.Request.URL.String()
	}
	if resp.TLS != nil {
		out.TLS = tlsInfo(resp.TLS, resp.Request.URL.Hostname(), tlsConf)
	}

	pr := d.progress(ctx, req)
	if in.SaveTo != "" {
		err = d.saveBody(resp, in.SaveTo, pr, &out)
	} else {
		err = d.readBody(resp, in, &out)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		return nil, NetRequestHTTPOutput{}, err
	}
	return nil, out, nil
}

// checkURL checks whether the policy allows the URL's destination.
func (d *Deps) checkURL(u *url.URL, unixSocket string) error {
	if unixSocket != "" {
		return d.Policy.CheckUnixSocket(unixSocket)
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	n, _ := strconv.Atoi(port)
	return d.Policy.CheckHost(u.Hostname(), n)
}

func requestBody(in NetRequestHTTPInput) (io.Reader, error) {
	switch {
	case in.BodyFile != "" && in.Body != "":
		return nil, errors.New("body and body_file are mutually exclusive")
	case in.BodyFile != "":
		return os.Open(in.BodyFile)
	case in.Body == "":
		return nil, nil
	}
	switch orDefault(in.BodyEncoding, "text") {
	case "text":
		return strings.NewReader(in.Body), nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(in.Body)
		if err != nil {
			return nil, fmt.Errorf("body: %w", err)
		}
		return bytes.NewReader(b), nil
	default:
		return nil, fmt.Errorf("unknown body_encoding %q", in.BodyEncoding)
	}
}

func (d *Deps) readBody(resp *http.Response, in NetRequestHTTPInput, out *NetRequestHTTPOutput) error {
	max := d.Cfg.MaxOutputBytes
	enc := orDefault(in.ResponseEncoding, "auto")
	if enc == "base64" || enc == "auto" {
		max = d.Cfg.MaxOutputBytes / 4 * 3
	}
	if in.MaxBytes > 0 && in.MaxBytes < max {
		max = in.MaxBytes
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, int64(max)))
	if err != nil {
		return err
	}
	// Only count the rest.
	rest, _ := io.Copy(io.Discard, resp.Body)
	out.BodyBytes = int64(len(buf)) + rest
	out.BodyTruncated = rest > 0

	ct := resp.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(ct)
	charset := strings.ToLower(params["charset"])
	if enc == "auto" {
		enc = "base64"
		if textual(mediaType) || (mediaType == "" && utf8.Valid(buf[:len(buf)-textenc.IncompleteTail(buf)])) {
			enc = "text"
		}
		if charset != "" && charset != "utf-8" && charset != "us-ascii" {
			enc = "text"
		}
	}
	switch enc {
	case "text":
		out.BodyEncoding = "text"
		if out.BodyTruncated {
			buf = buf[:len(buf)-textenc.IncompleteTail(buf)]
		}
		if charset != "" && charset != "utf-8" && charset != "us-ascii" {
			if e, err := textenc.Lookup(charset); err == nil && !e.IsUTF8() {
				out.Body = e.Decode(buf)
				out.BodyCharset = e.Name
				return nil
			}
		}
		out.Body = strings.ToValidUTF8(string(buf), "�")
	case "base64":
		out.BodyEncoding = "base64"
		out.Body = base64.StdEncoding.EncodeToString(buf)
	default:
		return fmt.Errorf("unknown response_encoding %q", in.ResponseEncoding)
	}
	return nil
}

// textual reports whether the body should be returned as text.
func textual(mediaType string) bool {
	switch {
	case strings.HasPrefix(mediaType, "text/"):
		return true
	case strings.HasSuffix(mediaType, "+json"), strings.HasSuffix(mediaType, "+xml"):
		return true
	}
	return slices.Contains([]string{"application/json", "application/xml", "application/javascript",
		"application/x-www-form-urlencoded", "application/yaml", "application/x-yaml", "application/x-ndjson",
		"application/problem+json", "application/graphql"}, mediaType)
}

func (d *Deps) saveBody(resp *http.Response, path string, pr *progress, out *NetRequestHTTPOutput) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	total := float64(max(resp.ContentLength, 0))
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := f.Write(buf[:k]); err != nil {
				f.Close()
				return err
			}
			n += int64(k)
			pr.report(float64(n), total, fmt.Sprintf("%d bytes downloaded", n), false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	out.BodyBytes = n
	out.SavedTo = absPath(path)
	return nil
}
