package sshpass

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// helperEnv makes the test binary act as a previously set SSH_ASKPASS (see TestMain).
const helperEnv = "SSHPASS_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		fmt.Println("from next:", os.Args[len(os.Args)-1])
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func TestNormalizeKey(t *testing.T) {
	for in, want := range map[string]string{
		"alice@Hop1.Example.COM": "alice@hop1.example.com",
		"Alice@hop1":             "Alice@hop1",
		"alice@corp.example@h":   "alice@corp.example@h",
		"bob@[::1]":              "bob@[::1]",
	} {
		if got, err := NormalizeKey(in); err != nil || got != want {
			t.Errorf("NormalizeKey(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"alice", "@hop1", "alice@", "a b@h", "(a@h)", "a@h's", "a@h\n"} {
		if _, err := NormalizeKey(in); err == nil {
			t.Errorf("NormalizeKey(%q) accepted", in)
		}
	}
}

func TestStore(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	for _, k := range []string{"alice@hop1", "Alice@hop1", "bob@[::1]", "alice@corp@h"} {
		if err := Set(k, "pw "+k); err != nil {
			t.Fatal(err)
		}
	}
	if err := Set("alice@HOP1", "new"); err != nil {
		t.Fatal(err)
	}
	if keys, _ := List(); !slices.Equal(keys, []string{"Alice@hop1", "alice@corp@h", "alice@hop1", "bob@[::1]"}) {
		t.Errorf("List = %v", keys)
	}
	if pw, found, err := Get("alice@Hop1"); err != nil || !found || pw != "new" {
		t.Errorf("Get = %q %v %v", pw, found, err)
	}
	if pw, _, _ := Get("Alice@hop1"); pw != "pw Alice@hop1" {
		t.Errorf("user names must keep their case: %q", pw)
	}
	for _, bad := range []string{"", "a\nb"} {
		if err := Set("alice@hop1", bad); err == nil {
			t.Errorf("Set accepted %q", bad)
		}
	}
	if ok, _ := Delete("alice@hop1"); !ok {
		t.Error("Delete failed")
	}
	if ok, _ := Delete("alice@hop1"); ok {
		t.Error("Delete of a missing key succeeded")
	}
	if _, found, _ := Get("alice@hop1"); found {
		t.Error("still stored")
	}
}

func TestMatchPrompt(t *testing.T) {
	cases := []struct {
		prompt string
		modern string // key in modern mode ("" for no match)
		legacy string // key in legacy mode
	}{
		{"alice@hop1.example.com's password: ", "alice@hop1.example.com", "alice@hop1.example.com"},
		{"alice@corp@hop1's password:", "alice@corp@hop1", "alice@corp@hop1"},
		{"(alice@hop1) Password: ", "alice@hop1", ""},
		{"(alice@hop1) password:", "alice@hop1", ""},
		// A keyboard-interactive prompt from a server after the "(user@host) " prefix of ssh.
		{"(bob@evil) alice@hop1's password: ", "", ""},
		{"(bob@evil) (alice@hop1) Password: ", "", ""},
		{"(bob@evil) x\nalice@hop1's password: ", "", ""},
		{"(alice@hop1) Verification code: ", "", ""},
		{"Enter passphrase for key '/home/alice/.ssh/id_ed25519': ", "", ""},
		{"Are you sure you want to continue connecting (yes/no/[fingerprint])? ", "", ""},
		{"Password: ", "", ""},
	}
	for _, c := range cases {
		got, ok := matchPrompt(c.prompt, false)
		if (c.modern != "") != ok || got != c.modern {
			t.Errorf("modern %q: %q %v", c.prompt, got, ok)
		}
		got, ok = matchPrompt(c.prompt, true)
		if (c.legacy != "") != ok || got != c.legacy {
			t.Errorf("legacy %q: %q %v", c.prompt, got, ok)
		}
	}
}

func TestServe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dirEnv, dir)
	t.Setenv(markerEnv, ModeModern)
	t.Setenv(nextEnv, "")
	if err := Set("alice@hop1", "secret"); err != nil {
		t.Fatal(err)
	}
	serve := func(prompt string) (bool, int, string, string) {
		var out, errb bytes.Buffer
		ran, code := Serve([]string{"host-shell", prompt}, &out, &errb)
		return ran, code, out.String(), errb.String()
	}
	if ran, code, out, _ := serve("(alice@HOP1) Password: "); !ran || code != 0 || out != "secret\n" {
		t.Errorf("first request = %v %d %q", ran, code, out)
	}
	// The same ssh process asks again only when the server rejected the password.
	if ran, code, out, errs := serve("alice@hop1's password: "); !ran || code != 1 || out != "" || !strings.Contains(errs, "was rejected") {
		t.Errorf("second request = %v %d %q %q", ran, code, out, errs)
	}
	if keys, _ := List(); !slices.Equal(keys, []string{"alice@hop1"}) {
		t.Errorf("state files are listed: %v", keys)
	}
	// Another ssh process (a reconnection) does not get the rejected password either.
	if !Rejected("alice@hop1") {
		t.Error("not marked as rejected")
	}
	os.Remove(filepath.Join(dir, statePrefix+"askpass-"+fmt.Sprint(sshProcessID())+"-"+processStart(sshProcessID())+"-alice@hop1"))
	if ran, code, out, errs := serve("alice@hop1's password: "); !ran || code != 1 || out != "" || !strings.Contains(errs, "rejected before") {
		t.Errorf("after rejection = %v %d %q %q", ran, code, out, errs)
	}
	// Storing a new password clears the mark.
	if err := Set("alice@hop1", "secret2"); err != nil || Rejected("alice@hop1") {
		t.Fatalf("Set did not clear the mark: %v", err)
	}
	os.Remove(filepath.Join(dir, statePrefix+"askpass-"+fmt.Sprint(sshProcessID())+"-"+processStart(sshProcessID())+"-alice@hop1"))
	if _, code, out, _ := serve("alice@hop1's password: "); code != 0 || out != "secret2\n" {
		t.Errorf("after Set = %d %q", code, out)
	}
	for _, prompt := range []string{"bob@hop1's password: ", "Enter passphrase for key 'k': ", "Are you sure (yes/no)? "} {
		if ran, code, out, _ := serve(prompt); !ran || code != 1 || out != "" {
			t.Errorf("prompt %q: %v %d %q", prompt, ran, code, out)
		}
	}
	// Not a prompt: a child of ssh that runs host-shell with one argument.
	if ran, _, _, _ := serve("ssh-pass"); ran {
		t.Error("served a non-prompt argument")
	}
	t.Setenv(markerEnv, "")
	if ran, _, _, _ := serve("alice@hop1's password: "); ran {
		t.Error("served without the marker")
	}
}

