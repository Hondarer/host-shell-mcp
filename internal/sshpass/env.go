package sshpass

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// legacyEnv=1 enables the helper for OpenSSH before 8.4 (see Mode).
const legacyEnv = "HOST_SHELL_SSH_ASKPASS_LEGACY"

var versionRE = regexp.MustCompile(`OpenSSH_(?:for_Windows_)?(\d+)\.(\d+)`)

// parseVersion extracts the OpenSSH version from the output of ssh -V.
func parseVersion(out string) (major, minor int, ok bool) {
	m := versionRE.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

// IsSSH reports whether path names an ssh client executable (ssh or ssh.exe).
func IsSSH(path string) bool {
	base := filepath.Base(path)
	if runtime.GOOS == "windows" {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	}
	return base == "ssh"
}

// modeFor returns the helper mode for the version string printed by ssh -V: ModeModern for
// OpenSSH 8.4 or later, ModeLegacy for earlier versions when legacy is set, and "" otherwise.
//
// Before 8.4, ssh ignores SSH_ASKPASS_REQUIRE and shows keyboard-interactive prompts from the
// server without the "(user@host)" prefix, so a server could ask for another host's password with
// a prompt that looks like ssh's own. Legacy mode answers only the "password" method's prompt and
// is safe only when keyboard-interactive authentication is disabled in the client's ssh config.
func modeFor(versionOutput string, legacy bool) string {
	major, minor, ok := parseVersion(versionOutput)
	switch {
	case ok && (major > 8 || major == 8 && minor >= 4):
		return ModeModern
	case legacy:
		return ModeLegacy
	}
	return ""
}

// Mode runs ssh -V and returns the helper mode for that ssh ("" disables the helper).
func Mode(ssh string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, ssh, "-V").CombinedOutput()
	return modeFor(string(out), os.Getenv(legacyEnv) == "1")
}

// Env returns environ with the settings that make ssh ask self (host-shell) for passwords. An
// SSH_ASKPASS set before is kept as the next helper for prompts that host-shell does not answer.
// With mode "" or when environ already points SSH_ASKPASS at host-shell, environ is returned as is.
func Env(environ []string, self, mode string) []string {
	if mode == "" {
		return environ
	}
	get := func(name string) string {
		v := ""
		for _, e := range environ {
			if k, val, ok := strings.Cut(e, "="); ok && envNameEqual(k, name) {
				v = val
			}
		}
		return v
	}
	prev := get("SSH_ASKPASS")
	if get(markerEnv) != "" && prev != "" && (prev == self || sameFile(prev, self)) {
		return environ
	}
	next := get(nextEnv)
	if prev != "" {
		next = prev
	}
	drop := []string{"SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", markerEnv, nextEnv}
	out := make([]string, 0, len(environ)+5)
	for _, e := range environ {
		k, _, _ := strings.Cut(e, "=")
		keep := true
		for _, d := range drop {
			if envNameEqual(k, d) {
				keep = false
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	out = append(out, "SSH_ASKPASS="+self, "SSH_ASKPASS_REQUIRE=force", markerEnv+"="+mode)
	if next != "" {
		out = append(out, nextEnv+"="+next)
	}
	// OpenSSH before 8.4 ignores SSH_ASKPASS_REQUIRE and uses the helper only when DISPLAY is set.
	if mode == ModeLegacy && get("DISPLAY") == "" {
		out = append(out, "DISPLAY=host-shell")
	}
	return out
}

func envNameEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
