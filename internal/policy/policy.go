// Package policy は host-shell の利用者が起動時に指定する制限を表します。
//
// 既定は制限なしです。オプションで指定したものだけを制限します。
// ここでの検査は、ツールの引数に対するものです。exec_run などでコマンドを実行できる場合、
// コマンドはファイルやネットワークに自由に触れられるため、厳密に制限するには、
// コマンド実行のツールの組 (exec) を --tools で外します。
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

// Options は起動時のオプションから作る制限の指定です。
type Options struct {
	// Tools は公開するツールの指定です (例: "all,-admin,-sudo_run")。空の場合は all です。
	Tools string
	// ReadOnly は接続先を変更するツールを公開しないことを示します。
	ReadOnly bool
	// Confirm は実行の前に利用者の確認 (MCP の elicitation) を求めるツールの指定です。
	Confirm string
	// AllowPaths はファイルの読み書きを許す場所です。空の場合は制限しません。
	AllowPaths []string
	// AllowReadPaths はファイルの読み取りだけを許す場所です。
	AllowReadPaths []string
	// AllowCommands は実行を許すプログラムの名前 (glob) です。空の場合は制限しません。
	AllowCommands []string
	// DenyCommands は実行を拒むコマンド行の正規表現です。
	DenyCommands []string
	// AllowHosts はネットワークのツールが接続してよい宛先です。空の場合は制限しません。
	AllowHosts []string
	// UnsetEnv は子プロセスに渡さない環境変数の名前 (glob) です。
	UnsetEnv []string
	// Redact は、出力に含まれる秘密情報 (鍵、トークン、パスワードの類) を伏せ字にすることを示します。
	Redact bool
	// RedactPatterns は伏せ字にする追加の正規表現です。
	RedactPatterns []string
}

// Policy は検査に使う形に変換した制限です。ゼロ値は制限なしです。
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

// New はオプションを検査して Policy を作ります。known はツールの名前から組の名前への対応です。
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
		// 読み取りだけの場所を指定した場合、書き込みはどこにも許さない。
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

// Unrestricted は制限のない Policy を返します。
func Unrestricted() *Policy {
	p, _ := New(Options{}, nil)
	return p
}

// Enabled はツールを公開するかを返します。
func (p *Policy) Enabled(tool, group string, readOnlyTool bool) bool {
	return p.tools.match(tool, group, !readOnlyTool)
}

// NeedsConfirm はツールの実行の前に利用者の確認が必要かを返します。
func (p *Policy) NeedsConfirm(tool, group string, readOnlyTool bool) bool {
	return p.confirm.match(tool, group, !readOnlyTool)
}

// Summary は制限の内容を、項目と値の組で返します。制限のない項目は含めません。
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

// ---- ツールの選択 ----

// selector は "all,-admin,net,-sudo_run" のような指定です。左から順に適用します。
type selector struct {
	spec  string
	items []selItem
}

type selItem struct {
	remove bool
	name   string
}

// 組の名前のほかに使える特別な名前です。
const (
	selAll         = "all"
	selDestructive = "destructive" // 接続先を変更しうるツール
	selReadOnly    = "readonly"    // 接続先を変更しないツール
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

// ---- パス ----

// CheckPath は path の読み取り (write が false) または書き込みが許されるかを調べます。
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

// resolveRoot は許可する場所を、シンボリック リンクを解決した絶対パスにします。
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

// resolveExisting は、存在する部分までのシンボリック リンクを解決し、残りをつなげます。
// 作成前のファイルでも、その親のリンクを解決して判定するためです。
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

// ---- コマンド ----

// CheckCommand は programs (起動するプログラムやシェル) と、コマンド行の全体 text が許されるかを調べます。
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

// baseName は OS によらずパスの最後の要素を返します。
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// normalizeProgram は Windows では小文字にし、実行ファイルの拡張子を除きます。
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

// ---- 環境変数 ----

// FilterEnv は env (KEY=VALUE の並び) から、渡さない環境変数を除きます。
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

// FiltersEnv は FilterEnv が何かを除く設定かを返します。
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

// ---- ネットワーク ----

// hostRule は宛先の許可です。host は glob か CIDR、port は空 (すべて) か番号の glob です。
// "unix:" で始まるものは Unix ドメイン ソケットのパスの glob です。
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

// CheckHost は host:port への接続が許されるかを調べます。
// 名前を CIDR の規則と照合する場合は、名前を解決したすべてのアドレスが範囲に入ることを求めます。
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

// CheckUnixSocket は Unix ドメイン ソケットへの接続が許されるかを調べます。
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

// ---- 伏せ字 ----

type redaction struct {
	re   *regexp.Regexp
	repl string
}

// builtinRedactions は、よくある秘密情報の形です。
// 名前と区切りを残し、値だけを伏せます。
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

// Redacting は伏せ字の設定があるかを返します。
func (p *Policy) Redacting() bool { return len(p.redact) > 0 }

// RedactString は s の秘密情報を伏せ字にします。
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
