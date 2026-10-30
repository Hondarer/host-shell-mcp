//go:build !linux && !windows

package tools

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// listProcesses reads ps on systems without /proc (macOS, where host-shell runs as the local bridge).
func listProcesses() ([]Process, error) {
	out, err := exec.Command("ps", "-axww", "-o", "pid=,ppid=,user=,state=,rss=,args=").Output()
	if err != nil {
		return nil, err
	}
	var list []Process
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		p := Process{User: f[2], State: f[3], CommandLine: strings.Join(f[5:], " "), Name: filepath.Base(f[5])}
		p.PID, _ = strconv.Atoi(f[0])
		p.PPID, _ = strconv.Atoi(f[1])
		if kb, err := strconv.ParseUint(f[4], 10, 64); err == nil {
			p.MemoryBytes = kb * 1024
		}
		list = append(list, p)
	}
	return list, nil
}
