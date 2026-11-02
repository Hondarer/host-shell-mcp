package tools

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDecodeData(t *testing.T) {
	for _, c := range []struct {
		enc, in, want string
		err           bool
	}{
		{"", "hi", "hi", false},
		{"text", "hi", "hi", false},
		{"base64", "aGk=", "hi", false},
		{"base64", "!!", "", true},
		{"hex", "6869", "", true},
	} {
		got, err := decodeData("send", c.enc, c.in)
		if (err != nil) != c.err || string(got) != c.want {
			t.Errorf("decodeData(%q, %q) = %q, %v", c.enc, c.in, got, err)
		}
	}
	if _, err := decodeData("send", "hex", ""); err == nil || err.Error() != `unknown send_encoding "hex"` {
		t.Errorf("error should name the encoding argument: %v", err)
	}
}

func TestClampMax(t *testing.T) {
	for _, c := range []struct{ limit, req, want int }{{100, 0, 100}, {100, -1, 100}, {100, 50, 50}, {100, 200, 100}} {
		if got := clampMax(c.limit, c.req); got != c.want {
			t.Errorf("clampMax(%d, %d) = %d, want %d", c.limit, c.req, got, c.want)
		}
	}
}

func TestDescendantPIDs(t *testing.T) {
	// 1 -> 2 -> 4, 1 -> 3; 5 is unrelated; 6 is its own parent.
	parents := map[int]int{2: 1, 3: 1, 4: 2, 5: 0, 6: 6}
	if got := descendantPIDs(parents, 1); !slices.Equal(got, []int{2, 3, 4}) {
		t.Errorf("descendantPIDs(1) = %v", got)
	}
	if got := descendantPIDs(parents, 6); len(got) != 0 {
		t.Errorf("descendantPIDs(6) = %v, want none", got)
	}
}

func TestCheckSpace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-file")
	if err := checkSpace(path, 0); err != nil {
		t.Errorf("unknown size should pass: %v", err)
	}
	if err := checkSpace(path, 1); err != nil {
		t.Errorf("one byte should fit: %v", err)
	}
	if err := checkSpace(path, 1<<62); err == nil || !strings.Contains(err.Error(), "not enough space") {
		t.Errorf("4 EiB should not fit: %v", err)
	}
}
