//go:build windows

package remotedns

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// routeMetric marks the loopback default route host-shell adds; together with the loopback interface and
// the prefix 0.0.0.0/0 it identifies a route left behind by a session that ended without cleanup.
const routeMetric = 9999

// configureSetup removes rules and routes of sessions that no longer run, adds an NRPT rule that sends
// every name to the loopback DNS server, and adds a loopback IPv4 default route in the active store when
// none exists: Windows getaddrinfo drops IPv4 answers without an IPv4 default route. The route lives in the
// active store only, so a reboot removes it; the NRPT rule is persistent and removed on exit.
const configureSetup = `$ErrorActionPreference = 'Stop'
$tag = '%[1]s'; $me = %[2]d; $name = '%[3]s'; $server = '%[4]s'; $metric = %[5]d
$lo = (Get-NetIPAddress -AddressFamily IPv4 -IPAddress 127.0.0.1).InterfaceIndex
foreach ($r in @(Get-DnsClientNrptRule | Where-Object { $_.Comment -eq $tag -or $_.Comment -like ($tag + ' pid=*') })) {
	$owner = 0
	if ($r.Comment -match ' pid=(\d+)$') { $owner = [int]$Matches[1] }
	$p = $null
	if ($owner) { $p = Get-Process -Id $owner -ErrorAction SilentlyContinue }
	if ($p -and $p.ProcessName -eq $name) { throw ('another host-shell session (pid ' + $owner + ') manages remote DNS rules') }
	Remove-DnsClientNrptRule -Name $r.Name -Force
	'removed stale NRPT rule ' + $r.Name
}
foreach ($r in @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0 -InterfaceIndex $lo -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object RouteMetric -eq $metric)) {
	$r | Remove-NetRoute -Confirm:$false
	'removed stale loopback default route'
}
Add-DnsClientNrptRule -Namespace '.' -NameServers $server -Comment ($tag + ' pid=' + $me)
'added NRPT rule'
if (-not (Get-NetRoute -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0 -ErrorAction SilentlyContinue)) {
	New-NetRoute -DestinationPrefix 0.0.0.0/0 -InterfaceIndex $lo -NextHop 0.0.0.0 -RouteMetric $metric -PolicyStore ActiveStore | Out-Null
	'added loopback default route'
}
Clear-DnsClientCache
`

// configureTeardown removes what configureSetup added for this process.
const configureTeardown = `$ErrorActionPreference = 'Continue'
$tag = '%[1]s'; $me = %[2]d; $metric = %[3]d; $route = $%[4]t
Get-DnsClientNrptRule | Where-Object Comment -eq ($tag + ' pid=' + $me) | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
if ($route) {
	$lo = (Get-NetIPAddress -AddressFamily IPv4 -IPAddress 127.0.0.1).InterfaceIndex
	Get-NetRoute -AddressFamily IPv4 -DestinationPrefix 0.0.0.0/0 -InterfaceIndex $lo -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object RouteMetric -eq $metric | Remove-NetRoute -Confirm:$false
}
Clear-DnsClientCache
`

// configureResolver applies the resolver rule and route for server and returns the function that undoes
// them, with one line per change made.
func configureResolver(ctx context.Context, server netip.Addr) (func(), []string, error) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil, nil, errors.New("--remote-dns-configure changes the machine's resolver and needs an elevated administrator session")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	name := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	if strings.ContainsAny(name, "'\r\n") {
		return nil, nil, fmt.Errorf("unsupported executable name %q", name)
	}
	pid := os.Getpid()
	out, err := runPowerShell(ctx, fmt.Sprintf(configureSetup, ruleComment, pid, name, server, routeMetric))
	changes := splitLines(out)
	route := contains(changes, "added loopback default route")
	teardown := fmt.Sprintf(configureTeardown, ruleComment, pid, routeMetric, route)
	if err != nil {
		// Undo whatever the failed script already added.
		runPowerShell(context.Background(), teardown)
		return nil, nil, fmt.Errorf("--remote-dns-configure: %w", err)
	}
	for _, c := range changes {
		log.Printf("remote DNS: %s", c)
	}
	undo := func() {
		if _, err := runPowerShell(context.Background(), teardown); err != nil {
			log.Printf("remote DNS: removing the resolver settings failed: %v (remove NRPT rules commented %q manually)", err, fmt.Sprintf("%s pid=%d", ruleComment, pid))
			return
		}
		log.Printf("remote DNS: removed the resolver settings")
	}
	return undo, changes, nil
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
