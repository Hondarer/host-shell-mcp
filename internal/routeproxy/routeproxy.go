// Package routeproxy implements "host-shell connect", an OpenSSH ProxyCommand that reaches the
// SSH server through an SSH route proxy: an HTTP proxy that tunnels the SSH connection with
// CONNECT, optionally with Basic authentication. The command line and the environment variables
// follow connect.c (the connect command shipped with Git for Windows) for its HTTP proxy subset;
// no code of connect.c is used.
//
// The SSH route proxy is unrelated to the internet proxy that the bridge reads from HTTP_PROXY
// and HTTPS_PROXY, so this package never reads those variables.
package routeproxy

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/version"
)

const (
	// defaultProxyPort is used when -H gives no port, as in connect.c.
	defaultProxyPort = 80
	// defaultTimeout bounds connecting to the proxy and waiting for its CONNECT response, matching
	// the bridge's dial timeout. An unattended MCP setup would otherwise hang on a silent proxy.
	defaultTimeout = 30 * time.Second
	// maxHeaderBytes bounds the status line and headers of the proxy response.
	maxHeaderBytes = 64 << 10
	// keepAlive keeps the TCP connection to the proxy observable when the SSH session is idle.
	keepAlive = 30 * time.Second
)

// usage is printed for --help and for a command line without the required arguments.
const usage = `usage: host-shell connect [-d] [-w SECONDS] [--password-file PATH] -H [http://][USER@]PROXY[:PORT] HOST PORT
       host-shell connect -V

Runs as an OpenSSH ProxyCommand and reaches HOST:PORT through an SSH route proxy
(HTTP CONNECT, optionally with Basic authentication), for example:

  ProxyCommand host-shell connect -H alice@proxy.example.com:8080 --password-file ~/.ssh/proxy.pass %h %p

  -H SPEC              SSH route proxy (required; the port defaults to 80)
  -w SECONDS           limit for connecting and for the CONNECT response (default 30, 0 for none)
  -d                   print diagnostics to standard error (repeat to include response headers)
  -V                   print the version
  --password-file PATH read the proxy password from the first line of PATH

The user name comes from USER@ in -H, HTTP_PROXY_USER or CONNECT_USER. The password comes from
--password-file, HTTP_PROXY_PASSWORD, CONNECT_PASSWORD, or a prompt on the terminal.`

// errHelp asks Main to print the usage and succeed.
var errHelp = errors.New("help requested")

// errUsage marks a command line that cannot be run; Main prints the usage after the message.
type errUsage struct{ msg string }

func (e errUsage) Error() string { return e.msg }

// config is the parsed command line.
type config struct {
	proxyHost    string
	proxyPort    int
	user         string // from USER@ in -H
	destHost     string
	destPort     int
	timeout      time.Duration
	debug        int
	passwordFile string
	version      bool
}

// environment holds what run reads from outside the command line, so tests can replace it.
type environment struct {
	lookupEnv func(string) (string, bool)
	// prompt asks the user for a password on the terminal. ok is false when no terminal is
	// available (for example when the agent started ssh with standard error on a pipe).
	prompt func(prompt string) (password string, ok bool, err error)
}

// Main runs host-shell connect with the arguments after "connect" and returns the exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args)
	switch {
	case errors.Is(err, errHelp):
		io.WriteString(stderr, usage+"\n")
		return 0
	case err != nil:
		fmt.Fprintf(stderr, "host-shell connect: %v\n", err)
		var u errUsage
		if errors.As(err, &u) {
			io.WriteString(stderr, usage+"\n")
		}
		return 1
	case cfg.version:
		fmt.Fprintf(stderr, "host-shell connect\nVersion %s\n", version.Version)
		return 0
	}
	env := environment{lookupEnv: os.LookupEnv, prompt: promptPassword}
	if err := run(cfg, env, stdin, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "host-shell connect: %v\n", err)
		return 1
	}
	return 0
}

// unsupportedOptions are connect.c options outside the HTTP CONNECT subset.
const unsupportedOptions = "SsTtc45aRpPnD"

