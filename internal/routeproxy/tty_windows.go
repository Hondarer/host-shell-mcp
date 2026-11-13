package routeproxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/sys/windows"
)

// promptPassword asks for the password on the console with echo off. It asks only when standard
// error is a console: when an agent starts ssh for MCP, standard error is a pipe and a prompt on
// the console would land in the agent's own window.
//
// Ctrl+C is caught while echo is off, so the console mode is restored and pending input is
// discarded before the process ends; otherwise the shell would receive leftover input.
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
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	newMode := old&^windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT
	if err := windows.SetConsoleMode(h, newMode); err != nil {
		return "", false, nil
	}
	defer func() {
		_ = windows.FlushConsoleInputBuffer(h)
		_ = windows.SetConsoleMode(h, old)
	}()
	fmt.Fprint(out, prompt)
	done := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(in).ReadString('\n')
		done <- readResult{line, err}
	}()
	select {
	case r := <-done:
		// The console itself moves to the next line on Enter even with echo off. Ctrl+C ends the
		// read with nothing read (EOF) and may arrive before the signal.
		if r.err != nil && r.line == "" {
			fmt.Fprint(out, "\r\n")
			if errors.Is(r.err, io.EOF) {
				return "", false, errInterrupted
			}
			return "", false, fmt.Errorf("cannot read the password: %v", r.err)
		}
		return strings.TrimRight(r.line, "\r\n"), true, nil
	case <-sigs:
		fmt.Fprint(out, "\r\n")
		return "", false, errInterrupted
	}
}
