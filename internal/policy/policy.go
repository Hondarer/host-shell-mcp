// Package policy enforces startup restrictions on tool exposure, paths, commands, network
// destinations, environment variables, and output redaction.
//
// By default, nothing is restricted. Path and network checks apply to tool arguments only;
// child processes started via exec tools can access the host directly, so strict sandboxing
// requires disabling the exec group with --tools.
package policy

import (
	"errors"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// Options configures startup restrictions.
type Options struct {
	// Tools selects exposed tools (e.g., "all,-admin,-exec_sudo"; empty means "all").
	Tools string
	// ReadOnly hides tools that modify the host.
	ReadOnly bool
	// Confirm selects tools requiring user confirmation (MCP elicitation) per call.
	Confirm string
	// AllowPaths restricts file read/write access to these directories (empty means unrestricted).
	AllowPaths []string
	// AllowReadPaths allows read-only file access under these directories.
	AllowReadPaths []string
	// AllowCommands restricts execution to matching program name globs (empty means unrestricted).
	AllowCommands []string
	// DenyCommands rejects command lines matching any of these regular expressions.
	DenyCommands []string
	// AllowHosts restricts network tools to matching destinations (empty means unrestricted).
	AllowHosts []string
	// UnsetEnv excludes matching environment variable globs from child processes.
	UnsetEnv []string
	// Redact masks common secrets (keys, tokens, passwords) in tool output.
	Redact bool
	// RedactPatterns specifies additional regular expressions to mask in tool output.
	RedactPatterns []string
}

// Policy holds compiled restrictions; the zero value imposes none.
type Policy struct {
	ReadOnly bool

	tools   selector
	confirm selector

	writeRoots []string
	readRoots  []string

	allowCommands []string
	denyCommands  []*regexp.Regexp

	allowHosts []hostRule

	unsetEnv []string

	redact []redaction
}

// New validates o and compiles a Policy. known maps tool names to their group names.
func New(o Options, known map[string]string) (*Policy, error) {
	p := &Policy{ReadOnly: o.ReadOnly}
	var err error
	if p.tools, err = parseSelector(orDefault(o.Tools, "all"), known); err != nil {
		return nil, fmt.Errorf("tools: %w", err)
	}
	if o.Confirm != "" {
		if p.confirm, err = parseSelector(o.Confirm, known); err != nil {
			return nil, fmt.Errorf("confirm: %w", err)
		}
	}
	for _, r := range o.AllowPaths {
		root, err := resolveRoot(r)
		if err != nil {
			return nil, fmt.Errorf("allow-path: %w", err)
		}
		p.writeRoots = append(p.writeRoots, root)
		p.readRoots = append(p.readRoots, root)
	}
	for _, r := range o.AllowReadPaths {
		root, err := resolveRoot(r)
		if err != nil {
			return nil, fmt.Errorf("allow-read-path: %w", err)
		}
		p.readRoots = append(p.readRoots, root)
	}
	if len(p.readRoots) > 0 && len(p.writeRoots) == 0 {
		// With only read-only roots configured, disallow all writes.
		p.writeRoots = []string{}
	}
	for _, c := range o.AllowCommands {
		if _, err := path.Match(c, ""); err != nil {
			return nil, fmt.Errorf("allow-command %q: %w", c, err)
		}
		p.allowCommands = append(p.allowCommands, normalizeProgram(c))
	}
	for _, c := range o.DenyCommands {
		re, err := regexp.Compile(c)
		if err != nil {
			return nil, fmt.Errorf("deny-command: %w", err)
		}
		p.denyCommands = append(p.denyCommands, re)
	}
	for _, h := range o.AllowHosts {
		r, err := parseHostRule(h)
		if err != nil {
			return nil, fmt.Errorf("allow-host %q: %w", h, err)
		}
		p.allowHosts = append(p.allowHosts, r)
	}
	for _, e := range o.UnsetEnv {
		if _, err := path.Match(e, ""); err != nil {
			return nil, fmt.Errorf("unset-env %q: %w", e, err)
		}
		p.unsetEnv = append(p.unsetEnv, e)
	}
	if o.Redact {
		p.redact = builtinRedactions()
	}
	for _, r := range o.RedactPatterns {
		re, err := regexp.Compile(r)
		if err != nil {
			return nil, fmt.Errorf("redact-pattern: %w", err)
		}
		p.redact = append(p.redact, redaction{re: re, repl: "[REDACTED]"})
	}
	return p, nil
}

// Unrestricted returns a Policy with no restriction.
func Unrestricted() *Policy {
	p, _ := New(Options{}, nil)
	return p
}

// Enabled reports whether the tool is exposed.
func (p *Policy) Enabled(tool, group string, readOnlyTool bool) bool {
	return p.tools.match(tool, group, !readOnlyTool)
}

// NeedsConfirm reports whether the tool needs user confirmation before it runs.
func (p *Policy) NeedsConfirm(tool, group string, readOnlyTool bool) bool {
	return p.confirm.match(tool, group, !readOnlyTool)
}

// Summary returns active restrictions as (label, value) pairs.
func (p *Policy) Summary() [][2]string {
	var out [][2]string
	if p.tools.spec != "all" {
		out = append(out, [2]string{"Tool selection", p.tools.spec})
	}
	if p.ReadOnly {
		out = append(out, [2]string{"Read-only", "tools that change the host are not available"})
	}
	if p.confirm.spec != "" {
		out = append(out, [2]string{"Confirmation required", p.confirm.spec})
	}
	if p.writeRoots != nil {
		out = append(out, [2]string{"Writable paths", listOrNone(p.writeRoots)})
		out = append(out, [2]string{"Readable paths", listOrNone(p.readRoots)})
	}
	if len(p.allowCommands) > 0 {
		out = append(out, [2]string{"Allowed programs", strings.Join(p.allowCommands, ", ")})
	}
	if len(p.denyCommands) > 0 {
		var s []string
		for _, re := range p.denyCommands {
			s = append(s, re.String())
		}
		out = append(out, [2]string{"Denied commands", strings.Join(s, ", ")})
	}
	if len(p.allowHosts) > 0 {
		var s []string
		for _, h := range p.allowHosts {
			s = append(s, h.spec)
		}
		out = append(out, [2]string{"Allowed network destinations", strings.Join(s, ", ")})
	}
	if len(p.unsetEnv) > 0 {
		out = append(out, [2]string{"Environment not passed to commands", strings.Join(p.unsetEnv, ", ")})
	}
	if len(p.redact) > 0 {
		out = append(out, [2]string{"Secrets in output", "redacted"})
	}
	return out
}

func listOrNone(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

// ---- Tool selection ----

// selector evaluates comma-separated tool/group expressions (e.g., "all,-admin,net,-exec_sudo") left to right.
type selector struct {
	spec  string
	items []selItem
}

type selItem struct {
	remove bool
	name   string
}

const (
	selAll         = "all"
	selDestructive = "destructive" // tools that may modify the target
	selReadOnly    = "readonly"    // tools that do not modify the target
)

func parseSelector(spec string, known map[string]string) (selector, error) {
	s := selector{spec: spec}
	groups := map[string]bool{}
	for _, g := range known {
		groups[g] = true
	}
	for f := range strings.SplitSeq(spec, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		it := selItem{}
		if strings.HasPrefix(f, "-") || strings.HasPrefix(f, "+") {
			it.remove = f[0] == '-'
			f = f[1:]
		}
		it.name = f
		_, isTool := known[f]
		if known != nil && !isTool && !groups[f] && f != selAll && f != selDestructive && f != selReadOnly {
			return selector{}, fmt.Errorf("unknown tool or group %q", f)
		}
		s.items = append(s.items, it)
	}
	return s, nil
}

func (s selector) match(tool, group string, destructive bool) bool {
	on := false
	for _, it := range s.items {
		hit := it.name == selAll || it.name == tool || it.name == group ||
			(it.name == selDestructive && destructive) || (it.name == selReadOnly && !destructive)
		if hit {
			on = !it.remove
		}
	}
	return on
}

// ---- Paths ----

// CheckPath verifies that reading (write=false) or writing pth is permitted.
func (p *Policy) CheckPath(pth string, write bool) error {
	roots := p.readRoots
	kind := "read"
	if write {
		roots = p.writeRoots
		kind = "write"
	}
	if roots == nil {
		return nil
	}
	abs, err := filepath.Abs(orDefault(pth, "."))
	if err != nil {
		return err
	}
	real := resolveExisting(abs)
	for _, r := range roots {
		if within(real, r) {
			return nil
		}
	}
	return fmt.Errorf("policy: %s access to %s is not allowed (allowed: %s)", kind, abs, listOrNone(roots))
}

// resolveRoot converts r to a symlink-resolved absolute path.
func resolveRoot(r string) (string, error) {
	if r == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(r)
	if err != nil {
		return "", err
	}
	return resolveExisting(abs), nil
}

// resolveExisting resolves symlinks in the deepest existing prefix of abs and appends the remaining suffix,
// so not-yet-created paths are checked against their resolved parent directory.
func resolveExisting(abs string) string {
	rest := ""
	cur := filepath.Clean(abs)
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(abs)
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func within(p, root string) bool {
	if runtime.GOOS == "windows" {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ---- Commands ----

// CheckCommand verifies that all executed programs/shells and the full command line text are permitted.
func (p *Policy) CheckCommand(programs []string, text string) error {
	if len(p.allowCommands) > 0 {
		for _, prog := range programs {
			name := normalizeProgram(baseName(prog))
			if !slices.ContainsFunc(p.allowCommands, func(g string) bool {
				ok, _ := path.Match(g, name)
				return ok
			}) {
				return fmt.Errorf("policy: program %q is not allowed (allowed: %s). Shell command lines need the shell itself (sh, bash, cmd, powershell, pwsh) to be allowed; use argv to run an allowed program directly",
					name, strings.Join(p.allowCommands, ", "))
			}
		}
	}
	for _, re := range p.denyCommands {
		if re.MatchString(text) {
			return fmt.Errorf("policy: command matches denied pattern %q", re.String())
		}
	}
	return nil
}

// baseName returns the final path element across both '/' and '\' separators.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// normalizeProgram lowercases program names and strips executable extensions on Windows.
func normalizeProgram(name string) string {
	if runtime.GOOS != "windows" {
		return name
	}
	name = strings.ToLower(name)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return name
}

// ---- Environment variables ----

// FilterEnv removes environment variables (KEY=VALUE) matching UnsetEnv.
func (p *Policy) FilterEnv(env []string) []string {
	if len(p.unsetEnv) == 0 {
		return env
	}
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !p.envUnset(k) {
			out = append(out, kv)
		}
	}
	return out
}

// FiltersEnv reports whether FilterEnv removes anything.
func (p *Policy) FiltersEnv() bool { return len(p.unsetEnv) > 0 }

func (p *Policy) envUnset(name string) bool {
	for _, g := range p.unsetEnv {
		n := name
		if runtime.GOOS == "windows" {
			n, g = strings.ToUpper(n), strings.ToUpper(g)
		}
		if ok, _ := path.Match(g, n); ok {
			return true
		}
	}
	return false
}

// ---- Network ----

// hostRule matches an allowed network destination: a host glob or CIDR with optional port glob,
// or a "unix:" socket path glob.
type hostRule struct {
	spec string
	host string
	cidr *net.IPNet
	port string
	unix string
}

func parseHostRule(s string) (hostRule, error) {
	r := hostRule{spec: s}
	if p, ok := strings.CutPrefix(s, "unix:"); ok {
		r.unix = p
		return r, nil
	}
	host, port := s, ""
	if h, pt, err := net.SplitHostPort(s); err == nil {
		host, port = h, pt
	} else if strings.Count(s, ":") == 1 && !strings.Contains(s, "/") {
		return r, err
	}
	if _, n, err := net.ParseCIDR(host); err == nil {
		r.cidr = n
	} else {
		r.host = strings.ToLower(host)
		if _, err := path.Match(r.host, ""); err != nil {
			return r, err
		}
	}
	r.port = port
	return r, nil
}

// CheckHost verifies that connecting to host:port is permitted.
// Against a CIDR rule, every IP address that a hostname resolves to must fall within the subnet.
func (p *Policy) CheckHost(host string, port int) error {
	if len(p.allowHosts) == 0 {
		return nil
	}
	h := strings.ToLower(strings.Trim(host, "[]"))
	ps := strconv.Itoa(port)
	var ips []net.IP
	resolved := false
	for _, r := range p.allowHosts {
		if r.unix != "" {
			continue
		}
		if r.port != "" {
			if ok, _ := path.Match(r.port, ps); !ok {
				continue
			}
		}
		if r.cidr == nil {
			if ok, _ := path.Match(r.host, h); ok {
				return nil
			}
			continue
		}
		if !resolved {
			resolved = true
			if ip := net.ParseIP(h); ip != nil {
				ips = []net.IP{ip}
			} else {
				ips, _ = net.LookupIP(h)
			}
		}
		if len(ips) > 0 && !slices.ContainsFunc(ips, func(ip net.IP) bool { return !r.cidr.Contains(ip) }) {
			return nil
		}
	}
	return fmt.Errorf("policy: connection to %s is not allowed (allowed: %s)", net.JoinHostPort(h, ps), p.hostList())
}

// CheckUnixSocket checks whether connecting to a Unix domain socket is allowed.
func (p *Policy) CheckUnixSocket(sock string) error {
	if len(p.allowHosts) == 0 {
		return nil
	}
	for _, r := range p.allowHosts {
		if r.unix != "" {
			if ok, _ := path.Match(r.unix, sock); ok {
				return nil
			}
		}
	}
	return fmt.Errorf("policy: connection to unix socket %s is not allowed (allowed: %s)", sock, p.hostList())
}

func (p *Policy) hostList() string {
	var s []string
	for _, r := range p.allowHosts {
		s = append(s, r.spec)
	}
	return strings.Join(s, ", ")
}

// ---- Redaction ----

type redaction struct {
	re   *regexp.Regexp
	repl string
}

// builtinRedactions returns regular expressions for common secret formats, preserving key names
// and delimiters while masking secret values.
func builtinRedactions() []redaction {
	r := func(expr, repl string) redaction { return redaction{re: regexp.MustCompile(expr), repl: repl} }
	return []redaction{
		r(`-----BEGIN ([A-Z0-9 ]*)PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`, "-----BEGIN ${1}PRIVATE KEY-----[REDACTED]-----END ${1}PRIVATE KEY-----"),
		r(`(?i)(authorization:\s*(?:bearer|basic|token)\s+)[^\s"']+`, "${1}[REDACTED]"),
		r(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`, "[REDACTED]"),
		r(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`, "[REDACTED]"),
		r(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`, "[REDACTED]"),
		r(`\bxox[abposr]-[A-Za-z0-9-]{10,}\b`, "[REDACTED]"),
		r(`\bsk-[A-Za-z0-9_-]{20,}\b`, "[REDACTED]"),
		r(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`, "[REDACTED]"),
		r(`(?i)\b([A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)["']?\s*[:=]\s*["']?)[^\s"',;&]+`, "${1}[REDACTED]"),
		r(`(?i)\b([a-z][a-z0-9+.-]*://[^\s:/@]+:)[^\s@/]+@`, "${1}[REDACTED]@"),
	}
}

// AddSecret masks session credentials independently of optional redaction.
func (p *Policy) AddSecret(secret string) {
	if secret != "" {
		p.redact = append(p.redact, redaction{re: regexp.MustCompile(regexp.QuoteMeta(secret)), repl: "[REDACTED]"})
	}
}

// Redacting reports whether any redaction is configured.
func (p *Policy) Redacting() bool { return len(p.redact) > 0 }

// RedactString masks the secrets in s.
func (p *Policy) RedactString(s string) string {
	for _, r := range p.redact {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
