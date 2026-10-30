package tools

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// ---- proc_kill ----

func killProcess(_ context.Context, _ *Deps, pid int, sig string, tree, sudo bool) ([]int, error) {
	if sudo {
		return nil, errors.New("sudo is not available on Windows; run host-shell in an elevated session to kill other users' processes")
	}
	if err := proc.Signal(pid, sig, tree); err != nil {
		return nil, err
	}
	return []int{pid}, nil
}

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
}

// ---- svc_list / svc_control ----

// connectSCM opens the Service Control Manager with read/enumerate rights so non-admin users can list and query services
// (mgr.Connect requests all access rights and fails without elevation).
func connectSCM() (*mgr.Mgr, error) {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, fmt.Errorf("open service control manager: %w", err)
	}
	return &mgr.Mgr{Handle: h}, nil
}

func openService(m *mgr.Mgr, name string, access uint32) (*mgr.Service, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.OpenService(m.Handle, p, access)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, fmt.Errorf("service %q not found (use the service name, not the display name; see svc_list)", name)
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, fmt.Errorf("access denied to service %q (changing services needs an elevated session)", name)
		}
		return nil, err
	}
	return &mgr.Service{Name: name, Handle: h}, nil
}

var svcStates = map[svc.State]string{
	svc.Stopped: "stopped", svc.StartPending: "starting", svc.StopPending: "stopping", svc.Running: "running",
	svc.ContinuePending: "starting", svc.PausePending: "stopping", svc.Paused: "paused",
}

func startModeName(c mgr.Config) string {
	switch c.StartType {
	case mgr.StartAutomatic:
		if c.DelayedAutoStart {
			return "delayed-auto"
		}
		return "auto"
	case mgr.StartManual:
		return "manual"
	case mgr.StartDisabled:
		return "disabled"
	case windows.SERVICE_BOOT_START:
		return "boot"
	case windows.SERVICE_SYSTEM_START:
		return "system"
	}
	return strconv.Itoa(int(c.StartType))
}

func listServices(_ context.Context, _ *Deps, user bool) ([]Service, error) {
	if user {
		return nil, errors.New("user applies to systemd on Linux")
	}
	m, err := connectSCM()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()
	var needed, count, resume uint32
	var buf []byte
	for {
		var p *byte
		if len(buf) > 0 {
			p = &buf[0]
		}
		err = windows.EnumServicesStatusEx(m.Handle, windows.SC_ENUM_PROCESS_INFO, windows.SERVICE_WIN32,
			windows.SERVICE_STATE_ALL, p, uint32(len(buf)), &needed, &count, &resume, nil)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_MORE_DATA) {
			return nil, err
		}
		buf = make([]byte, needed)
		resume = 0
	}
	var out []Service
	if count == 0 {
		return out, nil
	}
	entries := unsafe.Slice((*windows.ENUM_SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0])), count)
	for _, e := range entries {
		s := Service{
			Name:        windows.UTF16PtrToString(e.ServiceName),
			Description: windows.UTF16PtrToString(e.DisplayName),
			State:       svcStates[svc.State(e.ServiceStatusProcess.CurrentState)],
			PID:         int(e.ServiceStatusProcess.ProcessId),
		}
		if sv, err := openService(m, s.Name, windows.SERVICE_QUERY_CONFIG); err == nil {
			if c, err := sv.Config(); err == nil {
				s.StartMode = startModeName(c)
			}
			sv.Close()
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Service) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}

