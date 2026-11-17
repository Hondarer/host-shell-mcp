// Package session keeps a bridge to a target running on this machine for agents that cannot
// register MCP servers. host-shell session start runs the bridge (package bridge) and serves
// MCP on a Unix domain socket; host-shell call and host-shell tools (package cli) connect to it
// once per command.
//
// The socket lives in a directory that only the current user can use. The first line a client
// sends selects the protocol: a control request ({"host_shell_session":"status"} or "stop")
// gets one JSON line back; anything else is the start of an MCP session.
package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/ownerdir"
)

const (
	// EnvName selects the session when a command has no --name or --session.
	EnvName = "HOST_SHELL_SESSION"
	// EnvDir overrides the directory that holds the sockets.
	EnvDir = "HOST_SHELL_SESSION_DIR"
	// DefaultName is the session name when neither an option nor EnvName gives one.
	DefaultName = "default"

	// envChild marks the process that session start --detach starts in the background.
	envChild = "HOST_SHELL_SESSION_CHILD"
	// controlKey is the field of a control request.
	controlKey = "host_shell_session"
	// maxSocketPath is the shortest limit on the path of a Unix domain socket (macOS: 104 bytes
	// including the terminating NUL; Linux and Windows allow 108).
	maxSocketPath = 103
)

var validName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// State describes a running session (session status, session list, session start --detach).
type State struct {
	Name          string    `json:"name"`
	State         string    `json:"state"` // connecting or ready
	PID           int       `json:"pid"`
	Started       time.Time `json:"started"`
	Mode          string    `json:"mode"`    // foreground or detached
	Confirm       string    `json:"confirm"` // terminal: asks in the terminal; refuse: refuses tools needing confirmation
	Version       string    `json:"version"`
	RemoteVersion string    `json:"remote_version,omitempty"`
	Command       []string  `json:"command"`
	Socket        string    `json:"socket"`
	Clients       int       `json:"clients"`
	IdleTimeout   string    `json:"idle_timeout,omitempty"`
}

// Name returns the session name given by an option, EnvName, or DefaultName, in that order.
func Name(option string) string {
	if option != "" {
		return option
	}
	if v := os.Getenv(EnvName); v != "" {
		return v
	}
	return DefaultName
}

func checkName(name string) error {
	if !validName.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("invalid session name %q: use 1 to 32 letters, digits, '.', '_' or '-'", name)
	}
	return nil
}

// Dir returns the directory of the session sockets, creating it for the current user only.
func Dir() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return d, ownerdir.Ensure(d)
	}
	return defaultDir()
}

// paths returns the socket, state and log paths of a session.
func paths(name string) (sock, state, logFile string, err error) {
	if err := checkName(name); err != nil {
		return "", "", "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", "", "", fmt.Errorf("session directory: %w", err)
	}
	sock = filepath.Join(dir, name+".sock")
	if len(sock) > maxSocketPath {
		return "", "", "", fmt.Errorf("socket path %s is too long for a Unix domain socket; set %s to a shorter directory", sock, EnvDir)
	}
	return sock, filepath.Join(dir, name+".json"), filepath.Join(dir, name+".log"), nil
}

// Dial connects to the socket of the named session.
func Dial(name string) (net.Conn, error) {
	sock, _, _, err := paths(name)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		return nil, &NotRunningError{Name: name}
	}
	return conn, nil
}

// NotRunningError reports that no session with the name accepts connections.
type NotRunningError struct{ Name string }

func (e *NotRunningError) Error() string {
	return fmt.Sprintf("no session %q is running; start one with: host-shell session start --name %s --detach -- COMMAND...", e.Name, e.Name)
}

// control sends a control request to the named session and decodes its one-line answer.
func control(name, op string, out any) error {
	conn, err := Dial(name)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	req, _ := json.Marshal(map[string]string{controlKey: op})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("session %q: %w", name, err)
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(line, &e) == nil && e.Error != "" {
		return errors.New(e.Error)
	}
	return json.Unmarshal(line, out)
}

// Status returns the state of the named session.
func Status(name string) (State, error) {
	var st State
	err := control(name, "status", &st)
	return st, err
}

// list returns the states of the running sessions.
func list() ([]State, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []State
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".sock")
		if !ok || checkName(name) != nil {
			continue
		}
		if st, err := Status(name); err == nil {
			out = append(out, st)
		}
	}
	slices.SortFunc(out, func(a, b State) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// stop asks the named session to end and waits until its socket stops accepting connections.
func stop(name string, timeout time.Duration) error {
	var ack map[string]any
	if err := control(name, "stop", &ack); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := Dial(name)
		if err != nil {
			return nil
		}
		conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("session %q did not stop within %s", name, timeout)
}

// writeJSONLine writes v as one line of JSON without HTML escaping.
func writeJSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
