package remotedns

import "testing"

func TestResolverAddress(t *testing.T) {
	for _, tc := range []struct {
		addr, want string
	}{
		{"127.0.0.1:53", "127.0.0.1"},
		{"127.0.0.53:53", "127.0.0.53"},
		{"[::1]:53", "::1"},
		{"127.0.0.1:5353", ""},
		{"192.168.0.1:53", ""},
		{"localhost:53", ""},
		{"", ""},
	} {
		got, err := resolverAddress(tc.addr)
		if tc.want == "" {
			if err == nil {
				t.Errorf("resolverAddress(%q) = %v, want an error", tc.addr, got)
			}
			continue
		}
		if err != nil || got.String() != tc.want {
			t.Errorf("resolverAddress(%q) = %v, %v; want %s", tc.addr, got, err, tc.want)
		}
	}
}
