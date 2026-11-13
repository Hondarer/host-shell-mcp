//go:build !windows && !linux && !darwin

package sshpass

// processName is unknown on this platform, so stopSSH leaves ssh running.
func processName(int) string { return "" }

func processStart(int) string { return "" }
