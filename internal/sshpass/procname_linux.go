package sshpass

import (
	"os"
	"strconv"
	"strings"
)

func processName(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// processStart returns the start time of pid (in clock ticks since boot), which tells a reused
// process ID from the earlier process.
func processStart(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return ""
	}
	f := strings.Fields(s[i+1:]) // fields from 3 (state) on
	if len(f) < 20 {
		return ""
	}
	return f[19] // field 22: starttime
}
