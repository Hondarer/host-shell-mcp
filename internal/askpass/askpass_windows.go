// Package askpass lets exec_sudo pass a password to sudo -A through host-shell itself.
package askpass

import "errors"

// Serve does nothing on Windows, which has no exec_sudo.
func Serve() (ran bool, code int) { return false, 0 }

// Env fails on Windows, which has no sudo.
func Env(string) (map[string]string, func(), error) {
	return nil, nil, errors.New("sudo is not available on Windows")
}
