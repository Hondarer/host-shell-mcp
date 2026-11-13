package sshpass

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/routeproxy"
)

// Environment variables that Env sets for ssh. markerEnv marks the process started by ssh as the
// askpass helper and tells it which prompts it may answer; nextEnv is the SSH_ASKPASS that was set
// before (for example VS Code's), which gets every prompt host-shell does not answer.
const (
	markerEnv = "HOST_SHELL_SSH_ASKPASS"
	nextEnv   = "HOST_SHELL_SSH_ASKPASS_NEXT"
)

// Values of markerEnv.
const (
	ModeModern = "1"      // OpenSSH 8.4 or later
	ModeLegacy = "legacy" // OpenSSH before 8.4, enabled by legacyEnv
)

// keyPattern is a user@host as ssh prints it: no white space, parentheses or quotes.
const keyPattern = `([^\s()']+@[^\s()']+)`

var (
	// passwordRE is the prompt of the "password" method, written by ssh itself.
	passwordRE = regexp.MustCompile(`^` + keyPattern + `'s password: ?$`)
	// kbdRE is a "keyboard-interactive" prompt whose text from the server is "Password:". OpenSSH
	// 8.4 and later prefix the server's text with "(user@host) ", so the key part cannot be forged
	// by the server; earlier versions show the server's text alone, so the form is not accepted.
	kbdRE = regexp.MustCompile(`^\(` + keyPattern + `\) (?i:password): ?$`)
)

// matchPrompt returns the user@host whose stored password answers prompt.
func matchPrompt(prompt string, legacy bool) (string, bool) {
	if m := passwordRE.FindStringSubmatch(prompt); m != nil {
		return m[1], true
	}
	if legacy {
		return "", false
	}
	if m := kbdRE.FindStringSubmatch(prompt); m != nil {
		return m[1], true
	}
	return "", false
}

// looksLikePrompt reports whether s is a prompt from ssh rather than an ordinary argument, so that
// a host-shell started with one argument by a child of ssh is not mistaken for the helper.
func looksLikePrompt(s string) bool {
	t := strings.TrimRight(s, " ")
	return strings.Contains(s, "\n") || strings.HasSuffix(t, ":") || strings.HasSuffix(t, "?")
}

// rejectedPath is the marker of a stored password that a server rejected.
func rejectedPath(key string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, statePrefix+"rejected-"+fileName(key)), nil
}

