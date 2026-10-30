package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hondarer/host-shell-mcp/internal/proc"
	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

// runTool runs an administrative command, prefixed with sudo -n when sudo is set.
func (d *Deps) runTool(ctx context.Context, argv []string, sudo bool) (proc.Result, error) {
	if sudo && os.Geteuid() != 0 {
		argv = append([]string{"sudo", "-n", "--"}, argv...)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return proc.Result{}, fmt.Errorf("%s is not available on this host: %w", argv[0], err)
	}
	spec := d.procSpec(CommandSpec{Argv: argv})
	return proc.Run(ctx, spec, proc.Options{
		Output:    textenc.Choice{Auto: true, Enc: textenc.Console()},
		Timeout:   d.Cfg.DefaultTimeout,
		MaxOutput: 32 << 20,
	})
}

// ---- proc_kill ----

func killProcess(ctx context.Context, d *Deps, pid int, sig string, tree, sudo bool) ([]int, error) {
	pids := []int{pid}
	if tree {
		pids = append(pids, descendants(pid)...)
	}
	if sudo {
		names := map[string]string{"interrupt": "INT", "terminate": "TERM", "hangup": "HUP", "quit": "QUIT", "kill": "KILL", "stop": "STOP", "continue": "CONT", "usr1": "USR1", "usr2": "USR2"}
		if names[sig] == "" {
			return nil, fmt.Errorf("unknown signal %q (use one of: %s)", sig, proc.SignalNames)
		}
		argv := []string{"kill", "-s", names[sig]}
		for _, p := range pids {
			argv = append(argv, strconv.Itoa(p))
		}
		res, err := d.runTool(ctx, argv, true)
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("kill: %s", strings.TrimSpace(res.Stderr))
		}
		return pids, nil
	}
	var done []int
	for _, p := range pids {
		if err := proc.Signal(p, sig, false); err != nil {
			if p == pid {
				return done, err
			}
			continue // Descendants may already have exited.
		}
		done = append(done, p)
	}
	return done, nil
}

// descendants returns all descendant PIDs of pid using /proc/<pid>/stat parent links.
func descendants(pid int) []int {
	children := map[int][]int{}
	stats, _ := filepath.Glob("/proc/[0-9]*/stat")
	for _, st := range stats {
		b, err := os.ReadFile(st)
		if err != nil {
			continue
		}
		// comm in "pid (comm) state ppid ..." can contain spaces and ")", so parse after the last ")".
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 2 {
			continue
		}
		p, _ := strconv.Atoi(filepath.Base(filepath.Dir(st)))
		pp, _ := strconv.Atoi(f[1])
		children[pp] = append(children[pp], p)
	}
	var out []int
	queue := []int{pid}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

func processAlive(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// Zombies (Z) count as exited.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && len(s) > i+2 {
		return s[i+2] != 'Z'
	}
	return syscall.Kill(pid, 0) == nil
}

// ---- svc_list / svc_control ----

func systemctl(user bool, args ...string) []string {
	argv := []string{"systemctl"}
	if user {
		argv = append(argv, "--user")
	}
	return append(append(argv, "--no-pager"), args...)
}

