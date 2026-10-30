package tools

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// processMemoryCounters is PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// processCommandLineInformation is the ProcessCommandLineInformation class of NtQueryInformationProcess.
const processCommandLineInformation = 60

// listProcesses enumerates processes via a Toolhelp snapshot, populating command line, user,
// start time, and working set when PROCESS_QUERY_LIMITED_INFORMATION access is granted.
func listProcesses() ([]Process, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	users := map[string]string{}
	var out []Process
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		p := Process{PID: int(e.ProcessID), PPID: int(e.ParentProcessID), Name: windows.UTF16ToString(e.ExeFile[:])}
		processDetails(&p, users)
		out = append(out, p)
	}
	return out, nil
}

func processDetails(p *Process, users map[string]string) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.PID))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)

	buf := make([]byte, 4096)
	for {
		var n uint32
		err := windows.NtQueryInformationProcess(h, processCommandLineInformation, unsafe.Pointer(&buf[0]), uint32(len(buf)), &n)
		if err == nil {
			s := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
			p.CommandLine = s.String()
			break
		}
		if n <= uint32(len(buf)) || n > 1<<20 {
			break
		}
		buf = make([]byte, n)
	}

	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) == nil && created.Nanoseconds() > 0 {
		p.StartTime = time.Unix(0, created.Nanoseconds()).Format(time.RFC3339)
	}
	var mc processMemoryCounters
	mc.cb = uint32(unsafe.Sizeof(mc))
	if r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.cb)); r != 0 {
		p.MemoryBytes = uint64(mc.WorkingSetSize)
	}

	var token windows.Token
	if windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token) != nil {
		return
	}
	defer token.Close()
	tu, err := token.GetTokenUser()
	if err != nil {
		return
	}
	sid := tu.User.Sid.String()
	name, ok := users[sid]
	if !ok {
		if account, domain, _, err := tu.User.Sid.LookupAccount(""); err == nil {
			name = domain + `\` + account
		} else {
			name = sid
		}
		users[sid] = name
	}
	p.User = name
}
