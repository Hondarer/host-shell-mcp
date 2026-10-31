package policy

import (
	"errors"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
)

// NameRules match requested destination names on the bridge: a host glob with an optional
// port glob, case-insensitive. CIDR and unix: rules are rejected because the bridge cannot
// see how the peer resolves the name; the rules constrain names, not resolved IP ranges.
type NameRules []string

// ParseNameRules validates rules; option names the flag in error messages.
func ParseNameRules(option string, rules []string) (NameRules, error) {
	for _, r := range rules {
		if r == "" || strings.ContainsAny(r, "/\r\n\t @") || strings.HasPrefix(r, "unix:") {
			return nil, fmt.Errorf("invalid %s rule %q", option, r)
		}
		host, port := SplitNameRule(r)
		if host == "" || strings.HasSuffix(r, ":") {
			return nil, fmt.Errorf("empty host or port in %s rule %q", option, r)
		}
		if _, e := path.Match(host, "example.com"); e != nil {
			return nil, fmt.Errorf("invalid host glob in %s rule %q", option, r)
		}
		if port != "" {
			if _, e := path.Match(port, "443"); e != nil {
				return nil, errors.New("invalid port glob in " + option + " rule " + strconv.Quote(r))
			}
		}
	}
	return append(NameRules{}, rules...), nil
}

// SplitNameRule splits HOST[:PORT], accepting bracketed IPv6 hosts.
func SplitNameRule(r string) (string, string) {
	if h, p, e := net.SplitHostPort(r); e == nil {
		return strings.Trim(h, "[]"), p
	}
	if strings.Count(r, ":") == 1 {
		h, p, _ := strings.Cut(r, ":")
		return h, p
	}
	return strings.Trim(r, "[]"), ""
}

// Allow reports whether any rule matches host and port.
func (rules NameRules) Allow(host string, port int) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	for _, r := range rules {
		h, p := SplitNameRule(r)
		hm, _ := path.Match(strings.ToLower(h), host)
		pm, _ := path.Match(p, strconv.Itoa(port))
		if hm && (p == "" || pm) {
			return true
		}
	}
	return false
}