// Rejected reports whether the stored password for the normalized key was rejected by a server.
func Rejected(key string) bool {
	p, err := rejectedPath(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func markRejected(key string) {
	if p, err := rejectedPath(key); err == nil {
		if f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE, 0o600); err == nil {
			f.Close()
		}
	}
}

func clearRejected(key string) {
	if p, err := rejectedPath(key); err == nil {
		os.Remove(p)
	}
}

// retryWindow is how long a used password is remembered for the ssh process that asked for it.
// ssh asks again only after the server rejected the password, a few seconds later.
const retryWindow = 2 * time.Minute

// firstUse records that the ssh process ppid is given the password for key and reports whether it
// is the first time. A second request from the same ssh means the stored password was rejected:
// sending it again would only count towards the server's lockout. The process is identified by its
// ID and start time, as IDs are reused (quickly in containers and on Windows).
func firstUse(dir, key string, ppid int) bool {
	now := time.Now()
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if !strings.HasPrefix(e.Name(), statePrefix+"askpass-") {
				continue
			}
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > retryWindow {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	id := strconv.Itoa(ppid) + "-" + processStart(ppid)
	path := filepath.Join(dir, statePrefix+"askpass-"+id+"-"+fileName(key))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false
	}
	if err == nil {
		f.Close()
	}
	return true
}

// Serve runs host-shell as the SSH_ASKPASS helper when ssh started it with an environment from Env.
// If ran is true, the caller must exit with code. It answers a password prompt for a user@host that
// has a stored password, once per ssh process. A second request from the same ssh means that the
// server rejected the password: it is marked as rejected and not sent again by any ssh until it is
// replaced, so that repeated connections do not lock the account. Every other prompt (host key
// confirmation, key passphrases, other user@host, a rejected password) goes to the helper that was
// set before (nextEnv), or, without one, to the terminal when standard error is a terminal (as ssh
// itself would ask); otherwise it fails, so that ssh fails without waiting for input. When a
// rejected password is asked for and no one can be asked, the ssh process is terminated (refuse).
func Serve(args []string, stdout, stderr io.Writer) (ran bool, code int) {
	mode := os.Getenv(markerEnv)
	if (mode != ModeModern && mode != ModeLegacy) || len(args) != 2 || !looksLikePrompt(args[1]) {
		return false, 0
	}
	prompt := args[1]
	key, ok := matchPrompt(prompt, mode == ModeLegacy)
	if ok {
		key, err := NormalizeKey(key)
		var pw string
		var found bool
		if err == nil {
			pw, found, err = Get(key)
		}
		switch {
		case err != nil:
			fmt.Fprintln(stderr, "host-shell askpass:", err)
		case found && Rejected(key):
			fmt.Fprintf(stderr, "host-shell askpass: the stored password for %s was rejected before and is not sent again; replace it with host-shell ssh-pass set %s\n", key, key)
			return true, refuse(prompt, stdout, stderr)
		case found:
			dir, _ := Dir()
			if firstUse(dir, key, sshProcessID()) {
				fmt.Fprintln(stdout, pw)
				return true, 0
			}
			markRejected(key)
			fmt.Fprintf(stderr, "host-shell askpass: the stored password for %s was rejected and is not sent again; replace it with host-shell ssh-pass set %s\n", key, key)
			return true, refuse(prompt, stdout, stderr)
		}
	}
	return true, next(prompt, stdout, stderr)
}

// refuse handles a password prompt whose stored password was rejected. The user is asked instead,
// through the previous helper (VS Code's, for example) or on the terminal. Without either, ssh would
// send an empty password that the server counts as another failed attempt, so the ssh process is
// terminated.
func refuse(prompt string, stdout, stderr io.Writer) int {
	if code, asked := ask(prompt, stdout, stderr); asked {
		return code
	}
	stopSSH(sshProcessID())
	return 1
}

// next passes a prompt that host-shell does not answer to the user, or fails.
func next(prompt string, stdout, stderr io.Writer) int {
	code, _ := ask(prompt, stdout, stderr)
	return code
}

// promptLine asks on the terminal; a variable for tests.
var promptLine = routeproxyPromptLine

var routeproxyPromptLine = routeproxy.PromptLine

// ask passes the prompt to the helper that was set before host-shell, or, without one, asks on the
// terminal as ssh itself would (only when standard error is a terminal, so never for an ssh that an
// agent started with pipes). VS Code Remote - SSH in its terminal mode reads such a prompt from the
// terminal and shows its input box. asked is false when there is no one to ask.
func ask(prompt string, stdout, stderr io.Writer) (code int, asked bool) {
	helper := os.Getenv(nextEnv)
	if helper == "" || sameFile(helper, selfPath()) {
		echo := os.Getenv("SSH_ASKPASS_PROMPT") == "confirm" || strings.Contains(prompt, "(yes/no")
		line, ok, err := promptLine(prompt, echo)
		switch {
		case err != nil:
			fmt.Fprintln(stderr, "host-shell askpass:", err)
			return 1, true
		case !ok:
			return 1, false
		}
		fmt.Fprintln(stdout, line)
		return 0, true
	}
	cmd := exec.Command(helper, prompt)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), true
		}
		fmt.Fprintln(stderr, "host-shell askpass:", err)
		return 1, true
	}
	return 0, true
}

func selfPath() string {
	p, _ := os.Executable()
	return p
}

// sameFile reports whether a and b name the same file (including through links).
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}