func listServices(ctx context.Context, d *Deps, user bool) ([]Service, error) {
	res, err := d.runTool(ctx, systemctl(user, "list-units", "--type=service", "--all", "--plain", "--no-legend"), false)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("systemctl: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	byName := map[string]*Service{}
	var out []*Service
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(strings.TrimLeft(line, "●* "))
		if len(f) < 4 {
			continue
		}
		s := &Service{Name: f[0], State: serviceState(f[2], f[3]), SubState: f[3], Description: strings.Join(f[4:], " ")}
		byName[s.Name] = s
		out = append(out, s)
	}
	// Include unloaded unit files and their enablement state from list-unit-files.
	if res, err := d.runTool(ctx, systemctl(user, "list-unit-files", "--type=service", "--plain", "--no-legend"), false); err == nil && res.ExitCode == 0 {
		for _, line := range strings.Split(res.Stdout, "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || strings.HasSuffix(f[0], "@.service") {
				continue
			}
			if s, ok := byName[f[0]]; ok {
				s.StartMode = f[1]
			} else {
				s := &Service{Name: f[0], State: "stopped", SubState: "dead", StartMode: f[1]}
				byName[s.Name] = s
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, func(a, b *Service) int { return strings.Compare(a.Name, b.Name) })
	list := make([]Service, len(out))
	for i, s := range out {
		list[i] = *s
	}
	return list, nil
}

// serviceState maps systemd states to OS-independent names.
func serviceState(active, sub string) string {
	switch active {
	case "active":
		if sub == "exited" {
			return "stopped"
		}
		return "running"
	case "inactive":
		return "stopped"
	case "failed":
		return "failed"
	case "activating", "reloading":
		return "starting"
	case "deactivating":
		return "stopping"
	}
	return active
}

func controlService(ctx context.Context, d *Deps, in SvcControlInput, action string) (SvcControlOutput, error) {
	var out SvcControlOutput
	if action != "status" {
		res, err := d.runTool(ctx, systemctl(in.User, action, in.Name), in.Sudo)
		if err != nil {
			return out, err
		}
		out.Output = strings.TrimSpace(res.Stdout + res.Stderr)
		if res.ExitCode != 0 {
			hint := ""
			if !in.Sudo && !in.User && os.Geteuid() != 0 && strings.Contains(out.Output, "auth") {
				hint = " (system units need root: set sudo)"
			}
			return out, fmt.Errorf("systemctl %s %s failed (exit %d): %s%s", action, in.Name, res.ExitCode, out.Output, hint)
		}
	}
	res, err := d.runTool(ctx, systemctl(in.User, "show", in.Name,
		"--property=Id,Description,LoadState,ActiveState,SubState,UnitFileState,MainPID,ExecMainStartTimestamp,FragmentPath,User,Result,NRestarts"), false)
	if err != nil {
		return out, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && v != "" {
			props[k] = v
		}
	}
	if props["LoadState"] == "not-found" {
		return out, fmt.Errorf("unit %s not found", in.Name)
	}
	pid, _ := strconv.Atoi(props["MainPID"])
	out.Service = Service{
		Name: orDefault(props["Id"], in.Name), Description: props["Description"],
		State: serviceState(props["ActiveState"], props["SubState"]), SubState: props["SubState"],
		StartMode: props["UnitFileState"], PID: pid,
	}
	out.Properties = props
	if st, err := d.runTool(ctx, systemctl(in.User, "status", "--lines=20", in.Name), in.Sudo); err == nil {
		out.StatusText = strings.TrimSpace(st.Stdout)
	}
	return out, nil
}

// ---- log_query ----

var journalPriorities = []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}

