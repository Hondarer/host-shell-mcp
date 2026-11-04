//go:build windows

package proc

import (
	"errors"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func childrenOf(t *testing.T, pid uint32) []uint32 {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snap)
	var out []uint32
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ParentProcessID == pid {
			out = append(out, e.ProcessID)
		}
	}
	return out
}

func alive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
}

func TestSignalGroupTerminatesDescendants(t *testing.T) {
	// cmd starts a grandchild (ping) and waits for it.
	cmd := exec.Command("cmd.exe", "/d", "/c", "ping -n 60 127.0.0.1 > NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	root := uint32(cmd.Process.Pid)
	var kids []uint32
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if kids = childrenOf(t, root); len(kids) > 0 {
			break
		}
	}
	if len(kids) == 0 {
		t.Fatal("cmd started no child")
	}
	if err := SignalGroup(cmd.Process.Pid, "kill"); err != nil {
		t.Fatalf("SignalGroup: %v", err)
	}
	cmd.Wait()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		left := 0
		for _, k := range kids {
			if alive(k) {
				left++
			}
		}
		if left == 0 {
			return
		}
	}
	t.Fatalf("descendants %v still running", kids)
}

func TestSignalNoProcessAndUnsupported(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := Signal(cmd.Process.Pid, "kill"); !errors.Is(err, errNoProcess) {
		t.Errorf("Signal on an exited process = %v, want errNoProcess", err)
	}
	if err := SignalGroup(cmd.Process.Pid, "term"); err == nil {
		t.Error("SignalGroup with term succeeded, want an error")
	}
}