func controlService(ctx context.Context, _ *Deps, in SvcControlInput, action string) (SvcControlOutput, error) {
	var out SvcControlOutput
	if in.User || in.Sudo {
		return out, errors.New("user and sudo apply to Linux only")
	}
	m, err := connectSCM()
	if err != nil {
		return out, err
	}
	defer m.Disconnect()
	access := uint32(windows.SERVICE_QUERY_STATUS | windows.SERVICE_QUERY_CONFIG)
	switch action {
	case "start":
		access |= windows.SERVICE_START
	case "stop":
		access |= windows.SERVICE_STOP
	case "restart":
		access |= windows.SERVICE_START | windows.SERVICE_STOP
	case "enable", "disable":
		access |= windows.SERVICE_CHANGE_CONFIG
	case "reload":
		return out, errors.New("reload is not supported for Windows services; use restart")
	}
	s, err := openService(m, in.Name, access)
	if err != nil {
		return out, err
	}
	defer s.Close()
	wait := time.Duration(orDefaultInt(in.WaitSec, 30)) * time.Second
	switch action {
	case "start":
		err = startService(ctx, s, wait)
	case "stop":
		err = stopService(ctx, s, wait)
	case "restart":
		if err = stopService(ctx, s, wait); err == nil {
			err = startService(ctx, s, wait)
		}
	case "enable", "disable":
		var c mgr.Config
		if c, err = s.Config(); err == nil {
			if action == "enable" {
				if c.StartType == mgr.StartDisabled || c.StartType == mgr.StartManual {
					c.StartType = mgr.StartAutomatic
				}
			} else {
				c.StartType = mgr.StartDisabled
			}
			err = s.UpdateConfig(c)
		}
	}
	if err != nil {
		return out, fmt.Errorf("%s %s: %w", action, in.Name, err)
	}
	st, err := s.Query()
	if err != nil {
		return out, err
	}
	out.Service = Service{Name: in.Name, State: svcStates[st.State], PID: int(st.ProcessId)}
	out.Properties = map[string]string{"ExitCode": strconv.Itoa(int(st.Win32ExitCode))}
	if c, err := s.Config(); err == nil {
		out.Service.Description = c.DisplayName
		out.Service.StartMode = startModeName(c)
		out.Properties["BinaryPath"] = c.BinaryPathName
		out.Properties["Account"] = c.ServiceStartName
		out.Properties["Description"] = c.Description
		if len(c.Dependencies) > 0 {
			out.Properties["Dependencies"] = strings.Join(c.Dependencies, ", ")
		}
	}
	return out, nil
}

