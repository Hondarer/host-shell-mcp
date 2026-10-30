//go:build !linux && !windows

package tools

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// lstartLayout is the format of the lstart column of ps, in local time ("Sat Oct 31 03:04:05 2026").
const lstartLayout = "Mon Jan 2 15:04:05 2006"

// listProcesses reads ps on systems without /proc (macOS, where host-shell runs as the local bridge).
func listProcesses() ([]Process, error) {
	out, err := exec.Command("ps", "-axww", "-o", "pid=,ppid=,user=,state=,rss=,lstart=,args=").Output()
	if err != nil {
		return nil, err
	}
	var list []Process
	for _, line := range strings.Split(string(out), "\n") {
		// pid ppid user state rss, then lstart in five fields, then the command line.
		f := strings.Fields(line)
		if len(f) < 11 {
			continue
		}
		p := Process{User: f[2], State: f[3], CommandLine: strings.Join(f[10:], " "), Name: filepath.Base(f[10])}
		if t, err := time.ParseInLocation(lstartLayout, strings.Join(f[5:10], " "), time.Local); err == nil {
			p.StartTime = t.Format(time.RFC3339)
		}
		p.PID, _ = strconv.Atoi(f[0])
		p.PPID, _ = strconv.Atoi(f[1])
		if kb, err := strconv.ParseUint(f[4], 10, 64); err == nil {
			p.MemoryBytes = kb * 1024
		}
		list = append(list, p)
	}
	return list, nil
}
