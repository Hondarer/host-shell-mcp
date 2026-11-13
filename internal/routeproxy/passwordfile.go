package routeproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

// maxPasswordFileBytes bounds how much of the password file is read; only its first line is used.
const maxPasswordFileBytes = 64 << 10

// readPasswordFile returns the first line of the password file after checking that its owner and
// permissions meet what OpenSSH requires of the user's ~/.ssh/config (see checkedOpen).
// A UTF-8 or UTF-16LE byte order mark is accepted, as editors and Windows PowerShell 5.1
// redirection write them.
func readPasswordFile(path string) (string, error) {
	f, err := checkedOpen(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPasswordFileBytes))
	if err != nil {
		return "", fmt.Errorf("cannot read password file %s: %v", path, err)
	}
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
		}
		b = []byte(string(utf16.Decode(u)))
	}
	if len(b) == 0 {
		return "", fmt.Errorf("password file %s is empty", path)
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i]
	}
	return string(bytes.TrimRight(b, "\r")), nil
}

// errInterrupted reports that the user interrupted the password prompt (for example with Ctrl+C).
var errInterrupted = errors.New("password input was interrupted")

// readResult is a line read from the terminal by promptPassword.
type readResult struct {
	line string
	err  error
}

// errBadPermissions prefixes the message for a password file that fails the permission check.
var errBadPermissions = errors.New("bad owner or permissions on password file")

// ReadPasswordFile is readPasswordFile for other packages that keep passwords in owner-only files.
func ReadPasswordFile(path string) (string, error) { return readPasswordFile(path) }

// PromptPassword asks for a password on the terminal with echo off; ok is false without a terminal.
func PromptPassword(prompt string) (string, bool, error) { return promptPassword(prompt) }

// PromptLine asks on the terminal like PromptPassword, echoing the input when echo is set.
func PromptLine(prompt string, echo bool) (string, bool, error) { return promptLine(prompt, echo) }
