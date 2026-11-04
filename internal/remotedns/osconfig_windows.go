//go:build windows

package remotedns

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The DNS Client service reads local NRPT rules from subkeys of this key and watches it for changes.
const nrptKey = `SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`

// routeMetric marks the loopback default route host-shell adds; together with the loopback interface and
// the prefix 0.0.0.0/0 it identifies a route left behind by a session that ended without cleanup.
const routeMetric = 9999

var (
	advapi32           = windows.NewLazySystemDLL("advapi32.dll")
	procRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	ntdll              = windows.NewLazySystemDLL("ntdll.dll")
	procNtQueryKey     = ntdll.NewProc("NtQueryKey")
	dnsapi             = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushCache  = dnsapi.NewProc("DnsFlushResolverCache")
)

const (
	regOptionVolatile   = 0x1
	keyFlagsInformation = 5
	regFlagVolatile     = 0x1
)

// createVolatileKey creates (or opens) a subkey that lives in memory only: it is never written to disk
// and disappears at the latest when the machine restarts.
func createVolatileKey(parent registry.Key, name string) (registry.Key, bool, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, false, err
	}
	var h syscall.Handle
	var disposition uint32
	r, _, _ := procRegCreateKeyEx.Call(uintptr(parent), uintptr(unsafe.Pointer(p)), 0, 0, regOptionVolatile,
		uintptr(registry.ALL_ACCESS), 0, uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disposition)))
	if r != 0 {
		return 0, false, syscall.Errno(r)
	}
	return registry.Key(h), disposition == 1 /* REG_CREATED_NEW_KEY */, nil
}

// isVolatile reports whether k is a volatile (memory-only) key.
func isVolatile(k registry.Key) bool {
	var info struct{ Wow64Flags, KeyFlags, ControlFlags uint32 }
	var n uint32
	st, _, _ := procNtQueryKey.Call(uintptr(k), keyFlagsInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), uintptr(unsafe.Pointer(&n)))
	return st == 0 && info.KeyFlags&regFlagVolatile != 0
}

// processName returns the executable base name (without extension) of a running process, or "".
func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	base := filepath.Base(windows.UTF16ToString(buf[:n]))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// nrpt manages one volatile NRPT rule under nrptKey.
type nrpt struct {
	rule          string // subkey name of this session's rule
	createdParent bool   // nrptKey did not exist and was created as a volatile key
}

