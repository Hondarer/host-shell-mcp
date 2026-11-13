// Package sshpass stores SSH login passwords per user@host and supplies them to OpenSSH through
// SSH_ASKPASS, so that password-authenticated jump servers work without a terminal.
package sshpass

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hondarer/host-shell-mcp/internal/routeproxy"
)

// Environment variables. HOST_SHELL_SSH_ASKPASS marks the process started by ssh as the askpass helper.
const (
	markerEnv = "HOST_SHELL_SSH_ASKPASS"
	dirEnv    = "HOST_SHELL_SSH_PASSWORD_DIR"
)

var promptRE = regexp.MustCompile(`^(.+@.+)'s password: ?$`)

// Dir returns the directory that holds one password file per user@host.
func Dir() (string, error) {
	if d := os.Getenv(dirEnv); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "host-shell", "ssh-passwords"), nil
}

// fileName encodes a key into a file name that is valid on Linux and Windows.
func fileName(key string) string {
	var b strings.Builder
	for _, c := range []byte(key) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '@', c == '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ValidKey reports whether key has the form user@host.
func ValidKey(key string) bool {
	i := strings.LastIndex(key, "@")
	return i > 0 && i < len(key)-1 && !strings.ContainsAny(key, "\r\n")
}

// Set stores the password for key, replacing any earlier one. The file is created in a directory
// only the user can enter, so it is not readable by others (on Windows it inherits the profile's ACL).
func Set(key, password string) error {
	if !ValidKey(key) {
		return fmt.Errorf("%q is not in the form user@host", key)
	}
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return errors.New("the password must be a single non-empty line")
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(password + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, fileName(key)))
}

// Get returns the stored password for key; found is false when none is stored. The file must pass
// the same owner and permission check as a connect --password-file.
func Get(key string) (password string, found bool, err error) {
	dir, err := Dir()
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(dir, fileName(key))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	password, err = routeproxy.ReadPasswordFile(path)
	return password, err == nil, err
}

// Delete removes the stored password for key and reports whether one existed.
func Delete(key string) (bool, error) {
	dir, err := Dir()
	if err != nil {
		return false, err
	}
	err = os.Remove(filepath.Join(dir, fileName(key)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// List returns the keys that have a stored password, sorted.
func List() ([]string, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if key, err := url.PathUnescape(e.Name()); err == nil && ValidKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Env returns the environment that makes an ssh started with it ask this executable for passwords.
// A non-empty SSH_ASKPASS the user already set is left alone.
func Env(environ []string, self string) []string {
	has := func(name string) bool {
		for _, e := range environ {
			if strings.HasPrefix(e, name+"=") && len(e) > len(name)+1 {
				return true
			}
		}
		return false
	}
	if has("SSH_ASKPASS") {
		return environ
	}
	out := append([]string{}, environ...)
	out = append(out, "SSH_ASKPASS="+self, markerEnv+"=1", "SSH_ASKPASS_REQUIRE=force")
	// OpenSSH before 8.4 ignores SSH_ASKPASS_REQUIRE and uses the helper only when DISPLAY is set.
	if !has("DISPLAY") {
		out = append(out, "DISPLAY=host-shell")
	}
	return out
}

// Serve runs host-shell as the SSH_ASKPASS helper when ssh started it with Env. If ran is true,
// the caller must exit with code. It answers only "user@host's password:" prompts with a stored
// password; for any other prompt (host key confirmation, key passphrase) it exits 1 without output.
func Serve(args []string, stdout, stderr io.Writer) (ran bool, code int) {
	if os.Getenv(markerEnv) != "1" || len(args) != 2 || strings.HasPrefix(args[1], "-") {
		return false, 0
	}
	m := promptRE.FindStringSubmatch(args[1])
	if m == nil {
		return true, 1
	}
	pw, found, err := Get(m[1])
	switch {
	case err != nil:
		fmt.Fprintln(stderr, "host-shell askpass:", err)
		return true, 1
	case !found:
		return true, 1
	}
	fmt.Fprintln(stdout, pw)
	return true, 0
}

const usage = `usage: host-shell ssh-pass set USER@HOST     store the password (asked on the terminal, echo off)
       host-shell ssh-pass delete USER@HOST  remove the stored password
       host-shell ssh-pass list              list the USER@HOST that have a stored password
       host-shell ssh-pass dir               show the directory that holds the passwords

USER@HOST is what ssh shows in its prompt "USER@HOST's password:": the user name and the host name
after ~/.ssh/config is applied (HostName). host-shell bridge passes the stored passwords to ssh
through SSH_ASKPASS.`

// Main runs host-shell ssh-pass with the arguments after "ssh-pass" and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "host-shell ssh-pass: %v\n", err)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)
	case "dir":
		d, err := Dir()
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, d)
	case "list":
		keys, err := List()
		if err != nil {
			return fail(err)
		}
		for _, k := range keys {
			fmt.Fprintln(stdout, k)
		}
	case "delete":
		if len(args) != 2 {
			fmt.Fprintln(stderr, usage)
			return 1
		}
		ok, err := Delete(args[1])
		if err != nil {
			return fail(err)
		}
		if !ok {
			return fail(fmt.Errorf("no password is stored for %s", args[1]))
		}
	case "set":
		if len(args) != 2 {
			fmt.Fprintln(stderr, usage)
			return 1
		}
		if !ValidKey(args[1]) {
			return fail(fmt.Errorf("%q is not in the form user@host", args[1]))
		}
		pw, ok, err := routeproxy.PromptPassword(fmt.Sprintf("Password for %s: ", args[1]))
		switch {
		case err != nil:
			return fail(err)
		case !ok:
			return fail(errors.New("a terminal is required to enter the password"))
		}
		if err := Set(args[1], pw); err != nil {
			return fail(err)
		}
	default:
		fmt.Fprintln(stderr, usage)
		return 1
	}
	return 0
}