// parseArgs parses a connect.c style command line: single-letter options that may be grouped
// (-dd) and may carry their value in the same argument (-Hproxy:8080), plus --password-file.
func parseArgs(args []string) (config, error) {
	cfg := config{timeout: defaultTimeout}
	var spec string
	var haveSpec bool
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
		case a == "--help":
			return cfg, errHelp
		case a == "--password-file":
			if i+1 >= len(args) {
				return cfg, errUsage{"option --password-file requires a path"}
			}
			i++
			cfg.passwordFile = args[i]
		case strings.HasPrefix(a, "--password-file="):
			cfg.passwordFile = strings.TrimPrefix(a, "--password-file=")
		case strings.HasPrefix(a, "--"):
			return cfg, errUsage{fmt.Sprintf("unknown option %s", a)}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				switch {
				case c == 'd':
					cfg.debug++
				case c == 'V':
					cfg.version = true
				case c == 'H' || c == 'w':
					val := a[j+1:]
					if val == "" {
						if i+1 >= len(args) {
							return cfg, errUsage{fmt.Sprintf("option -%c requires an argument", c)}
						}
						i++
						val = args[i]
					}
					j = len(a)
					if c == 'H' {
						spec, haveSpec = val, true
						continue
					}
					sec, err := strconv.Atoi(val)
					if err != nil || sec < 0 {
						return cfg, fmt.Errorf("invalid -w value %q: give the number of seconds (0 for no limit)", val)
					}
					cfg.timeout = time.Duration(sec) * time.Second
				case c == 'h':
					return cfg, errors.New("option -h is not supported: give the SSH route proxy explicitly with -H (HTTP_PROXY is not read)")
				case strings.IndexByte(unsupportedOptions, c) >= 0:
					return cfg, fmt.Errorf("option -%c is not supported (only HTTP CONNECT through -H is supported)", c)
				default:
					return cfg, errUsage{fmt.Sprintf("unknown option -%c", c)}
				}
			}
		default:
			pos = append(pos, a)
		}
	}
	if cfg.version {
		return cfg, nil
	}
	if !haveSpec {
		return cfg, errUsage{"-H is required (the SSH route proxy)"}
	}
	var err error
	if cfg.proxyHost, cfg.proxyPort, cfg.user, err = parseProxySpec(spec); err != nil {
		return cfg, err
	}
	if len(pos) != 2 {
		return cfg, errUsage{"HOST and PORT are required (use %h %p in ProxyCommand)"}
	}
	cfg.destHost = strings.TrimSuffix(strings.TrimPrefix(pos[0], "["), "]")
	if cfg.destHost == "" {
		return cfg, errors.New("empty destination host")
	}
	if cfg.destPort, err = parsePort(pos[1]); err != nil {
		return cfg, fmt.Errorf("invalid destination port %q", pos[1])
	}
	return cfg, nil
}

// parseProxySpec splits [http://][USER@]HOST[:PORT][/...] as connect.c does: the user ends at
// the last '@' and anything from '/' on is ignored. An IPv6 address must be enclosed in [ ].
func parseProxySpec(spec string) (host string, port int, user string, err error) {
	s := spec
	if i := strings.Index(s, "://"); i >= 0 {
		if !strings.EqualFold(s[:i], "http") {
			return "", 0, "", fmt.Errorf("proxy scheme %q is not supported (only http)", s[:i])
		}
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		user, s = s[:i], s[i+1:]
		if user == "" {
			return "", 0, "", errors.New("empty user name before @ in -H")
		}
		if strings.Contains(user, ":") {
			return "", 0, "", errors.New("the user name in -H must not contain ':'; give the password with --password-file or HTTP_PROXY_PASSWORD")
		}
	}
	host, port = s, defaultProxyPort
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return "", 0, "", fmt.Errorf("missing ] in proxy %q", spec)
		}
		host = s[1:end]
		if rest := s[end+1:]; rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return "", 0, "", fmt.Errorf("invalid proxy %q", spec)
			}
			if port, err = parsePort(rest[1:]); err != nil {
				return "", 0, "", fmt.Errorf("invalid proxy port in %q", spec)
			}
		}
	case strings.Count(s, ":") > 1:
		return "", 0, "", fmt.Errorf("enclose the IPv6 address of proxy %q in [ ]", spec)
	case strings.Contains(s, ":"):
		i := strings.IndexByte(s, ':')
		host = s[:i]
		if port, err = parsePort(s[i+1:]); err != nil {
			return "", 0, "", fmt.Errorf("invalid proxy port in %q", spec)
		}
	}
	if host == "" {
		return "", 0, "", fmt.Errorf("empty proxy host in %q", spec)
	}
	return host, port, user, nil
}

// parsePort accepts a decimal port from 1 to 65535.
func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, errors.New("invalid port")
	}
	return p, nil
}

