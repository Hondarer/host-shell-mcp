package policy

import (
	"fmt"
	"strconv"
	"strings"
)

// PortRange is an inclusive range of port numbers.
type PortRange struct{ Lo, Hi int }

// Ports limits the ports remote forwards listen on (--allow-forward-remote-port, like ssh's
// PermitListen); nil permits any port.
type Ports []PortRange

// ParsePorts parses PORT or PORT-PORT values, each of which may be comma-separated.
func ParsePorts(values []string) (Ports, error) {
	var out Ports
	for _, v := range values {
		for f := range strings.SplitSeq(v, ",") {
			f = strings.TrimSpace(f)
			lo, hi, isRange := strings.Cut(f, "-")
			r := PortRange{port(lo), port(lo)}
			if isRange {
				r.Hi = port(hi)
			}
			if r.Lo == 0 || r.Hi == 0 || r.Lo > r.Hi {
				return nil, fmt.Errorf("invalid --allow-forward-remote-port %q: want PORT or PORT-PORT with ports 1 to 65535 in ascending order", f)
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// port returns 0 unless s is a port number from 1 to 65535.
func port(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != s {
		return 0
	}
	return n
}

// Contains reports whether p permits port; nil permits any port.
func (p Ports) Contains(port int) bool {
	if p == nil {
		return true
	}
	for _, r := range p {
		if port >= r.Lo && port <= r.Hi {
			return true
		}
	}
	return false
}

// String lists the ranges in the order given, as "15432, 20000-20099".
func (p Ports) String() string {
	parts := make([]string, len(p))
	for i, r := range p {
		parts[i] = strconv.Itoa(r.Lo)
		if r.Hi != r.Lo {
			parts[i] += "-" + strconv.Itoa(r.Hi)
		}
	}
	return strings.Join(parts, ", ")
}
