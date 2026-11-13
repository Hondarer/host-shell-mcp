//go:build !windows

package sshpass

// protect stores the password as plain text; the file is readable only by the user.
func protect(password string) (string, error) { return password, nil }

// unprotect returns the stored password as is.
func unprotect(data string) (string, error) { return data, nil }

// secureFile does nothing: os.CreateTemp creates the file with mode 0600.
func secureFile(string) error { return nil }
