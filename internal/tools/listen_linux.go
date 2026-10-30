package tools

import (
	"bufio"
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listeners reads listening sockets from /proc/net and finds their processes through /proc/<pid>/fd.
func listeners(context.Context) ([]Listener, string, error) {
	owners := socketOwners()
	var out []Listener
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		f, err := os.Open("/proc/net/" + proto)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 {
				continue
			}
			state := fields[3]
			// TCP sockets in LISTEN (0A) and unconnected UDP sockets (07) count as listening.
			if (strings.HasPrefix(proto, "tcp") && state != "0A") || (strings.HasPrefix(proto, "udp") && state != "07") {
				continue
			}
			ip, port, ok := parseProcAddr(fields[1])
			if !ok {
				continue
			}
			l := Listener{Protocol: proto, Address: ip, Port: port}
			if o, ok := owners[fields[9]]; ok {
				l.PID, l.Process = o.pid, o.name
			}
			out = append(out, l)
		}
		f.Close()
	}
	note := ""
	if os.Geteuid() != 0 {
		note = "processes of other users are not shown (pid empty) unless host-shell runs as root"
	}
	return out, note, nil
}

// parseProcAddr reads an address of the form "0100007F:1F90" from /proc/net/tcp.
// Each 32-bit word of the address is in host byte order (little endian).
func parseProcAddr(s string) (string, int, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, false
	}
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return "", 0, false
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return "", 0, false
	}
	return net.IP(b).String(), int(port), true
}

type sockOwner struct {
	pid  int
	name string
}

// socketOwners maps socket inodes to the processes that have them open.
func socketOwners() map[string]sockOwner {
	out := map[string]sockOwner{}
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	names := map[string]string{}
	for _, fd := range fds {
		link, err := os.Readlink(fd)
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
		pidDir := filepath.Dir(filepath.Dir(fd))
		pid, _ := strconv.Atoi(filepath.Base(pidDir))
		name, ok := names[pidDir]
		if !ok {
			b, _ := os.ReadFile(filepath.Join(pidDir, "comm"))
			name = strings.TrimSpace(string(b))
			names[pidDir] = name
		}
		if _, exists := out[inode]; !exists {
			out[inode] = sockOwner{pid: pid, name: name}
		}
	}
	return out
}
