package server

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

type procOut struct {
	Processes []struct {
		PID         int    `json:"pid"`
		PPID        int    `json:"ppid"`
		Name        string `json:"name"`
		CommandLine string `json:"command_line"`
		User        string `json:"user"`
		StartTime   string `json:"start_time"`
		MemoryBytes uint64 `json:"memory_bytes"`
	} `json:"processes"`
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

func TestProcList(t *testing.T) {
	cs := connect(t)
	child := exec.Command("sleep", "30")
	if runtime.GOOS == "windows" {
		child = exec.Command("ping", "-n", "30", "127.0.0.1")
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()

	out := call[procOut](t, cs, "proc_list", map[string]any{"pid": os.Getpid()})
	if len(out.Processes) < 2 {
		t.Fatalf("got %+v", out)
	}
	var self, kid bool
	for _, p := range out.Processes {
		switch p.PID {
		case os.Getpid():
			self = p.PPID == os.Getppid() && p.User != "" && p.StartTime != "" && p.MemoryBytes > 0 && p.CommandLine != ""
		case child.Process.Pid:
			kid = p.PPID == os.Getpid()
		}
	}
	if !self || !kid {
		t.Errorf("self %v, child %v: %+v", self, kid, out.Processes)
	}
	name := "sleep"
	if runtime.GOOS == "windows" {
		name = "ping"
	}
	out = call[procOut](t, cs, "proc_list", map[string]any{"filter": name, "pid": os.Getpid()})
	if len(out.Processes) != 1 || out.Processes[0].PID != child.Process.Pid {
		t.Errorf("filter: %+v", out.Processes)
	}
	out = call[procOut](t, cs, "proc_list", map[string]any{"max": 1})
	if len(out.Processes) != 1 || !out.Truncated || out.Total < 2 {
		t.Errorf("max: %+v", out)
	}
}
