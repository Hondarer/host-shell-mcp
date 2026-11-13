//go:build linux || darwin

package routeproxy

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// promptPassword asks for the password on the controlling terminal with echo off. It asks only
// when standard error is a terminal: when an agent starts ssh for MCP, standard error is a pipe
// and a prompt on /dev/tty would land in the agent's own terminal.
//
// Ctrl+C and termination signals are caught while echo is off, so the terminal settings are
// restored before the process ends; otherwise the user's shell would be left without echo.
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
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	noEcho := *old
	noEcho.Lflag &^= unix.ECHO
	noEcho.Lflag |= unix.ICANON | unix.ISIG
	noEcho.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &noEcho); err != nil {
		return "", false, nil
	}
	defer unix.IoctlSetTermios(fd, ioctlSetTermios, old)
	fmt.Fprint(tty, prompt)
	done := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(tty).ReadString('\n')
		done <- readResult{line, err}
	}()
	select {
	case r := <-done:
		// Echo is off, so the newline typed by the user is not shown.
		fmt.Fprint(tty, "\n")
		if r.err != nil && r.line == "" {
			return "", false, fmt.Errorf("cannot read the password: %v", r.err)
		}
		return strings.TrimRight(r.line, "\r\n"), true, nil
	case <-sigs:
		fmt.Fprint(tty, "\n")
		return "", false, errInterrupted
	}
}
