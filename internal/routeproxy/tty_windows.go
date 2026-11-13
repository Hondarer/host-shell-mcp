package routeproxy

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// promptPassword asks for the password on the console with echo off. It asks only when standard
// error is a console: when an agent starts ssh for MCP, standard error is a pipe and a prompt on
// the console would land in the agent's own window.
func promptPassword(prompt string) (string, bool, error) {
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(os.Stderr.Fd()), &mode) != nil {
		return "", false, nil
	}
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return "", false, nil
	}
	defer in.Close()
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return "", false, nil
	}
	defer out.Close()
	h := windows.Handle(in.Fd())
	var old uint32
	if err := windows.GetConsoleMode(h, &old); err != nil {
		return "", false, nil
	}
	newMode := old&^windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT
	if err := windows.SetConsoleMode(h, newMode); err != nil {
		return "", false, nil
	}
	defer windows.SetConsoleMode(h, old)
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	fmt.Fprint(out, "\r\n")
	if err != nil && line == "" {
		return "", false, fmt.Errorf("cannot read the password: %v", err)
	}
	return strings.TrimRight(line, "\r\n"), true, nil
}
