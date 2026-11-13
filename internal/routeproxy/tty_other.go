//go:build !linux && !darwin && !windows

package routeproxy

// promptPassword never prompts on platforms without terminal support here.
func promptPassword(string) (string, bool, error) { return "", false, nil }
