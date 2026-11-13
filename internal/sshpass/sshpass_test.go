package sshpass

import (
	"bytes"
	"os"
	"slices"
	"testing"
)

func TestStoreAndServe(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	t.Setenv(markerEnv, "1")
	for _, k := range []string{"alice@hop1", "Bob@[::1]"} {
		if err := Set(k, "pw "+k); err != nil {
			t.Fatal(err)
		}
	}
	if err := Set("alice@hop1", "new"); err != nil {
		t.Fatal(err)
	}
	if keys, _ := List(); !slices.Equal(keys, []string{"Bob@[::1]", "alice@hop1"}) {
		t.Errorf("List = %v", keys)
	}
	var out bytes.Buffer
	ran, code := Serve([]string{"host-shell", "alice@hop1's password: "}, &out, os.Stderr)
	if !ran || code != 0 || out.String() != "new\n" {
		t.Errorf("Serve = %v %d %q", ran, code, out.String())
	}
	for _, prompt := range []string{"bob@hop1's password: ", "Enter passphrase for key 'k': ", "Are you sure (yes/no)? "} {
		out.Reset()
		if ran, code := Serve([]string{"host-shell", prompt}, &out, os.Stderr); !ran || code != 1 || out.Len() != 0 {
			t.Errorf("prompt %q: %v %d %q", prompt, ran, code, out.String())
		}
	}
	if ok, _ := Delete("alice@hop1"); !ok {
		t.Error("Delete failed")
	}
	if _, found, _ := Get("alice@hop1"); found {
		t.Error("still stored")
	}
	t.Setenv(markerEnv, "")
	if ran, _ := Serve([]string{"host-shell", "alice@hop1's password: "}, &out, os.Stderr); ran {
		t.Error("served without the marker")
	}
}

func TestEnvKeepsUserAskpass(t *testing.T) {
	if got := Env([]string{"SSH_ASKPASS=/x"}, "/self"); len(got) != 1 {
		t.Errorf("Env overrode SSH_ASKPASS: %v", got)
	}
}

func TestEnvReplacesEmptyAskpass(t *testing.T) {
	got := Env([]string{"SSH_ASKPASS=", "DISPLAY=:0"}, "/self")
	if !slices.Contains(got, "SSH_ASKPASS=/self") || slices.Contains(got, "DISPLAY=host-shell") {
		t.Errorf("Env = %v", got)
	}
}
