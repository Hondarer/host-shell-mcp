package sshpass

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hondarer/host-shell-mcp/internal/routeproxy"
)

const usage = `usage: host-shell ssh-pass set [--stdin] USER@HOST  store the password (asked on the terminal with echo off,
                                                  or read from the first line of standard input with --stdin)
       host-shell ssh-pass delete USER@HOST       remove the stored password
       host-shell ssh-pass list                   list the USER@HOST that have a stored password, marking
                                                  those that a server rejected (they are not sent again)
       host-shell ssh-pass dir                    show the directory that holds the passwords

USER@HOST is what ssh shows in its prompt "USER@HOST's password:": the user name and the host name
after ~/.ssh/config is applied (HostName, or HostKeyAlias when set). The host name is not case-sensitive.

The stored passwords are given to ssh through SSH_ASKPASS by host-shell bridge, and by the wrappers
"host-shell ssh [SSH ARGS...]" and "host-shell scp [SCP ARGS...]". Links or copies of host-shell named
ssh and scp in a directory of their own run the same wrappers (for VS Code's remote.SSH.path). OpenSSH 8.4 or later is required; for earlier
versions see HOST_SHELL_SSH_ASKPASS_LEGACY in the documentation.`

// Main runs host-shell ssh-pass with the arguments after "ssh-pass" and returns the exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "host-shell ssh-pass: %v\n", err)
		return 1
	}
	bad := func() int {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if len(args) == 0 {
		return bad()
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)
	case "dir":
		if len(args) != 1 {
			return bad()
		}
		d, err := Dir()
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, d)
	case "list":
		if len(args) != 1 {
			return bad()
		}
		keys, err := List()
		if err != nil {
			return fail(err)
		}
		for _, k := range keys {
			if Rejected(k) {
				fmt.Fprintf(stdout, "%s\trejected (replace it with ssh-pass set)\n", k)
			} else {
				fmt.Fprintln(stdout, k)
			}
		}
	case "delete":
		if len(args) != 2 {
			return bad()
		}
		ok, err := Delete(args[1])
		if err != nil {
			return fail(err)
		}
		if !ok {
			return fail(fmt.Errorf("no password is stored for %s", args[1]))
		}
	case "set":
		fromStdin := len(args) == 3 && args[1] == "--stdin"
		if len(args) != 2 && !fromStdin {
			return bad()
		}
		key := args[len(args)-1]
		norm, err := NormalizeKey(key)
		if err != nil {
			return fail(err)
		}
		var pw string
		if fromStdin {
			line, err := bufio.NewReader(stdin).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return fail(err)
			}
			pw = strings.TrimRight(line, "\r\n")
		} else {
			var ok bool
			pw, ok, err = routeproxy.PromptPassword(fmt.Sprintf("Password for %s: ", norm))
			switch {
			case err != nil:
				return fail(err)
			case !ok:
				return fail(errors.New("a terminal is required to enter the password (or use --stdin)"))
			}
		}
		if err := Set(norm, pw); err != nil {
			return fail(err)
		}
	default:
		return bad()
	}
	return 0
}
