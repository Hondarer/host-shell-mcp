// Package sshpass stores SSH login passwords per user@host and supplies them to OpenSSH through its
// standard SSH_ASKPASS mechanism, so that password-authenticated SSH servers work without a terminal
// for host-shell bridge, the host-shell ssh wrapper (for scripts and VS Code Remote - SSH), and any
// ssh started from them, including the ssh that OpenSSH starts for ProxyJump.
package sshpass

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hondarer/host-shell-mcp/internal/routeproxy"
)

// dirEnv overrides the directory that holds the passwords.
const dirEnv = "HOST_SHELL_SSH_PASSWORD_DIR"

// statePrefix starts the names of files in the password directory that are not passwords.
const statePrefix = "."

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

// NormalizeKey checks that key has the form user@host and lowercases the host, as host names are
// case-insensitive. The user name keeps its case. The last "@" separates the two, so a user name
// may itself contain "@" (for example alice@corp.example@host).
func NormalizeKey(key string) (string, error) {
	i := strings.LastIndex(key, "@")
	if i <= 0 || i == len(key)-1 || strings.ContainsAny(key, " \t\r\n()'") {
		return "", fmt.Errorf("%q is not in the form user@host", key)
	}
	return key[:i] + "@" + strings.ToLower(key[i+1:]), nil
}

// fileName encodes a normalized key into a file name that is valid on Linux, macOS and Windows.
// Upper-case letters are escaped too, so that user names differing only in case stay distinct on
// case-insensitive file systems.
func fileName(key string) string {
	var b strings.Builder
	for _, c := range []byte(key) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '-', c == '@', c == '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Set stores the password for key, replacing any earlier one. The file is readable and writable
// only by the user (mode 0600; on Windows a protected DACL for the user alone, and the password is
// encrypted with DPAPI for the user), in a directory created with mode 0700.
func Set(key, password string) error {
	key, err := NormalizeKey(key)
	if err != nil {
		return err
	}
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return errors.New("the password must be a single non-empty line")
	}
	data, err := protect(password)
	if err != nil {
		return err
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, statePrefix+"tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(data + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := secureFile(tmp.Name()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, fileName(key))); err != nil {
		return err
	}
	clearRejected(key)
	return nil
}

// Get returns the stored password for key; found is false when none is stored. The file must pass
// the same owner and permission check as a connect --password-file.
func Get(key string) (password string, found bool, err error) {
	key, err = NormalizeKey(key)
	if err != nil {
		return "", false, err
	}
	dir, err := Dir()
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(dir, fileName(key))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	data, err := routeproxy.ReadPasswordFile(path)
	if err != nil {
		return "", false, err
	}
	password, err = unprotect(data)
	if err != nil {
		return "", false, fmt.Errorf("cannot decrypt the password for %s: %v", key, err)
	}
	return password, true, nil
}

// Delete removes the stored password for key and reports whether one existed.
func Delete(key string) (bool, error) {
	key, err := NormalizeKey(key)
	if err != nil {
		return false, err
	}
	dir, err := Dir()
	if err != nil {
		return false, err
	}
	err = os.Remove(filepath.Join(dir, fileName(key)))
	clearRejected(key)
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
		if e.IsDir() || strings.HasPrefix(e.Name(), statePrefix) {
			continue
		}
		if key, err := url.PathUnescape(e.Name()); err == nil {
			if k, err := NormalizeKey(key); err == nil && k == key {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys, nil
}
