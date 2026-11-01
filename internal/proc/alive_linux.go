package proc

import (
	"os"
	"strconv"
	"strings"
)

// Alive reports whether the process exists and is not a zombie.
func Alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// comm in "pid (comm) state ..." may contain ")", so the state follows the last one.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || !strings.HasPrefix(s[i+1:], " Z")
}
