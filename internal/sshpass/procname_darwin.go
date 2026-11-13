package sshpass

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processName(pid int) string {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ""
	}
	b := kp.Proc.P_comm[:]
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

// processStart returns the start time of pid, which tells a reused process ID from the earlier one.
func processStart(pid int) string {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d", kp.Proc.P_starttime.Sec, kp.Proc.P_starttime.Usec)
}
