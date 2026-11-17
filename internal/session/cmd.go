package session

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/bridge"
)

const usage = `usage: host-shell session start [--name NAME] [--detach] [--idle-timeout DURATION] [bridge options] -- COMMAND [ARGS...]
       host-shell session status [--name NAME]
       host-shell session stop [--name NAME]
       host-shell session list

Keep a bridge to a target running on this machine for agents that cannot register MCP
servers. Agents then use the target through the shell:

  host-shell tools                               list the tools
  host-shell call TOOL [KEY=VALUE ...]           call a tool (see host-shell call --help)

COMMAND starts host-shell on the target, as for host-shell bridge, for example:

  host-shell session start --detach -- ssh -T target /tmp/host-shell-1000/host-shell --mcp

The session runs in the foreground until Ctrl+C. --detach starts it in the background and
returns when it is connected. Without --name, status and stop use $HOST_SHELL_SESSION or
"default"; start uses "default".

Tools that the target requires confirmation for (--confirm) ask in the terminal of a
foreground session; a session started with --detach or without a terminal refuses them.
`

// Main runs host-shell session and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "start":
		return startMain(args[1:], stdout, stderr)
	case "status", "stop":
		fs := flag.NewFlagSet("host-shell session "+args[0], flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "session name (default: $HOST_SHELL_SESSION or \"default\")")
		if err := fs.Parse(args[1:]); err != nil {
			return usageCode(err)
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "host-shell session %s: unexpected argument %q\n", args[0], fs.Arg(0))
			return 2
		}
		n := Name(*name)
		if args[0] == "stop" {
			if err := stop(n, 30*time.Second); err != nil {
				fmt.Fprintf(stderr, "host-shell: %v\n", err)
				return 1
			}
			return 0
		}
		st, err := Status(n)
		if err != nil {
			fmt.Fprintf(stderr, "host-shell: %v\n", err)
			return 1
		}
		_ = writeJSONLine(stdout, st)
		return 0
	case "list":
		if len(args) > 1 {
			fmt.Fprintf(stderr, "host-shell session list: unexpected argument %q\n", args[1])
			return 2
		}
		states, err := list()
		if err != nil {
			fmt.Fprintf(stderr, "host-shell: %v\n", err)
			return 1
		}
		for _, st := range states {
			_ = writeJSONLine(stdout, st)
		}
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "host-shell session: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func usageCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func startMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("host-shell session start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage+"\noptions:\n")
		fs.PrintDefaults()
	}
	name := fs.String("name", DefaultName, "session name: 1 to 32 letters, digits, '.', '_' or '-'")
	detach := fs.Bool("detach", false, "run the session in the background; print its state when it is connected")
	idle := fs.Duration("idle-timeout", 0, "end the session when no client has been connected for this long (default: never)")
	var opt bridge.Options
	opt.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return usageCode(err)
	}
	opt.Command = fs.Args()
	if len(opt.Command) == 0 {
		fs.Usage()
		fmt.Fprintln(stderr, "host-shell session start: COMMAND is required")
		return 2
	}
	if err := checkName(*name); err != nil {
		fmt.Fprintf(stderr, "host-shell: %v\n", err)
		return 2
	}
	if *idle < 0 {
		fmt.Fprintln(stderr, "host-shell: --idle-timeout must not be negative")
		return 2
	}
	child := os.Getenv(envChild) == "1"
	if *detach && !child {
		return startBackground(*name, args, stdout, stderr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	so := serveOptions{Name: *name, Bridge: opt, Remote: bridge.Command(opt.Command), Mode: "foreground", Idle: *idle, Log: stderr}
	if child {
		so.Mode = "detached"
	} else if isTerminal(os.Stdin) && isTerminal(os.Stderr) {
		so.Confirm = newPrompter(*name, os.Stdin, os.Stderr)
	}
	if err := serve(ctx, so); err != nil {
		fmt.Fprintf(stderr, "host-shell: %v\n", err)
		return 1
	}
	return 0
}

// startBackground starts this command again as a detached process and waits until the session
// is connected, or prints its log and fails when it ends first.
func startBackground(name string, args []string, stdout, stderr io.Writer) int {
	_, _, logPath, err := paths(name)
	if err != nil {
		fmt.Fprintf(stderr, "host-shell: %v\n", err)
		return 1
	}
	if st, err := Status(name); err == nil {
		fmt.Fprintf(stderr, "host-shell: session %q is already running (pid %d); stop it with: host-shell session stop --name %s\n", name, st.PID, name)
		return 1
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "host-shell: %v\n", err)
		return 1
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(stderr, "host-shell: %v\n", err)
		return 1
	}
	defer logFile.Close()
	cmd, err := startDetached(func() *exec.Cmd {
		cmd := exec.Command(self, append([]string{"session", "start"}, args...)...)
		cmd.Env = append(os.Environ(), envChild+"=1")
		cmd.Stdout, cmd.Stderr = logFile, logFile
		return cmd
	})
	if err != nil {
		fmt.Fprintf(stderr, "host-shell: start the session: %v\n", err)
		return 1
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			log, _ := os.ReadFile(logPath)
			fmt.Fprintf(stderr, "host-shell: session %q ended before it was ready. Its log (%s):\n%s", name, logPath, log)
			return 1
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			fmt.Fprintf(stderr, "host-shell: interrupted; session %q was not started\n", name)
			return 130
		case <-tick.C:
			st, err := Status(name)
			if err == nil && st.PID == cmd.Process.Pid && st.State == "ready" {
				_ = writeJSONLine(stdout, st)
				return 0
			}
		}
	}
}
