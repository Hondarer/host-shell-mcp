package tools

import "testing"

func TestStripTerminal(t *testing.T) {
	for _, c := range []struct {
		in, want string
		pending  int
	}{
		{"\x1b[1;32mok\x1b[0m done\r\n", "ok done\n", 0},
		{"\x1b]0;title\x07$ ls\r\n", "$ ls\n", 0},
		{"10%\r50%\r100%\r\nnext", "100%\nnext", 0},
		{"abc\b\bX\n", "aXc\n", 0},
		{"ok\x1b[3", "ok", 3},
		{"\x1b(Bplain\x0f", "plain", 0},
		{"日本語\x1b[K\n", "日本語\n", 0},
	} {
		got, pending := stripTerminal(c.in)
		if got != c.want || pending != c.pending {
			t.Errorf("stripTerminal(%q) = %q, %d; want %q, %d", c.in, got, pending, c.want, c.pending)
		}
	}
}