func waitServiceState(ctx context.Context, s *mgr.Service, want svc.State, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("still %s after %s", svcStates[st.State], wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func startService(ctx context.Context, s *mgr.Service, wait time.Duration) error {
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return waitServiceState(ctx, s, svc.Running, wait)
}

func stopService(ctx context.Context, s *mgr.Service, wait time.Duration) error {
	if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return err
	}
	return waitServiceState(ctx, s, svc.Stopped, wait)
}

// ---- log_query ----

var eventLevels = map[string]int{"critical": 1, "error": 2, "warning": 3, "information": 4, "info": 4, "verbose": 5}
var eventLevelNames = map[int]string{0: "information", 1: "critical", 2: "error", 3: "warning", 4: "information", 5: "verbose"}

// event is one event of wevtutil's RenderedXml.
type event struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     int `xml:"EventID"`
		Level       int `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
		Execution struct {
			ProcessID int `xml:"ProcessID,attr"`
		} `xml:"Execution"`
	} `xml:"System"`
	EventData struct {
		Data []string `xml:"Data"`
	} `xml:"EventData"`
	RenderingInfo struct {
		Message string `xml:"Message"`
		Level   string `xml:"Level"`
	} `xml:"RenderingInfo"`
}

func queryLog(ctx context.Context, d *Deps, in LogQueryInput) ([]LogEntry, error) {
	if in.User || in.Sudo {
		return nil, errors.New("user and sudo apply to Linux only")
	}
	var conds []string
	if in.Priority != "" {
		lv, ok := eventLevels[strings.ToLower(in.Priority)]
		if !ok {
			return nil, fmt.Errorf("unknown priority %q (use critical, error, warning, information or verbose)", in.Priority)
		}
		var ors []string
		for l := 1; l <= lv; l++ {
			ors = append(ors, fmt.Sprintf("Level=%d", l))
		}
		if lv >= 4 {
			ors = append(ors, "Level=0")
		}
		conds = append(conds, "("+strings.Join(ors, " or ")+")")
	}
	if in.Unit != "" {
		conds = append(conds, fmt.Sprintf("Provider[@Name='%s']", strings.ReplaceAll(in.Unit, "'", "")))
	}
	if in.EventID != 0 {
		conds = append(conds, fmt.Sprintf("EventID=%d", in.EventID))
	}
	if in.Since != "" {
		t, err := parseSince(in.Since)
		if err != nil {
			return nil, err
		}
		conds = append(conds, fmt.Sprintf("TimeCreated[@SystemTime>='%s']", t.UTC().Format("2006-01-02T15:04:05.000Z")))
	}
	if in.Until != "" {
		t, err := parseSince(in.Until)
		if err != nil {
			return nil, err
		}
		conds = append(conds, fmt.Sprintf("TimeCreated[@SystemTime<='%s']", t.UTC().Format("2006-01-02T15:04:05.000Z")))
	}
	query := "*"
	if len(conds) > 0 {
		query = "*[System[" + strings.Join(conds, " and ") + "]]"
	}
	var grep *regexp.Regexp
	count := in.Lines
	if in.Grep != "" {
		var err error
		if grep, err = regexp.Compile("(?i)" + in.Grep); err != nil {
			return nil, fmt.Errorf("grep: %w", err)
		}
		count = min(in.Lines*20, 20000)
	}
	argv := []string{"wevtutil", "qe", orDefault(in.Log, "System"), "/q:" + query, "/c:" + strconv.Itoa(count), "/rd:true", "/f:RenderedXml", "/e:Events"}
	// wevtutil output encoding varies (UTF-16, UTF-8, or console code page), so capture raw bytes and auto-detect.
	res, err := proc.Run(ctx, d.procSpec(CommandSpec{Argv: argv}), proc.Options{
		Timeout:   d.Cfg.DefaultTimeout,
		MaxOutput: 64 << 20,
	})
	if err != nil {
		return nil, err
	}
	res.Stdout, res.Stderr = decodeToolOutput(res.Stdout), decodeToolOutput(res.Stderr)
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("wevtutil failed (exit %d): %s", res.ExitCode, strings.TrimSpace(res.Stderr+res.Stdout))
	}
	var doc struct {
		Events []event `xml:"Event"`
	}
	// Strip the XML declaration before unmarshaling the UTF-8 body.
	body := res.Stdout
	if i := strings.Index(body, "?>"); strings.HasPrefix(strings.TrimSpace(body), "<?xml") && i >= 0 {
		body = body[i+2:]
	}
	if err := xml.Unmarshal([]byte(body), &doc); err != nil {
		return nil, fmt.Errorf("parse wevtutil output: %w", err)
	}
	var out []LogEntry
	for _, e := range doc.Events {
		msg := strings.TrimSpace(e.RenderingInfo.Message)
		if msg == "" {
			msg = strings.Join(e.EventData.Data, "; ")
		}
		if grep != nil && !grep.MatchString(msg) {
			continue
		}
		entry := LogEntry{
			Priority: eventLevelNames[e.System.Level],
			Source:   e.System.Provider.Name,
			PID:      e.System.Execution.ProcessID,
			EventID:  e.System.EventID,
			Message:  msg,
		}
		if t, err := time.Parse(time.RFC3339Nano, e.System.TimeCreated.SystemTime); err == nil {
			entry.Time = t.Local().Format(time.RFC3339)
		}
		out = append(out, entry)
		if len(out) >= in.Lines {
			break
		}
	}
	slices.Reverse(out) // newest last
	return out, nil
}

// decodeToolOutput converts Windows command output to UTF-8, trying UTF-16 (BOM or heuristic), UTF-8, then the console code page.
func decodeToolOutput(s string) string {
	b := []byte(s)
	if enc, n, ok := textenc.DetectUnicode(b); ok {
		return enc.Decode(b[n:])
	}
	if utf8.Valid(b) {
		return s
	}
	return textenc.Console().Decode(b)
}

// ---- disk_list ----

func listDisks(bool) ([]Disk, error) {
	n, err := windows.GetLogicalDriveStrings(0, nil)
	if err != nil {
		return nil, err
	}
	buf := make([]uint16, n)
	if _, err := windows.GetLogicalDriveStrings(n, &buf[0]); err != nil {
		return nil, err
	}
	var out []Disk
	for _, root := range strings.Split(strings.TrimRight(string(utf16Decode(buf)), "\x00"), "\x00") {
		if root == "" {
			continue
		}
		p, _ := windows.UTF16PtrFromString(root)
		dk := Disk{Mount: root, Type: driveType(windows.GetDriveType(p))}
		var fsName [64]uint16
		if windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))) == nil {
			dk.Type += " " + windows.UTF16ToString(fsName[:])
		}
		var avail, total, free uint64
		if windows.GetDiskFreeSpaceEx(p, &avail, &total, &free) == nil {
			dk.TotalBytes, dk.UsedBytes, dk.FreeBytes = total, total-free, avail
		}
		out = append(out, dk)
	}
	return out, nil
}

func utf16Decode(b []uint16) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func driveType(t uint32) string {
	switch t {
	case windows.DRIVE_REMOVABLE:
		return "removable"
	case windows.DRIVE_FIXED:
		return "fixed"
	case windows.DRIVE_REMOTE:
		return "network"
	case windows.DRIVE_CDROM:
		return "cdrom"
	case windows.DRIVE_RAMDISK:
		return "ramdisk"
	}
	return "unknown"
}