func queryLog(ctx context.Context, d *Deps, in LogQueryInput) ([]LogEntry, error) {
	if in.EventID != 0 {
		return nil, errors.New("event_id is for the Windows Event Log")
	}
	argv := []string{"journalctl", "--no-pager", "-o", "json", "-n", strconv.Itoa(in.Lines)}
	if in.User {
		argv = append(argv, "--user")
	}
	if in.Unit != "" {
		if in.User {
			argv = append(argv, "--user-unit", in.Unit)
		} else {
			argv = append(argv, "-u", in.Unit)
		}
	}
	switch in.Log {
	case "", "system":
	case "kernel":
		argv = append(argv, "-k")
	default:
		return nil, fmt.Errorf("log %q: on Linux use kernel or leave it empty", in.Log)
	}
	for _, t := range []struct{ flag, val string }{{"--since", in.Since}, {"--until", in.Until}} {
		if t.val == "" {
			continue
		}
		tm, err := parseSince(t.val)
		if err != nil {
			return nil, err
		}
		argv = append(argv, t.flag, tm.Format("2006-01-02 15:04:05"))
	}
	if in.Priority != "" {
		p := strings.ToLower(in.Priority)
		switch p {
		case "error":
			p = "err"
		case "critical":
			p = "crit"
		case "information":
			p = "info"
		}
		if !slices.Contains(journalPriorities, p) {
			return nil, fmt.Errorf("unknown priority %q (use one of: %s)", in.Priority, strings.Join(journalPriorities, ", "))
		}
		argv = append(argv, "-p", p)
	}
	var grep *regexp.Regexp
	if in.Grep != "" {
		var err error
		if grep, err = regexp.Compile("(?i)" + in.Grep); err != nil {
			return nil, fmt.Errorf("grep: %w", err)
		}
		// journalctl -g requires PCRE2 build support, so fetch extra entries and filter in Go.
		argv[5] = strconv.Itoa(min(in.Lines*20, 50000))
	}
	res, err := d.runTool(ctx, argv, in.Sudo)
	if err != nil {
		return nil, fmt.Errorf("%w (read log files with fs_read or search_grep instead, e.g. /var/log/syslog or /var/log/messages)", err)
	}
	if res.ExitCode != 0 && strings.TrimSpace(res.Stdout) == "" {
		return nil, fmt.Errorf("journalctl failed (exit %d): %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	var out []LogEntry
	sc := bufio.NewScanner(strings.NewReader(res.Stdout))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		e := LogEntry{Message: journalString(m["MESSAGE"])}
		if grep != nil && !grep.MatchString(e.Message) {
			continue
		}
		if us, err := strconv.ParseInt(journalString(m["__REALTIME_TIMESTAMP"]), 10, 64); err == nil {
			e.Time = time.UnixMicro(us).Format(time.RFC3339)
		}
		if p, err := strconv.Atoi(journalString(m["PRIORITY"])); err == nil && p >= 0 && p < len(journalPriorities) {
			e.Priority = journalPriorities[p]
		}
		e.Source = journalString(m["_SYSTEMD_UNIT"])
		if id := journalString(m["SYSLOG_IDENTIFIER"]); id != "" && (e.Source == "" || !strings.HasPrefix(e.Source, id)) {
			e.Source = strings.TrimSpace(id + " " + e.Source)
		}
		e.PID, _ = strconv.Atoi(journalString(m["_PID"]))
		out = append(out, e)
	}
	if len(out) > in.Lines {
		out = out[len(out)-in.Lines:]
	}
	if len(out) == 0 && res.ExitCode != 0 {
		return nil, fmt.Errorf("journalctl failed (exit %d): %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return out, nil
}

// journalString converts a journalctl -o json field (string, number, or byte array for non-printable data) to a string.
func journalString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		b := make([]byte, 0, len(x))
		for _, n := range x {
			if f, ok := n.(float64); ok {
				b = append(b, byte(f))
			}
		}
		if utf8.Valid(b) {
			return string(b)
		}
		return textenc.Console().Decode(b)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	}
	return ""
}

// ---- sys_disks ----

// pseudoFS lists synthetic file systems excluded by default.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true, "devpts": true, "mqueue": true, "debugfs": true,
	"tracefs": true, "securityfs": true, "pstore": true, "bpf": true, "configfs": true, "fusectl": true,
	"hugetlbfs": true, "autofs": true, "binfmt_misc": true, "rpc_pipefs": true, "nsfs": true, "devtmpfs": true,
	"efivarfs": true, "selinuxfs": true, "ramfs": true, "squashfs": true, "fuse.gvfsd-fuse": true, "fuse.portal": true,
}

func listDisks(all bool) ([]Disk, error) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Disk
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		dev, mnt, typ := unescapeMount(fields[0]), unescapeMount(fields[1]), fields[2]
		if !all && (pseudoFS[typ] || (typ == "tmpfs" && !strings.HasPrefix(mnt, "/tmp") && mnt != "/dev/shm")) {
			continue
		}
		if seen[mnt] {
			continue
		}
		seen[mnt] = true
		var st syscall.Statfs_t
		if err := syscall.Statfs(mnt, &st); err != nil {
			continue
		}
		bs := uint64(st.Bsize)
		total, free, avail := st.Blocks*bs, st.Bfree*bs, st.Bavail*bs
		if total == 0 && !all {
			continue
		}
		out = append(out, Disk{Mount: mnt, Device: dev, Type: typ, TotalBytes: total, UsedBytes: total - free, FreeBytes: avail})
	}
	return out, sc.Err()
}

// unescapeMount decodes octal escape sequences (such as \040) in /proc/self/mounts.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				sb.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}
