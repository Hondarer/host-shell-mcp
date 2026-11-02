package main

import (
	"flag"
	"io"
	"testing"
)

func TestRemoteDNSFlags(t *testing.T) {
	for _, tc := range []struct {
		args             []string
		enabled, invalid bool
	}{
		{[]string{"--mcp"}, false, false},
		{[]string{"--mcp", "--remote-dns"}, true, false},
		{[]string{"--mcp", "--remote-dns", "--remote-dns-library", "/tmp/hook.so"}, true, false},
		{[]string{"--mcp", "--remote-dns-library", "/tmp/hook.so"}, false, true},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		cfg, _, err := parseFlags(fs, tc.args)
		if (err != nil) != tc.invalid || cfg.RemoteDNS != tc.enabled {
			t.Fatalf("%v: cfg=%+v error=%v", tc.args, cfg, err)
		}
	}
}
