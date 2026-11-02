package proc

// KillName returns the kill(1) name of a signal accepted by Signal (for example "TERM" for "terminate").
func KillName(name string) (string, error) {
	_, k, err := lookupSignal(name)
	return k, err
}
