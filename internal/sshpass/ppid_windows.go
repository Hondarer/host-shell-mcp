package sshpass

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processes maps process IDs to their parent and lower-case executable name.
type procEntry struct {
	parent uint32
	exe    string
}

func processes() map[uint32]procEntry {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	procs := map[uint32]procEntry{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		procs[e.ProcessID] = procEntry{e.ParentProcessID, strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))}
	}
	return procs
}

// stopSSH terminates the ssh process pid, so that it does not go on to send an empty password
// (which the server would count as another failure). It does nothing unless pid is ssh.exe.
func stopSSH(pid int) bool {
	if p, ok := processes()[uint32(pid)]; !ok || p.exe != "ssh.exe" {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 255) == nil
}

// sshProcessID identifies the ssh process that started the helper. Cygwin-based ssh (Git for
// Windows) emulates fork with a new process for every prompt, so the direct parent differs each
// time; the outermost of the consecutive ssh.exe ancestors is the ssh that the user started.
func sshProcessID() int {
	procs := processes()
	self, ok := procs[uint32(os.Getpid())]
	if !ok {
		return os.Getppid()
	}
	id := self.parent
	for range 8 {
		p, ok := procs[id]
		if !ok || p.exe != "ssh.exe" {
			break
		}
		if q, ok := procs[p.parent]; !ok || q.exe != "ssh.exe" {
			break
		}
		id = p.parent
	}
	return int(id)
}

// processStart returns the creation time of pid, which tells a reused process ID from the earlier one.
func processStart(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return ""
	}
	return fmt.Sprintf("%d", c.Nanoseconds())
}

// processName returns the executable name of pid without .exe.
func processName(pid int) string {
	return strings.TrimSuffix(processes()[uint32(pid)].exe, ".exe")
}