func TestServeLegacyRejectsKeyboardInteractive(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	t.Setenv(markerEnv, ModeLegacy)
	t.Setenv(nextEnv, "")
	if err := Set("alice@hop1", "secret"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if ran, code := Serve([]string{"host-shell", "(alice@hop1) Password: "}, &out, os.Stderr); !ran || code != 1 || out.Len() != 0 {
		t.Errorf("legacy kbd = %v %d %q", ran, code, out.String())
	}
	if ran, code := Serve([]string{"host-shell", "alice@hop1's password: "}, &out, os.Stderr); !ran || code != 0 || out.String() != "secret\n" {
		t.Errorf("legacy password = %v %d %q", ran, code, out.String())
	}
}

func TestServeNext(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	t.Setenv(markerEnv, ModeModern)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The previous helper is a different file from host-shell (a copy of the test binary).
	next := filepath.Join(t.TempDir(), filepath.Base(self))
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(next, b, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(nextEnv, next)
	t.Setenv(helperEnv, "1")
	var out bytes.Buffer
	ran, code := Serve([]string{"host-shell", "carol@hop2's password: "}, &out, os.Stderr)
	if !ran || code != 3 || out.String() != "from next: carol@hop2's password: \n" {
		t.Errorf("next = %v %d %q", ran, code, out.String())
	}
	// A previous helper that is host-shell itself is not run again.
	t.Setenv(nextEnv, self)
	out.Reset()
	if ran, code := Serve([]string{"host-shell", "carol@hop2's password: "}, &out, os.Stderr); !ran || code != 1 || out.Len() != 0 {
		t.Errorf("self as next = %v %d %q", ran, code, out.String())
	}
}

func TestModeFor(t *testing.T) {
	for _, c := range []struct {
		out    string
		legacy bool
		want   string
	}{
		{"OpenSSH_8.0p1, OpenSSL 1.1.1k  FIPS 25 Mar 2021", false, ""},
		{"OpenSSH_8.0p1, OpenSSL 1.1.1k  FIPS 25 Mar 2021", true, ModeLegacy},
		{"OpenSSH_8.4p1 Debian-5, OpenSSL 1.1.1n", false, ModeModern},
		{"OpenSSH_9.6p1 Ubuntu-3ubuntu13", false, ModeModern},
		{"OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2", false, ModeModern},
		{"OpenSSH_for_Windows_8.1p1, LibreSSL 3.0.2", false, ""},
		{"OpenSSH_10.6p1, OpenSSL 3.5.9", false, ModeModern},
		{"exec: \"ssh\": executable file not found", false, ""},
		{"", true, ModeLegacy},
	} {
		if got := modeFor(c.out, c.legacy); got != c.want {
			t.Errorf("modeFor(%q, %v) = %q, want %q", c.out, c.legacy, got, c.want)
		}
	}
}

func envValue(env []string, name string) (string, int) {
	v, n := "", 0
	for _, e := range env {
		if k, val, ok := strings.Cut(e, "="); ok && envNameEqual(k, name) {
			v, n = val, n+1
		}
	}
	return v, n
}

func TestEnv(t *testing.T) {
	base := []string{"PATH=/bin", "SSH_ASKPASS=/vscode/askpass.sh", "SSH_ASKPASS_REQUIRE=prefer"}
	if got := Env(base, "/self", ""); !slices.Equal(got, base) {
		t.Errorf("mode \"\" changed the environment: %v", got)
	}
	got := Env(base, "/self", ModeModern)
	for name, want := range map[string]string{
		"SSH_ASKPASS": "/self", "SSH_ASKPASS_REQUIRE": "force", markerEnv: ModeModern,
		nextEnv: "/vscode/askpass.sh", "PATH": "/bin",
	} {
		if v, n := envValue(got, name); v != want || n != 1 {
			t.Errorf("%s = %q (%d times), want %q", name, v, n, want)
		}
	}
	if _, n := envValue(got, "DISPLAY"); n != 0 {
		t.Error("DISPLAY set in modern mode")
	}
	// Applying it again (a bridge or wrapper under another one) keeps the chain.
	if again := Env(got, "/self", ModeModern); !slices.Equal(again, got) {
		t.Errorf("Env is not idempotent: %v", again)
	}
	legacy := Env([]string{"PATH=/bin"}, "/self", ModeLegacy)
	if v, _ := envValue(legacy, "DISPLAY"); v != "host-shell" {
		t.Errorf("legacy DISPLAY = %q", v)
	}
	if _, n := envValue(legacy, nextEnv); n != 0 {
		t.Error("next helper set without a previous SSH_ASKPASS")
	}
	if v, _ := envValue(Env([]string{"DISPLAY=:0"}, "/self", ModeLegacy), "DISPLAY"); v != ":0" {
		t.Errorf("legacy replaced DISPLAY: %q", v)
	}
}

func TestNames(t *testing.T) {
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	for name, want := range map[string]bool{"/usr/bin/ssh": true, "ssh" + exe: true, "/x/host-shell": false, "sshd": false} {
		if IsSSH(name) != want {
			t.Errorf("IsSSH(%q) != %v", name, want)
		}
	}
	for name, want := range map[string]string{"/x/host-shell-ssh" + exe: "", "/x/ssh" + exe: "ssh", "scp" + exe: "scp", "host-shell": "", "sshd": ""} {
		if got := WrapperTool(name); got != want {
			t.Errorf("WrapperTool(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFindTool(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(sshPathEnv, self)
	if _, err := findTool("ssh", sshPathEnv, self); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Errorf("findTool with itself = %v", err)
	}
	// A wrapper named ssh first in PATH is skipped in favor of the next ssh.
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	wrapDir, realDir := t.TempDir(), t.TempDir()
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(wrapDir, "ssh"+exe)
	if err := os.WriteFile(wrapper, b, 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "ssh"+exe)
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sshPathEnv, "")
	t.Setenv("PATH", wrapDir+string(os.PathListSeparator)+realDir)
	if got, err := findTool("ssh", sshPathEnv, wrapper); err != nil || !sameFile(got, real) {
		t.Errorf("findTool = %q, %v; want %q", got, err, real)
	}
	t.Setenv("PATH", wrapDir)
	if _, err := findTool("ssh", sshPathEnv, wrapper); err == nil {
		t.Error("findTool found the wrapper itself")
	}
}

func TestCLI(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	run := func(stdin string, args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := Main(args, strings.NewReader(stdin), &out, &errb)
		return code, out.String(), errb.String()
	}
	if code, _, errs := run("pw with space\r\nignored\n", "set", "--stdin", "alice@Hop1"); code != 0 {
		t.Fatalf("set --stdin = %d %s", code, errs)
	}
	if pw, _, _ := Get("alice@hop1"); pw != "pw with space" {
		t.Errorf("stored %q", pw)
	}
	if code, out, _ := run("", "list"); code != 0 || out != "alice@hop1\n" {
		t.Errorf("list = %d %q", code, out)
	}
	if code, _, _ := run("", "set", "--stdin", "alice@hop1"); code != 1 {
		t.Errorf("empty password accepted: %d", code)
	}
	if code, _, _ := run("", "delete", "alice@hop1"); code != 0 {
		t.Errorf("delete = %d", code)
	}
	if code, _, errs := run("", "delete", "alice@hop1"); code != 1 || !strings.Contains(errs, "no password") {
		t.Errorf("delete missing = %d %q", code, errs)
	}
	for _, args := range [][]string{{}, {"set"}, {"set", "-x", "a@b"}, {"list", "x"}, {"nosuch"}} {
		if code, _, _ := run("", args...); code != 2 {
			t.Errorf("%v = %d, want 2", args, code)
		}
	}
	if code, _, errs := run("", "set", "--stdin", "nohost"); code != 1 || !strings.Contains(errs, "user@host") {
		t.Errorf("bad key = %d %q", code, errs)
	}
}

func TestProcessStart(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		t.Skip("process start time is not available")
	}
	a, b := processStart(os.Getpid()), processStart(os.Getpid())
	if a == "" || a != b {
		t.Errorf("processStart = %q, %q", a, b)
	}
	if processName(os.Getpid()) == "ssh" || stopSSH(os.Getpid()) {
		t.Error("stopSSH accepted a process that is not ssh")
	}
}

func TestServeAsksOnTerminal(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	t.Setenv(markerEnv, ModeModern)
	t.Setenv(nextEnv, "")
	var gotEcho []bool
	promptLine = func(prompt string, echo bool) (string, bool, error) {
		gotEcho = append(gotEcho, echo)
		return "typed", true, nil
	}
	defer func() { promptLine = routeproxyPromptLine }()
	for _, prompt := range []string{"(carol@hop2) Password: ", "Are you sure you want to continue connecting (yes/no/[fingerprint])? "} {
		var out bytes.Buffer
		if ran, code := Serve([]string{"host-shell", prompt}, &out, os.Stderr); !ran || code != 0 || out.String() != "typed\n" {
			t.Errorf("%q = %v %d %q", prompt, ran, code, out.String())
		}
	}
	if !slices.Equal(gotEcho, []bool{false, true}) {
		t.Errorf("echo = %v", gotEcho)
	}
	// Without a terminal, the prompt fails.
	promptLine = func(string, bool) (string, bool, error) { return "", false, nil }
	var out bytes.Buffer
	if ran, code := Serve([]string{"host-shell", "(carol@hop2) Password: "}, &out, os.Stderr); !ran || code != 1 || out.Len() != 0 {
		t.Errorf("no terminal = %v %d %q", ran, code, out.String())
	}
}