// run connects through the proxy and relays standard input and output until the proxy side ends.
func run(cfg config, env environment, stdin io.Reader, stdout, stderr io.Writer) error {
	debugf := func(level int, format string, a ...any) {
		if cfg.debug >= level {
			fmt.Fprintf(stderr, "host-shell connect: "+format+"\n", a...)
		}
	}
	proxyAddr := net.JoinHostPort(cfg.proxyHost, strconv.Itoa(cfg.proxyPort))
	target := net.JoinHostPort(cfg.destHost, strconv.Itoa(cfg.destPort))

	user := cfg.user
	for _, name := range []string{"HTTP_PROXY_USER", "CONNECT_USER"} {
		if user != "" {
			break
		}
		user, _ = env.lookupEnv(name)
	}
	if strings.Contains(user, ":") {
		return errors.New("the proxy user name must not contain ':'")
	}
	// The credentials are sent with the first request, so the password is needed before
	// connecting; a 407 reply is final.
	var auth string
	if user != "" {
		pass, err := password(cfg, env, user, proxyAddr)
		if err != nil {
			return err
		}
		auth = base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	}

	var deadline time.Time
	if cfg.timeout > 0 {
		deadline = time.Now().Add(cfg.timeout)
	}
	timedOut := func(err error) bool {
		var ne net.Error
		return errors.As(err, &ne) && ne.Timeout()
	}
	timeoutErr := fmt.Errorf("timed out after %s waiting for SSH route proxy %s", cfg.timeout, proxyAddr)

	debugf(1, "connecting to SSH route proxy %s", proxyAddr)
	d := net.Dialer{Deadline: deadline, KeepAlive: keepAlive}
	conn, err := d.Dial("tcp", proxyAddr)
	if err != nil {
		if timedOut(err) {
			return timeoutErr
		}
		return fmt.Errorf("cannot connect to SSH route proxy %s: %v", proxyAddr, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	var req strings.Builder
	fmt.Fprintf(&req, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: host-shell/%s\r\n", target, target, version.Version)
	if auth != "" {
		fmt.Fprintf(&req, "Proxy-Authorization: Basic %s\r\n", auth)
		debugf(1, "sending CONNECT %s with Basic authentication as %s", target, user)
	} else {
		debugf(1, "sending CONNECT %s without authentication", target)
	}
	req.WriteString("\r\n")
	if _, err := io.WriteString(conn, req.String()); err != nil {
		if timedOut(err) {
			return timeoutErr
		}
		return fmt.Errorf("cannot send CONNECT to SSH route proxy %s: %v", proxyAddr, err)
	}

	br := bufio.NewReader(conn)
	code, status, err := readResponse(br, func(h string) { debugf(2, "response header: %s", h) })
	switch {
	case timedOut(err):
		return timeoutErr
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("SSH route proxy %s closed the connection without a complete response", proxyAddr)
	case err != nil:
		return fmt.Errorf("invalid response from SSH route proxy %s: %v", proxyAddr, err)
	}
	debugf(1, "response: %s", status)
	switch {
	case code >= 200 && code < 300:
	case code == 407 && auth != "":
		return fmt.Errorf("SSH route proxy %s rejected the credentials of %s (407)", proxyAddr, user)
	case code == 407:
		return fmt.Errorf("SSH route proxy %s requires authentication (407); give USER@ in -H or HTTP_PROXY_USER", proxyAddr)
	default:
		return fmt.Errorf("SSH route proxy %s refused CONNECT %s (%s)", proxyAddr, target, status)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	debugf(1, "relaying to %s", target)
	return relay(conn, br, stdin, stdout)
}

// password returns the proxy password from the password file, the environment, or the terminal.
func password(cfg config, env environment, user, proxyAddr string) (string, error) {
	if cfg.passwordFile != "" {
		return readPasswordFile(cfg.passwordFile)
	}
	for _, name := range []string{"HTTP_PROXY_PASSWORD", "CONNECT_PASSWORD"} {
		if v, ok := env.lookupEnv(name); ok {
			return v, nil
		}
	}
	if env.prompt != nil {
		p, ok, err := env.prompt(fmt.Sprintf("Enter password for SSH route proxy %s@%s: ", user, proxyAddr))
		if err != nil {
			return "", err
		}
		if ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("no password for %s@%s; use --password-file or HTTP_PROXY_PASSWORD", user, proxyAddr)
}

// readResponse reads the status line and headers of the proxy's reply to CONNECT and returns the
// status code and the status text after the HTTP version (for example "403 Forbidden").
func readResponse(br *bufio.Reader, header func(string)) (int, string, error) {
	budget := maxHeaderBytes
	line, err := readLine(br, &budget)
	if err != nil {
		return 0, "", err
	}
	version, status, _ := strings.Cut(line, " ")
	if !strings.HasPrefix(version, "HTTP/") || len(status) < 3 {
		return 0, "", fmt.Errorf("unexpected status line %q", line)
	}
	code, err := strconv.Atoi(status[:3])
	if err != nil || (len(status) > 3 && status[3] != ' ') {
		return 0, "", fmt.Errorf("unexpected status line %q", line)
	}
	for {
		h, err := readLine(br, &budget)
		if err != nil {
			return 0, "", err
		}
		if h == "" {
			return code, strings.TrimSpace(status), nil
		}
		header(h)
	}
}

// readLine reads one CRLF or LF terminated line, charging its length to budget.
func readLine(br *bufio.Reader, budget *int) (string, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		*budget -= len(chunk)
		if *budget < 0 {
			return "", fmt.Errorf("response headers exceed %d bytes", maxHeaderBytes)
		}
		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return "", io.ErrUnexpectedEOF
			}
			return "", err
		}
		return strings.TrimRight(string(line), "\r\n"), nil
	}
}

// relay copies bytes unchanged in both directions. The end of standard input half-closes the
// connection; the end of the proxy side ends the relay. Bytes the proxy sent right after the
// response headers are already in br and are written first.
func relay(conn net.Conn, br *bufio.Reader, stdin io.Reader, stdout io.Writer) error {
	go func() {
		_, _ = io.Copy(conn, stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	if _, err := br.WriteTo(stdout); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("relay: %v", err)
	}
	return nil
}
