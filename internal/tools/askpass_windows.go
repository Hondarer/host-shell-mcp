package tools

import "errors"

// Askpass does nothing on Windows, which has no sudo_run.
func Askpass() (ran bool, code int) { return false, 0 }

func withAskpass(string) (map[string]string, func(), error) {
	return nil, nil, errors.New("sudo is not available on Windows")
}
