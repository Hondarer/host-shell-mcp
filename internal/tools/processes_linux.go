package tools

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// clockTicks is the kernel USER_HZ value used for start times in /proc/<pid>/stat (100 on all Go-supported Linux architectures).
const clockTicks = 100

// procStat holds the parsed fields of /proc/<pid>/stat.
type procStat struct {
	name  string
	ppid  int
	state string
	rest  []string // the fields after comm, starting with state
}

// readStat parses /proc/<pid>/stat. comm in "pid (comm) state ppid ..." may contain spaces and
// parentheses, so the remaining fields are split after the last ")".
func readStat(pid int) (procStat, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, false // the process has exited meanwhile
	}
	s := string(b)
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return procStat{}, false
	}
	f := strings.Fields(s[end+1:])
	if len(f) < 2 {
		return procStat{}, false
	}
	st := procStat{name: s[open+1 : end], state: f[0], rest: f}
	st.ppid, _ = strconv.Atoi(f[1])
	return st, true
}

// procPIDs lists the PIDs present in /proc.
func procPIDs() ([]int, error) {
	dirs, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(dirs))
	for _, dir := range dirs {
		if pid, err := strconv.Atoi(filepath.Base(dir)); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// listProcesses reads all processes from /proc.
func listProcesses() ([]Process, error) {
	boot := bootTime()
	users := map[string]string{}
	pids, err := procPIDs()
	if err != nil {
		return nil, err
	}
	var out []Process
	for _, pid := range pids {
		st, ok := readStat(pid)
		if !ok || len(st.rest) < 20 {
			continue
		}
		dir := "/proc/" + strconv.Itoa(pid)
		p := Process{PID: pid, Name: st.name, State: st.state, PPID: st.ppid}
		if ticks, err := strconv.ParseInt(st.rest[19], 10, 64); err == nil && !boot.IsZero() {
			p.StartTime = boot.Add(time.Duration(ticks) * time.Second / clockTicks).Format(time.RFC3339)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
			p.CommandLine = strings.TrimSpace(string(bytes.ReplaceAll(b, []byte{0}, []byte{' '})))
		}
		if b, err := os.ReadFile(filepath.Join(dir, "statm")); err == nil {
			if f := strings.Fields(string(b)); len(f) > 1 {
				rss, _ := strconv.ParseUint(f[1], 10, 64)
				p.MemoryBytes = rss * uint64(os.Getpagesize())
			}
		}
		if b, err := os.ReadFile(filepath.Join(dir, "status")); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if f := strings.Fields(line); len(f) > 1 && f[0] == "Uid:" {
					p.User = userName(users, f[1])
					break
				}
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// userName resolves a numeric UID to a username, falling back to uid.
func userName(cache map[string]string, uid string) string {
	if n, ok := cache[uid]; ok {
		return n
	}
	n := uid
	if u, err := user.LookupId(uid); err == nil {
		n = u.Username
	}
	cache[uid] = n
	return n
}

// bootTime reads the system boot time from the btime entry in /proc/stat.
func bootTime() time.Time {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(sec, 0)
			}
		}
	}
	return time.Time{}
}
