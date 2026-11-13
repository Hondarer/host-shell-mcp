//go:build linux || darwin

package routeproxy

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// promptPassword asks for the password on the controlling terminal with echo off. It asks only
// when standard error is a terminal: when an agent starts ssh for MCP, standard error is a pipe
// and a prompt on /dev/tty would land in the agent's own terminal.
func promptPassword(prompt string) (string, bool, error) {
	if _, err := unix.IoctlGetTermios(int(os.Stderr.Fd()), ioctlGetTermios); err != nil {
		return "", false, nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", false, nil
	}
	defer tty.Close()
	fd := int(tty.Fd())
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return "", false, nil
	}
	noEcho := *old
	noEcho.Lflag &^= unix.ECHO
	noEcho.Lflag |= unix.ICANON | unix.ISIG
	noEcho.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &noEcho); err != nil {
		return "", false, nil
	}
	defer unix.IoctlSetTermios(fd, ioctlSetTermios, old)
	fmt.Fprint(tty, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	fmt.Fprint(tty, "\n")
	if err != nil && line == "" {
		return "", false, fmt.Errorf("cannot read the password: %v", err)
	}
	return strings.TrimRight(line, "\r\n"), true, nil
}
