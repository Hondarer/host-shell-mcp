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

// listProcesses reads all processes from /proc.
func listProcesses() ([]Process, error) {
	boot := bootTime()
	users := map[string]string{}
	dirs, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return nil, err
	}
	var out []Process
	for _, dir := range dirs {
		pid, err := strconv.Atoi(filepath.Base(dir))
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			continue // the process has exited meanwhile
		}
		// "pid (comm) state ppid ...": comm may contain spaces and parentheses.
		s := string(stat)
		open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if open < 0 || end < open {
			continue
		}
		f := strings.Fields(s[end+1:])
		if len(f) < 20 {
			continue
		}
		p := Process{PID: pid, Name: s[open+1 : end], State: f[0]}
		p.PPID, _ = strconv.Atoi(f[1])
		if ticks, err := strconv.ParseInt(f[19], 10, 64); err == nil && !boot.IsZero() {
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