// cleanStale removes rules marked by ruleComment whose owning process no longer runs (persistent rules from
// the manual procedure included), and an empty volatile nrptKey left by such a session. It fails when a
// running host-shell owns a rule.
func cleanStale(self string) ([]string, error) {
	parent, err := registry.OpenKey(registry.LOCAL_MACHINE, nrptKey, registry.ALL_ACCESS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	names, err := parent.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var changes []string
	for _, n := range names {
		k, err := registry.OpenKey(parent, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		comment, _, _ := k.GetStringValue("Comment")
		k.Close()
		if comment != ruleComment && !strings.HasPrefix(comment, ruleComment+" pid=") {
			continue
		}
		if owner, err := strconv.ParseUint(strings.TrimPrefix(comment, ruleComment+" pid="), 10, 32); err == nil && processName(uint32(owner)) == self {
			return changes, fmt.Errorf("another host-shell session (pid %d) manages remote DNS rules", owner)
		}
		if err := registry.DeleteKey(parent, n); err != nil {
			return changes, fmt.Errorf("remove stale NRPT rule %s: %w", n, err)
		}
		changes = append(changes, "removed stale NRPT rule "+n)
	}
	if left, err := parent.ReadSubKeyNames(-1); err == nil && len(left) == 0 && isVolatile(parent) {
		if registry.DeleteKey(registry.LOCAL_MACHINE, nrptKey) == nil {
			changes = append(changes, "removed stale volatile DnsPolicyConfig key")
		}
	}
	return changes, nil
}

// add writes a volatile NRPT rule sending every name to server.
func (r *nrpt) add(server netip.Addr, pid int) error {
	parent, err := registry.OpenKey(registry.LOCAL_MACHINE, nrptKey, registry.ALL_ACCESS)
	if errors.Is(err, registry.ErrNotExist) {
		// A volatile parent keeps the machine's persistent registry unchanged; it goes away with the rule.
		parent, r.createdParent, err = createVolatileKey(registry.LOCAL_MACHINE, nrptKey)
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", nrptKey, err)
	}
	defer parent.Close()
	var id [16]byte
	rand.Read(id[:])
	id[6], id[8] = id[6]&0x0f|0x40, id[8]&0x3f|0x80
	r.rule = fmt.Sprintf("{%X-%X-%X-%X-%X}", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
	k, _, err := createVolatileKey(parent, r.rule)
	if err != nil {
		return fmt.Errorf("create NRPT rule: %w", err)
	}
	defer k.Close()
	// The same values Add-DnsClientNrptRule writes for a rule with generic DNS servers.
	for _, set := range []func() error{
		func() error { return k.SetDWordValue("Version", 2) },
		func() error { return k.SetDWordValue("ConfigOptions", 8) },
		func() error { return k.SetStringValue("GenericDNSServers", server.String()) },
		func() error { return k.SetStringValue("IPSECCARestriction", "") },
		func() error { return k.SetStringValue("DisplayName", "") },
		func() error { return k.SetStringValue("Comment", fmt.Sprintf("%s pid=%d", ruleComment, pid)) },
		func() error { return k.SetStringsValue("Name", []string{"."}) },
	} {
		if err := set(); err != nil {
			return fmt.Errorf("write NRPT rule: %w", err)
		}
	}
	return nil
}

// remove deletes this session's rule, and nrptKey when this session created it and it is empty.
func (r *nrpt) remove() error {
	if r.rule == "" {
		return nil
	}
	parent, err := registry.OpenKey(registry.LOCAL_MACHINE, nrptKey, registry.ALL_ACCESS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := registry.DeleteKey(parent, r.rule); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	if r.createdParent {
		if left, err := parent.ReadSubKeyNames(-1); err == nil && len(left) == 0 {
			registry.DeleteKey(registry.LOCAL_MACHINE, nrptKey)
		}
	}
	return nil
}

func flushResolverCache() { procDnsFlushCache.Call() }

// routeSetup removes a loopback default route left by an earlier session and adds one in the active
// store (memory only) when there is no IPv4 default route: Windows getaddrinfo drops IPv4 answers without one.
const routeSetup = `$ErrorActionPreference = 'Stop'
$metric = %[1]d; $clean = $%[2]t
$lo = (Get-NetIPAddress -AddressFamily IPv4 -IPAddress 127.0.0.1).InterfaceIndex
if ($clean) {
	foreach ($r in @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0 -InterfaceIndex $lo -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object RouteMetric -eq $metric)) {
		$r | Remove-NetRoute -Confirm:$false
		'removed stale loopback default route'
	}
}
if (-not (Get-NetRoute -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0 -ErrorAction SilentlyContinue)) {
	New-NetRoute -DestinationPrefix 0.0.0.0/0 -InterfaceIndex $lo -NextHop 0.0.0.0 -RouteMetric $metric -PolicyStore ActiveStore | Out-Null
	'added loopback default route (active store)'
}
`

const routeTeardown = `$lo = (Get-NetIPAddress -AddressFamily IPv4 -IPAddress 127.0.0.1).InterfaceIndex
Get-NetRoute -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0 -InterfaceIndex $lo -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object RouteMetric -eq %d | Remove-NetRoute -Confirm:$false
`

// configureResolver applies a volatile NRPT rule for server (and a loopback default route in the active
// store when needed) and returns the function that removes them, with one line per change made. Nothing is
// written to the persistent registry or the persistent route store.
func configureResolver(ctx context.Context, server netip.Addr) (undo func(), changes []string, err error) {
	needAdmin := errors.New("--remote-dns-configure changes the machine's resolver and needs an elevated administrator session")
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil, nil, needAdmin
	}
	// Some tokens count as elevated without administrator rights (for example Power Users over SSH).
	defer func() {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			err = fmt.Errorf("%w (%v)", needAdmin, err)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	self := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	changes, err = cleanStale(self)
	if err != nil {
		return nil, nil, fmt.Errorf("--remote-dns-configure: %w", err)
	}
	rule := &nrpt{}
	if err := rule.add(server, os.Getpid()); err != nil {
		rule.remove()
		return nil, nil, fmt.Errorf("--remote-dns-configure: %w", err)
	}
	changes = append(changes, "added volatile NRPT rule "+rule.rule)
	if rule.createdParent {
		changes = append(changes, "created volatile DnsPolicyConfig key")
	}
	out, err := runPowerShell(ctx, fmt.Sprintf(routeSetup, routeMetric, true))
	routeChanges := splitLines(out)
	changes = append(changes, routeChanges...)
	route := contains(routeChanges, "added loopback default route (active store)")
	remove := func() {
		var failed []string
		if err := rule.remove(); err != nil {
			failed = append(failed, fmt.Sprintf("NRPT rule %s: %v", rule.rule, err))
		}
		if route {
			if _, err := runPowerShell(context.Background(), fmt.Sprintf(routeTeardown, routeMetric)); err != nil {
				failed = append(failed, fmt.Sprintf("loopback default route: %v", err))
			}
		}
		flushResolverCache()
		if len(failed) > 0 {
			log.Printf("remote DNS: removing the resolver settings failed (%s); they are volatile and disappear at the next restart", strings.Join(failed, "; "))
			return
		}
		log.Printf("remote DNS: removed the resolver settings")
	}
	if err != nil {
		remove()
		return nil, nil, fmt.Errorf("--remote-dns-configure: %w", err)
	}
	flushResolverCache()
	for _, c := range changes {
		log.Printf("remote DNS: %s", c)
	}
	return remove, changes, nil
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// runPowerShell runs script in Windows PowerShell with no standard input and returns its standard output;
// a failure carries the standard error text.
func runPowerShell(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	u := utf16.Encode([]rune("[Console]::OutputEncoding = [Text.Encoding]::UTF8\n" + script))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[2*i], b[2*i+1] = byte(r), byte(r>>8)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-OutputFormat", "Text", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(firstLine(msg))
	}
	return stdout.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
